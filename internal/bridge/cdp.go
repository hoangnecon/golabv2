package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// TargetInfo represents a browser tab or devtools target returned by /json/list.
type TargetInfo struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// CDPClient manages a multiplexed WebSocket connection to a Chrome DevTools Protocol target.
type CDPClient struct {
	cdpURL        string
	explicitWSURL string
	mu            sync.Mutex
	writeMu       sync.Mutex
	conn          *websocket.Conn
	target        *TargetInfo
	nextID        atomic.Int64
	pending       sync.Map // map[int64]chan *cdpResponse
	closed        chan struct{}
	connected     bool
}

type cdpRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// NewCDPClient creates a new CDP client pointing to the given CDP HTTP base URL (e.g. "http://127.0.0.1:9222").
func NewCDPClient(cdpURL string) *CDPClient {
	if cdpURL == "" {
		cdpURL = "http://127.0.0.1:9222"
	}
	return &CDPClient{
		cdpURL: strings.TrimRight(cdpURL, "/"),
		closed: make(chan struct{}),
	}
}

// NewTargetCDPClient creates a CDPClient bound directly to an explicit target WebSocket URL.
func NewTargetCDPClient(cdpPort int, target TargetInfo) *CDPClient {
	base := ""
	if cdpPort > 0 {
		base = fmt.Sprintf("http://127.0.0.1:%d", cdpPort)
	}
	return &CDPClient{
		cdpURL:        base,
		explicitWSURL: target.WebSocketDebuggerURL,
		target:        &target,
		closed:        make(chan struct{}),
	}
}

// BaseURL returns the CDP HTTP endpoint.
func (c *CDPClient) BaseURL() string {
	return c.cdpURL
}

// Target returns the currently attached target info.
func (c *CDPClient) Target() *TargetInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.target == nil {
		return nil
	}
	cp := *c.target
	return &cp
}

// ConnectToTarget dials a specific target WebSocket debugger URL directly.
func (c *CDPClient) ConnectToTarget(ctx context.Context, wsDebuggerURL string) error {
	wsDebuggerURL = strings.TrimSpace(wsDebuggerURL)
	if wsDebuggerURL == "" {
		return errors.New("empty webSocketDebuggerUrl")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected && c.conn != nil && c.explicitWSURL == wsDebuggerURL {
		return nil
	}
	if c.conn != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "reconnecting target")
		c.conn = nil
		c.connected = false
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, wsDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("dial cdp target websocket: %w", err)
	}
	conn.SetReadLimit(64 * 1024 * 1024)

	c.explicitWSURL = wsDebuggerURL
	if c.target == nil {
		c.target = &TargetInfo{WebSocketDebuggerURL: wsDebuggerURL, Type: "page"}
	} else {
		c.target.WebSocketDebuggerURL = wsDebuggerURL
	}
	c.conn = conn
	c.connected = true
	closedCh := make(chan struct{})
	c.closed = closedCh

	go c.readLoop(conn, closedCh)
	return nil
}

// Connect locates an active Colab tab and establishes a WebSocket connection.
func (c *CDPClient) Connect(ctx context.Context) error {
	if c.explicitWSURL != "" {
		return c.ConnectToTarget(ctx, c.explicitWSURL)
	}
	return c.ConnectNotebook(ctx, "")
}

// ConnectNotebook connects to a specific Colab notebook URL, or any active Colab tab if notebookURL is empty.
func (c *CDPClient) ConnectNotebook(ctx context.Context, notebookURL string) error {
	c.mu.Lock()
	if c.connected && c.conn != nil {
		if notebookURL == "" || (c.target != nil && strings.Contains(c.target.URL, notebookURL)) {
			c.mu.Unlock()
			return nil
		}
		_ = c.conn.Close(websocket.StatusNormalClosure, "switching target")
		c.conn = nil
		c.connected = false
	}
	c.mu.Unlock()

	targets, err := c.listTargets(ctx)
	if err != nil {
		return fmt.Errorf("list targets: %w", err)
	}

	var target *TargetInfo
	if notebookURL != "" {
		for i := range targets {
			t := &targets[i]
			if t.Type == "page" && strings.Contains(t.URL, notebookURL) {
				target = t
				break
			}
		}
	}
	if target == nil && notebookURL == "" {
		for i := range targets {
			t := &targets[i]
			if t.Type == "page" && strings.Contains(t.URL, "colab.research.google.com") {
				target = t
				break
			}
		}
	}
	if target == nil && notebookURL == "" {
		for i := range targets {
			t := &targets[i]
			if t.Type == "page" && strings.HasPrefix(t.URL, "http") {
				target = t
				break
			}
		}
	}
	if target == nil {
		return errors.New("no matching colab target found in chrome")
	}

	c.mu.Lock()
	c.target = target
	c.mu.Unlock()
	return c.ConnectToTarget(ctx, target.WebSocketDebuggerURL)
}

