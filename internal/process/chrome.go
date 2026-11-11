package process

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// ChromeOptions configures ChromeManager.
type ChromeOptions struct {
	ChromeBinary string
	Headless     bool
}

// ChromeManager implements domain.BrowserManager, managing isolated Chrome profile processes and CDP targets.
type ChromeManager struct {
	opts      ChromeOptions
	mu        sync.RWMutex
	processes map[string]*exec.Cmd
	ports     map[string]int
	httpCli   *http.Client
}

// Manager is an alias for ChromeManager for transitional compatibility.
type Manager = ChromeManager

// NewChromeManager creates a new ChromeManager implementing domain.BrowserManager.
func NewChromeManager(opts ChromeOptions) *ChromeManager {
	return &ChromeManager{
		opts:      opts,
		processes: make(map[string]*exec.Cmd),
		ports:     make(map[string]int),
		httpCli:   &http.Client{Timeout: 1200 * time.Millisecond},
	}
}

func NewManager() *ChromeManager {
	return NewChromeManager(ChromeOptions{})
}

// FindChromePath locates the Google Chrome / Chromium binary on the host OS.
func FindChromePath() (string, error) {
	if env := strings.TrimSpace(os.Getenv("CHROME_PATH")); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
	}

	switch runtime.GOOS {
	case "darwin":
		candidates := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(os.Getenv("HOME"), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	case "linux":
		candidates := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				return p, nil
			}
		}
	case "windows":
		candidates := []string{
			os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("ProgramFiles(x86)") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("LocalAppData") + `\Google\Chrome\Application\chrome.exe`,
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}

	return "", domain.NewAppError(domain.CodeBrowserStopped, fmt.Sprintf("Google Chrome binary not found on %s", runtime.GOOS), "Install Google Chrome or set CHROME_PATH.", http.StatusServiceUnavailable)
}

// IsPortListening checks if any TCP listener occupies 127.0.0.1:<port>.
func IsPortListening(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// IsCDPAlive verifies that 127.0.0.1:<port>/json/version returns HTTP 200 AND a non-empty webSocketDebuggerUrl.
// This avoids mistaking non-CDP listeners (e.g. Chrome without remote debugging returning 404) for active CDP!
func IsCDPAlive(ctx context.Context, port int) bool {
	if port <= 0 {
		return false
	}
	reqCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	var meta struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return false
	}
	return strings.TrimSpace(meta.WebSocketDebuggerURL) != ""
}

func findFreePortNear(start int) (int, error) {
	for p := start; p < start+100; p++ {
		if !IsPortListening(p) {
			return p, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func discoverPortOwnerPID(port int) int {
	if port <= 0 || runtime.GOOS == "windows" {
		return 0
	}
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN").Output()
	if err != nil {
		return 0
	}
	lines := strings.Fields(string(out))
	if len(lines) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(lines[0])
	if err != nil {
		return 0
	}
	return pid
}

// IsProfileRunning checks whether a healthy CDP endpoint is listening on cdpPort.
func (m *ChromeManager) IsProfileRunning(ctx context.Context, cdpPort int) bool {
	return IsCDPAlive(ctx, cdpPort)
}

// LaunchProfile starts or re-attaches to an isolated Chrome profile and returns (pid, actualPort, err).
func (m *ChromeManager) LaunchProfile(ctx context.Context, profile domain.Profile, initialURL string) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	port := profile.CDPPort
	if port <= 0 {
		port = 9222
	}

	// If CDP is already healthy on this port, reuse it cleanly!
	if IsCDPAlive(ctx, port) {
		pid := profile.ChromePID
		if cmd, ok := m.processes[profile.ID]; ok && cmd.Process != nil {
			pid = cmd.Process.Pid
		}
		if pid <= 0 {
			pid = discoverPortOwnerPID(port)
		}
		return pid, port, nil
	}

	// If a non-CDP service occupies the port (like port 9222 returning 404), dynamically pick a free port!
	if IsPortListening(port) {
		freePort, err := findFreePortNear(port + 1)
		if err != nil {
			return 0, 0, domain.WrapAppError(domain.CodeCDPPortConflict, fmt.Sprintf("port %d is occupied by a non-CDP listener and no free port could be allocated", port), "", http.StatusConflict, err)
		}
		port = freePort
	}

	chromeBin := m.opts.ChromeBinary
	if chromeBin == "" {
		var err error
		chromeBin, err = FindChromePath()
		if err != nil {
			return 0, 0, err
		}
	}

	if err := os.MkdirAll(profile.UserDataDir, 0o700); err != nil {
		return 0, 0, fmt.Errorf("create profile user_data_dir: %w", err)
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-allow-origins=*",
		fmt.Sprintf("--user-data-dir=%s", profile.UserDataDir),
		"--no-first-run",
		"--no-default-browser-check",
	}
	if m.opts.Headless {
		args = append(args, "--headless=new")
	}
	if strings.TrimSpace(initialURL) != "" {
		args = append(args, initialURL)
	} else {
		args = append(args, "https://colab.research.google.com")
	}

	cmd := exec.Command(chromeBin, args...)
	setDetachedProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return 0, 0, fmt.Errorf("start chrome binary: %w", err)
	}

	pid := cmd.Process.Pid
	m.processes[profile.ID] = cmd
	m.ports[profile.ID] = port

	go func(id string, c *exec.Cmd) {
		_ = c.Wait()
		m.mu.Lock()
		if m.processes[id] == c {
			delete(m.processes, id)
			delete(m.ports, id)
		}
		m.mu.Unlock()
	}(profile.ID, cmd)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if IsCDPAlive(ctx, port) {
			if actualPID := discoverPortOwnerPID(port); actualPID > 0 {
				pid = actualPID
			}
			return pid, port, nil
		}
		select {
		case <-ctx.Done():
			return pid, port, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}

	return pid, port, domain.NewAppError(domain.CodeBrowserStopped, fmt.Sprintf("Chrome started (PID %d) on port %d, but CDP /json/version did not respond within 8s", pid, port), "Ensure no stale Chrome SingletonLock exists in user_data_dir.", http.StatusGatewayTimeout)
}

// StopProfile terminates Chrome for a profile—supporting both tracked child processes AND pre-existing external PIDs!
func (m *ChromeManager) StopProfile(ctx context.Context, profile domain.Profile) error {
	m.mu.Lock()
	cmd := m.processes[profile.ID]
	delete(m.processes, profile.ID)
	delete(m.ports, profile.ID)
	m.mu.Unlock()

	var pids []int
	if cmd != nil && cmd.Process != nil {
		pids = append(pids, cmd.Process.Pid)
	}
	if profile.ChromePID > 0 {
		pids = appendUniquePID(pids, profile.ChromePID)
	}
	if portPID := discoverPortOwnerPID(profile.CDPPort); portPID > 0 {
		pids = appendUniquePID(pids, portPID)
	}

	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil && p != nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}

	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !IsCDPAlive(ctx, profile.CDPPort) && !IsPortListening(profile.CDPPort) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Escalate to SIGKILL if still listening after graceful grace period.
	if portPID := discoverPortOwnerPID(profile.CDPPort); portPID > 0 {
		pids = appendUniquePID(pids, portPID)
	}
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil && p != nil {
			_ = p.Kill()
		}
	}

	time.Sleep(150 * time.Millisecond)
	return nil
}

