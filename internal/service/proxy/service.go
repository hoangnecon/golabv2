package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// Service provides reverse proxy forwarding and external proxy URL/token retrieval for Colab VM ports.
type Service struct {
	drv domain.RuntimeDriver
}

func NewService(drv domain.RuntimeDriver) *Service {
	return &Service{drv: drv}
}

func (s *Service) ProxyForward(ctx context.Context, port int, subpath string, method string, headers map[string]string, body string) (*domain.FetchResponse, error) {
	if port <= 0 || port > 65535 {
		return nil, domain.NewAppError(domain.CodeInvalidInput, fmt.Sprintf("invalid port: %d", port), "Port must be between 1 and 65535.", http.StatusBadRequest)
	}
	subpath = strings.TrimPrefix(subpath, "/")
	proxyPath := fmt.Sprintf("/_proxy/%d/%s", port, subpath)
	return s.drv.FetchKernelXHR(ctx, proxyPath, domain.FetchOptions{
		Method:  method,
		Headers: headers,
		Body:    body,
	})
}

func (s *Service) GetProxyURL(ctx context.Context, port int) (*domain.ProxyEndpoint, error) {
	return s.drv.GetProxyURL(ctx, port)
}
