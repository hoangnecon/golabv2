package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoangnecon/golabv2/internal/api"
	"github.com/hoangnecon/golabv2/internal/config"
	"github.com/hoangnecon/golabv2/internal/driver/golab"
	"github.com/hoangnecon/golabv2/internal/mcpserver"
	"github.com/hoangnecon/golabv2/internal/process"
	"github.com/hoangnecon/golabv2/internal/service"
	"github.com/hoangnecon/golabv2/internal/session"
	"github.com/hoangnecon/golabv2/internal/store/sqlite"
)

// Daemon is the singleton GoLab v2 Control-Plane composition root.
type Daemon struct {
	cfg        *config.Config
	uiFS       fs.FS
	store      *sqlite.Store
	browser    *process.ChromeManager
	bus        *session.EventBus
	coord      *session.Coordinator
	mu         sync.RWMutex
	listenAddr string
	readyCh    chan struct{}
}

// New wires all concrete adapters into Clean Architecture domain ports.
func New(cfg *config.Config, uiFS fs.FS) (*Daemon, error) {
	if cfg == nil {
		cfg = config.Load()
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = "127.0.0.1:9120"
	}
	if strings.TrimSpace(cfg.Version) == "" {
		cfg.Version = config.DefaultVersion
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		cfg.DBPath = filepath.Join(config.DefaultHomeDir(), "state.db")
	}
	if strings.TrimSpace(cfg.ProfilesRoot) == "" {
		cfg.ProfilesRoot = filepath.Join(filepath.Dir(cfg.DBPath), "profiles")
	}

	st, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}

	browser := process.NewChromeManager(process.ChromeOptions{
		ChromeBinary: cfg.ChromeBinary,
		Headless:     cfg.Headless,
	})
	driverFactory := golab.NewFactory(browser)
	bus := session.NewEventBus()
	coord := session.NewCoordinator(st, browser, driverFactory, bus, session.CoordinatorOptions{
		ReconcileInterval: 5 * time.Second,
	})

	profSvc := service.NewProfileService(st, browser, coord, bus, cfg.ProfilesRoot)
	ctxInit, cancelInit := context.WithTimeout(context.Background(), 3*time.Second)
	_, _ = profSvc.EnsureDefaultProfile(ctxInit)
	cancelInit()

	return &Daemon{
		cfg:     cfg,
		uiFS:    uiFS,
		store:   st,
		browser: browser,
		bus:     bus,
		coord:   coord,
		readyCh: make(chan struct{}),
	}, nil
}

// Addr returns the bound TCP address (e.g. "127.0.0.1:9120") once Run has bound the listener.
func (d *Daemon) Addr() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.listenAddr
}

// WaitReady waits until the HTTP control plane listener is bound and ready.
func (d *Daemon) WaitReady(timeout time.Duration) (string, error) {
	select {
	case <-d.readyCh:
		return d.Addr(), nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for daemon listener after %s", timeout)
	}
}