func appendUniquePID(list []int, pid int) []int {
	if pid <= 0 {
		return list
	}
	for _, x := range list {
		if x == pid {
			return list
		}
	}
	return append(list, pid)
}

// ListTargets returns all CDP targets from http://127.0.0.1:<cdpPort>/json/list.
func (m *ChromeManager) ListTargets(ctx context.Context, cdpPort int) ([]domain.CDPTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", cdpPort), nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, domain.WrapAppError(domain.CodeBrowserStopped, fmt.Sprintf("query Chrome CDP /json/list on port %d failed", cdpPort), "Launch the account's Chrome browser first.", http.StatusServiceUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, domain.NewAppError(domain.CodeBrowserStopped, fmt.Sprintf("CDP /json/list returned HTTP %d", resp.StatusCode), "", http.StatusServiceUnavailable)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var targets []domain.CDPTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// OpenOrFocusTab finds an existing page tab matching matchSubstr (and focuses it) or opens targetURL in a new tab.
func (m *ChromeManager) OpenOrFocusTab(ctx context.Context, cdpPort int, targetURL string, matchSubstr string) (domain.CDPTarget, error) {
	targets, err := m.ListTargets(ctx, cdpPort)
	if err != nil {
		return domain.CDPTarget{}, err
	}

	needle := strings.TrimSpace(matchSubstr)
	if needle == "" {
		needle = strings.TrimSpace(targetURL)
	}

	if needle != "" {
		for _, t := range targets {
			if t.Type == "page" && strings.Contains(t.URL, needle) {
				_ = m.FocusTab(ctx, cdpPort, t.ID)
				return t, nil
			}
		}
	}

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/new", cdpPort)
	if strings.TrimSpace(targetURL) != "" {
		endpoint += "?" + targetURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return domain.CDPTarget{}, err
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return domain.CDPTarget{}, err
	}
	defer resp.Body.Close()

	// Fallback to GET /json/new for older Chromium builds if PUT returns 405.
	if resp.StatusCode == http.StatusMethodNotAllowed {
		reqGet, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		respGet, errGet := m.httpCli.Do(reqGet)
		if errGet != nil {
			return domain.CDPTarget{}, errGet
		}
		defer respGet.Body.Close()
		resp = respGet
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.CDPTarget{}, err
	}
	var created domain.CDPTarget
	if err := json.Unmarshal(body, &created); err != nil {
		return domain.CDPTarget{}, fmt.Errorf("decode /json/new target: %w", err)
	}
	return created, nil
}

// FocusTab activates a tab by ID in the Chrome window.
func (m *ChromeManager) FocusTab(ctx context.Context, cdpPort int, tabID string) error {
	if strings.TrimSpace(tabID) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/activate/%s", cdpPort, tabID), nil)
	if err != nil {
		return err
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// CloseTab closes a tab by ID via /json/close/<tabID>.
func (m *ChromeManager) CloseTab(ctx context.Context, cdpPort int, tabID string) error {
	if strings.TrimSpace(tabID) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/close/%s", cdpPort, tabID), nil)
	if err != nil {
		return err
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ShutdownAdapter detaches the daemon's in-memory process map WITHOUT killing running Chrome browsers (Spec §3.1).
func (m *ChromeManager) ShutdownAdapter() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processes = make(map[string]*exec.Cmd)
	m.ports = make(map[string]int)
	return nil
}

var _ domain.BrowserManager = (*ChromeManager)(nil)
