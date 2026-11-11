package bridge_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangnecon/golabv2/internal/bridge"
	"nhooyr.io/websocket"
)

func TestCDPClient_ConcurrentOutOfOrderEvaluate(t *testing.T) {
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		var writeMu sync.Mutex
		var wg sync.WaitGroup
		ctx := r.Context()

		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				break
			}
			var req struct {
				ID     int64 `json:"id"`
				Method string `json:"method"`
				Params struct {
					Expression string `json:"expression"`
				} `json:"params"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				continue
			}

			wg.Add(1)
			go func(id int64, expr string) {
				defer wg.Done()
				// Deliberately jitter response timing so responses arrive out of order!
				time.Sleep(time.Duration(5+rand.Intn(25)) * time.Millisecond)
				resp := map[string]any{
					"id": id,
					"result": map[string]any{
						"result": map[string]any{
							"type":  "string",
							"value": "echo:" + expr,
						},
					},
				}
				b, _ := json.Marshal(resp)
				writeMu.Lock()
				_ = conn.Write(ctx, websocket.MessageText, b)
				writeMu.Unlock()
			}(req.ID, req.Params.Expression)
		}
		wg.Wait()
	}))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := bridge.NewCDPClient("")
	if err := client.ConnectToTarget(ctx, wsURL); err != nil {
		t.Fatalf("ConnectToTarget: %v", err)
	}
	defer client.Close()

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			expr := fmt.Sprintf("job_%d", idx)
			raw, err := client.Evaluate(ctx, expr)
			if err != nil {
				errs <- fmt.Errorf("worker %d evaluate: %w", idx, err)
				return
			}
			var got string
			if err := json.Unmarshal(raw, &got); err != nil {
				errs <- fmt.Errorf("worker %d unmarshal: %w", idx, err)
				return
			}
			want := "echo:" + expr
			if got != want {
				errs <- fmt.Errorf("worker %d cross-talk: got %q, want %q", idx, got, want)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestCDPClient_AutoReconnectOnClosedWebSocketAndLargePayload(t *testing.T) {
	var connCount int
	var mu sync.Mutex
	largeText := strings.Repeat("x", 128*1024) // 128 KB (> default 32 KB websocket limit)

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		conn.SetReadLimit(64 * 1024 * 1024)
		mu.Lock()
		connCount++
		curConn := connCount
		mu.Unlock()

		ctx := r.Context()
		_, data, err := conn.Read(ctx)
		if err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		// Drop the very first request's socket mid-flight to force transparent auto-reconnect!
		if curConn == 1 {
			_ = conn.Close(websocket.StatusGoingAway, "simulated chrome target reset")
			return
		}
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(data, &req)
		resp := map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"result": map[string]any{
					"type":  "string",
					"value": largeText,
				},
			},
		}
		b, _ := json.Marshal(resp)
		_ = conn.Write(ctx, websocket.MessageText, b)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := bridge.NewTargetCDPClient(0, bridge.TargetInfo{
		ID:                   "tab-reconn",
		Type:                 "page",
		WebSocketDebuggerURL: wsURL,
	})
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	raw, err := client.Evaluate(ctx, "big_output()")
	if err != nil {
		t.Fatalf("expected transparent auto-reconnect to succeed, got err: %v", err)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil || len(got) != len(largeText) {
		t.Fatalf("unexpected large payload len=%d, err=%v", len(got), err)
	}
}