// Run starts the HTTP/SSE/MCP Control Plane server and 5s health reconciler until ctx is cancelled.
// On shutdown, SQLite WAL and CDP WebSockets are closed cleanly while running Chrome browsers stay alive (Spec §3.1).
func (d *Daemon) Run(ctx context.Context) error {
	defer func() {
		_ = d.coord.Close()
		_ = d.browser.ShutdownAdapter()
		_ = d.store.Close()
	}()

	ln, err := net.Listen("tcp", d.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", d.cfg.Addr, err)
	}
	boundAddr := ln.Addr().String()
	mcpURL := fmt.Sprintf("http://%s/mcp", boundAddr)

	d.mu.Lock()
	d.listenAddr = boundAddr
	close(d.readyCh)
	d.mu.Unlock()

	pidFile := filepath.Join(filepath.Dir(d.cfg.DBPath), "daemon.pid")
	_ = os.MkdirAll(filepath.Dir(pidFile), 0o700)
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	defer func() { _ = os.Remove(pidFile) }()

	profSvc := service.NewProfileService(d.store, d.browser, d.coord, d.bus, d.cfg.ProfilesRoot)
	projSvc := service.NewProjectService(d.store, d.coord, d.bus)
	sessSvc := service.NewSessionService(d.store, d.coord, d.bus)
	actSvc := service.NewActivityService(d.store, d.bus)
	wsSvc := service.NewWorkspaceService(d.store, d.browser, d.cfg.Version, mcpURL)

	mcpHandler := mcpserver.NewHTTPHandler(mcpserver.Dependencies{
		Coordinator:  d.coord,
		WorkspaceSvc: wsSvc,
		ProfileSvc:   profSvc,
		ProjectSvc:   projSvc,
		SessionSvc:   sessSvc,
		ActivitySvc:  actSvc,
		Version:      d.cfg.Version,
	})

	apiSrv := api.NewServer(api.Dependencies{
		WorkspaceSvc: wsSvc,
		ProfileSvc:   profSvc,
		ProjectSvc:   projSvc,
		SessionSvc:   sessSvc,
		ActivitySvc:  actSvc,
		EventBus:     d.bus,
		MCPHandler:   mcpHandler,
		StaticFS:     d.uiFS,
		Version:      d.cfg.Version,
		MCPHTTPURL:   mcpURL,
	})

	httpServer := &http.Server{
		Addr:              boundAddr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // Disabled so long-lived GET /api/v1/events SSE streams never cut off
	}

	d.coord.StartReconciler(ctx)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[GoLab Daemon %s] Control Plane, SSE & MCP listening on http://%s", d.cfg.Version, boundAddr)
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if d.cfg.OpenBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			openSystemBrowser("http://" + boundAddr)
		}()
	}

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func openSystemBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}

type healthProbe struct {
	Status     string `json:"status"`
	PID        int    `json:"pid"`
	Version    string `json:"version"`
	MCPHTTPURL string `json:"mcp_http_url"`
}

func probeDaemonHealth(ctx context.Context, addr string) (*healthProbe, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fmt.Sprintf("http://%s/api/v1/health", addr), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health status %d", resp.StatusCode)
	}
	var hp healthProbe
	if err := json.NewDecoder(resp.Body).Decode(&hp); err != nil {
		return nil, err
	}
	return &hp, nil
}

// EnsureDaemonRunning checks GET /api/v1/health on cfg.Addr; if down or running an older version,
// it gracefully stops the stale daemon (preserving Chrome) and spawns a detached `golab daemon` process.
func EnsureDaemonRunning(ctx context.Context, cfg *config.Config) (string, error) {
	if cfg == nil {
		cfg = config.Load()
	}
	expectedVer := cfg.Version
	if expectedVer == "" {
		expectedVer = config.DefaultVersion
	}

	if hp, err := probeDaemonHealth(ctx, cfg.Addr); err == nil && hp != nil {
		if hp.Version == expectedVer {
			if hp.MCPHTTPURL != "" {
				return hp.MCPHTTPURL, nil
			}
			return fmt.Sprintf("http://%s/mcp", cfg.Addr), nil
		}
		// Version mismatch: terminate old daemon PID so updated binary takes over cleanly
		if hp.PID > 0 && hp.PID != os.Getpid() {
			signalProcessTerm(hp.PID)
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if _, errP := probeDaemonHealth(ctx, cfg.Addr); errP != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate golab executable: %w", err)
	}

	stateDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	logPath := filepath.Join(stateDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("open daemon log: %w", err)
	}

	cmd := exec.Command(exePath, "daemon", "--addr", cfg.Addr, "--db", cfg.DBPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detachDaemonProcess(cmd)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return "", fmt.Errorf("spawn detached golab daemon: %w", err)
	}
	_ = logFile.Close()

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if hp, err := probeDaemonHealth(ctx, cfg.Addr); err == nil && hp != nil {
			if hp.MCPHTTPURL != "" {
				return hp.MCPHTTPURL, nil
			}
			return fmt.Sprintf("http://%s/mcp", cfg.Addr), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("spawned golab daemon on %s did not pass /api/v1/health within 6s (see %s)", cfg.Addr, logPath)
}