// ListTargets returns all active browser tabs and devtools targets.
func (c *CDPClient) ListTargets(ctx context.Context) ([]TargetInfo, error) {
	return c.listTargets(ctx)
}

func (c *CDPClient) listTargets(ctx context.Context) ([]TargetInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cdpURL+"/json/list", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query /json/list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var targets []TargetInfo
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// OpenTab opens a new tab with the specified URL.
func (c *CDPClient) OpenTab(ctx context.Context, targetURL string) (*TargetInfo, error) {
	endpoint := c.cdpURL + "/json/new"
	if targetURL != "" {
		endpoint += "?" + targetURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open new tab: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var target TargetInfo
	if err := json.Unmarshal(body, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

// FocusTab activates a specific tab in the browser window.
func (c *CDPClient) FocusTab(ctx context.Context, targetID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/json/activate/%s", c.cdpURL, targetID), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// CloseTab terminates a specific tab.
func (c *CDPClient) CloseTab(ctx context.Context, targetID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/json/close/%s", c.cdpURL, targetID), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *CDPClient) readLoop(conn *websocket.Conn, closed chan struct{}) {
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.connected = false
			c.conn = nil
		}
		c.mu.Unlock()

		// Fail fast any in-flight requests waiting on this socket!
		c.pending.Range(func(key, value any) bool {
			if ch, ok := value.(chan *cdpResponse); ok {
				select {
				case ch <- &cdpResponse{Error: &cdpError{Code: -32000, Message: "cdp websocket closed"}}:
				default:
				}
			}
			c.pending.Delete(key)
			return true
		})
		close(closed)
	}()

	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}

		var resp cdpResponse
		if err := json.Unmarshal(data, &resp); err != nil || resp.ID == 0 {
			continue
		}

		if ch, ok := c.pending.LoadAndDelete(resp.ID); ok {
			ch.(chan *cdpResponse) <- &resp
		}
	}
}

// Call executes an arbitrary CDP method on the connected target and returns raw result JSON.
// If a cached target WebSocket closed between calls, it transparently reconnects once.
func (c *CDPClient) Call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	res, err := c.callOnce(ctx, method, params)
	if err != nil && ctx.Err() == nil && isRetryableCDPError(err) {
		_ = c.Close()
		if reconnErr := c.ensureConnected(ctx); reconnErr == nil {
			return c.callOnce(ctx, method, params)
		}
	}
	return res, err
}

func isRetryableCDPError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cdp websocket closed") ||
		strings.Contains(msg, "cdp connection closed") ||
		strings.Contains(msg, "cdp websocket disconnected") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "eof")
}

func (c *CDPClient) callOnce(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	id := c.nextID.Add(1)
	req := cdpRequest{
		ID:     id,
		Method: method,
		Params: params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan *cdpResponse, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	c.mu.Lock()
	conn := c.conn
	closedCh := c.closed
	c.mu.Unlock()

	if conn == nil {
		return nil, errors.New("cdp connection closed")
	}

	writeCtx, cancelWrite := context.WithTimeout(ctx, 5*time.Second)
	c.writeMu.Lock()
	writeErr := conn.Write(writeCtx, websocket.MessageText, reqBytes)
	c.writeMu.Unlock()
	cancelWrite()
	if writeErr != nil {
		// Force-close broken socket so no subsequent caller blocks on writeMu!
		_ = c.Close()
		return nil, fmt.Errorf("send cdp request: %w", writeErr)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("cdp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-closedCh:
		return nil, errors.New("cdp websocket disconnected during request")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Evaluate runs a JavaScript expression in the page and returns the serialized JSON value.
func (c *CDPClient) Evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	rawResult, err := c.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"awaitPromise":  true,
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}

	var evalResult struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description,omitempty"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails,omitempty"`
	}

	if err := json.Unmarshal(rawResult, &evalResult); err != nil {
		return nil, fmt.Errorf("unmarshal cdp evaluate result: %w", err)
	}

	if evalResult.ExceptionDetails != nil {
		desc := evalResult.ExceptionDetails.Text
		if evalResult.ExceptionDetails.Exception != nil && evalResult.ExceptionDetails.Exception.Description != "" {
			desc = evalResult.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("js exception: %s", desc)
	}

	return evalResult.Result.Value, nil
}

func (c *CDPClient) ensureConnected(ctx context.Context) error {
	c.mu.Lock()
	connected := c.connected && c.conn != nil
	c.mu.Unlock()

	if !connected {
		return c.Connect(ctx)
	}
	return nil
}

// Close terminates the CDP client WebSocket without closing the browser tab itself.
func (c *CDPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "client closing")
		c.conn = nil
	}
	c.connected = false
	return nil
}
