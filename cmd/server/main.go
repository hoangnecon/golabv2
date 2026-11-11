package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/hoangnecon/golabv2/internal/app"
	"github.com/hoangnecon/golabv2/internal/config"
	"github.com/hoangnecon/golabv2/internal/mcpserver"
	"github.com/hoangnecon/golabv2/ui"
)

func main() {
	baseCfg := config.Load()

	// Support subcommand positional prefix (`golab daemon ...`, `golab mcp ...`, `golab ui ...`, `golab version`)
	mode := "mcp"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "daemon", "serve":
			mode = "daemon"
			args = args[1:]
		case "ui":
			mode = "ui"
			args = args[1:]
		case "mcp":
			mode = "mcp"
			args = args[1:]
		case "version":
			fmt.Println(baseCfg.Version)
			return
		}
	}

	fs := flag.NewFlagSet("golab", flag.ExitOnError)
	addr := fs.String("addr", baseCfg.Addr, "Control-plane HTTP/SSE/MCP bind address (default 127.0.0.1:9120)")
	uiPort := fs.Int("ui-port", 0, "Legacy alias for --addr 127.0.0.1:<port>")
	uiOnly := fs.Bool("ui-only", false, "Run singleton Control-Plane Daemon (alias for 'golab daemon')")
	noMCP := fs.Bool("no-mcp", false, "Legacy alias for 'golab daemon'")
	openFlag := fs.Bool("open", false, "Open GoLab Web UI in system browser on daemon startup")
	dbPath := fs.String("db", baseCfg.DBPath, "Path to SQLite WAL database (default ~/.golab/state.db)")
	token := fs.String("token", baseCfg.Token, "GoLab Project Token (glp_...) or Session Token (gls_...) for MCP routing")
	headless := fs.Bool("headless", false, "Launch managed Chrome profiles in --headless=new mode")
	_ = fs.Parse(args)

	if *uiPort > 0 {
		*addr = fmt.Sprintf("127.0.0.1:%d", *uiPort)
	}
	if *uiOnly || *noMCP {
		mode = "daemon"
	}
	if mode == "ui" {
		mode = "daemon"
		*openFlag = true
	}

	baseCfg.Addr = *addr
	baseCfg.DBPath = *dbPath
	baseCfg.Token = strings.TrimSpace(*token)
	baseCfg.OpenBrowser = *openFlag
	baseCfg.Headless = *headless

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if mode == "daemon" {
		uiFS, errFS := ui.FS()
		if errFS != nil {
			log.Printf("[UI] Embedded assets unavailable: %v", errFS)
		}
		daemon, err := app.New(baseCfg, uiFS)
		if err != nil {
			log.Fatalf("initialize golab daemon: %v", err)
		}
		if err := daemon.Run(ctx); err != nil && err != context.Canceled {
			log.Fatalf("golab daemon error: %v", err)
		}
		return
	}

	// Default mode ("mcp"): ensure singleton daemon is running, then bridge stdio <-> /mcp HTTP!
	mcpEndpoint, err := app.EnsureDaemonRunning(ctx, baseCfg)
	if err != nil {
		log.Fatalf("ensure golab daemon running: %v", err)
	}
	if err := mcpserver.RunStdioBridge(ctx, mcpEndpoint, baseCfg.Token); err != nil && err != context.Canceled {
		log.Fatalf("golab mcp stdio bridge error: %v", err)
	}
}
