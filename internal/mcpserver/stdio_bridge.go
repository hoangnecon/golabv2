package mcpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (rt *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if rt.token != "" && clone.Header.Get("Authorization") == "" {
		clone.Header.Set("Authorization", "Bearer "+rt.token)
	}
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// RunStdioBridge bridges os.Stdin / os.Stdout (MCP StdioTransport) to the singleton daemon's /mcp Streamable HTTP endpoint.
func RunStdioBridge(ctx context.Context, mcpEndpointURL string, token string) error {
	return runTransportBridge(ctx, &mcp.StdioTransport{}, mcpEndpointURL, token)
}

// RunIOBridge bridges arbitrary io.ReadCloser / io.WriteCloser streams to the daemon's /mcp Streamable HTTP endpoint.
func RunIOBridge(ctx context.Context, reader io.ReadCloser, writer io.WriteCloser, mcpEndpointURL string, token string) error {
	if reader == nil {
		reader = os.Stdin
	}
	if writer == nil {
		writer = os.Stdout
	}
	return runTransportBridge(ctx, &mcp.IOTransport{Reader: reader, Writer: writer}, mcpEndpointURL, token)
}

func runTransportBridge(ctx context.Context, localTransport mcp.Transport, mcpEndpointURL string, token string) error {
	token = strings.TrimSpace(token)
	endpoint := strings.TrimSpace(mcpEndpointURL)
	if token != "" {
		if u, err := url.Parse(endpoint); err == nil {
			q := u.Query()
			if q.Get("token") == "" {
				q.Set("token", token)
				u.RawQuery = q.Encode()
				endpoint = u.String()
			}
		}
	}

	httpClient := &http.Client{
		Transport: &bearerRoundTripper{
			base:  http.DefaultTransport,
			token: token,
		},
	}

	remoteTransport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}

	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	localConn, err := localTransport.Connect(bridgeCtx)
	if err != nil {
		return err
	}
	defer localConn.Close()

	remoteConn, err := remoteTransport.Connect(bridgeCtx)
	if err != nil {
		return err
	}
	defer remoteConn.Close()

	errCh := make(chan error, 2)

	// IDE client -> Daemon /mcp
	go func() {
		for {
			msg, err := localConn.Read(bridgeCtx)
			if err != nil {
				errCh <- err
				return
			}
			if err := remoteConn.Write(bridgeCtx, msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// Daemon /mcp -> IDE client
	go func() {
		for {
			msg, err := remoteConn.Read(bridgeCtx)
			if err != nil {
				errCh <- err
				return
			}
			if err := localConn.Write(bridgeCtx, msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-bridgeCtx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}
