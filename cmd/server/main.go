package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/api"
	"github.com/whitefirer/seneschal/config"
	"github.com/whitefirer/seneschal/web"
	"github.com/whitefirer/seneschal/workflow"
)

var staticFiles = web.StaticFiles

func main() {
	// Load config
	configPath := config.ConfigFlag()
	if configPath == "" {
		configPath = "server.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Warning: failed to load config: %v, using defaults", err)
		cfg = config.Default()
	}

	// CLI flags override config
	if p := config.PortFlag(); p != "" {
		cfg.Port = p
	}
	if h := config.HostFlag(); h != "" {
		cfg.Host = h
	}

	// Resolve workflows directory
	workflowsDir := cfg.WorkflowsDir
	if !filepath.IsAbs(workflowsDir) {
		if wd, err := os.Getwd(); err == nil {
			workflowsDir = filepath.Join(wd, workflowsDir)
		}
	}

	// Create workflows directory if not exists
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		log.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Copy example workflows if directory is empty
	entries, _ := os.ReadDir(workflowsDir)
	if len(entries) == 0 {
		if wd, err := os.Getwd(); err == nil {
			examplesDir := filepath.Join(wd, "examples")
			if files, err := os.ReadDir(examplesDir); err == nil {
				for _, f := range files {
					if !f.IsDir() && (filepath.Ext(f.Name()) == ".yaml" || filepath.Ext(f.Name()) == ".yml") {
						src := filepath.Join(examplesDir, f.Name())
						dst := filepath.Join(workflowsDir, f.Name())
						if data, err := os.ReadFile(src); err == nil {
							os.WriteFile(dst, data, 0644)
						}
					}
				}
			}
		}
	}

	// Resolve executions directory (for persisted history)
	executionsDir := cfg.ExecutionsDir
	if executionsDir == "" {
		executionsDir = "./executions"
	}
	if !filepath.IsAbs(executionsDir) {
		if wd, err := os.Getwd(); err == nil {
			executionsDir = filepath.Join(wd, executionsDir)
		}
	}

	// Create WebSocket hub
	hub := api.NewWSHub()
	go hub.Run()

	// Resolve runbooks directory.
	runbooksDir := cfg.RunbooksDir
	if runbooksDir == "" {
		runbooksDir = "./runbooks"
	}
	if !filepath.IsAbs(runbooksDir) {
		if wd, err := os.Getwd(); err == nil {
			runbooksDir = filepath.Join(wd, runbooksDir)
		}
	}
	os.MkdirAll(runbooksDir, 0755)

	// Create API handler with a file-backed execution store so history
	// survives restarts.
	store := workflow.NewFileStore(executionsDir)
	// Convert server-level AI config to workflow.AIConfig for the handler.
	aiCfg := workflow.AIConfig{
		Provider:    cfg.AI.Provider,
		Model:       cfg.AI.Model,
		BaseURL:     cfg.AI.BaseURL,
		MaxTokens:   cfg.AI.MaxTokens,
		Temperature: cfg.AI.Temperature,
	}
	// Convert server-level hooks to workflow.HookConfig.
	globalHooks := make([]workflow.HookConfig, len(cfg.Hooks))
	for i, h := range cfg.Hooks {
		globalHooks[i] = workflow.HookConfig{
			On:      workflow.HookPhase(h.On),
			When:    h.When,
			Type:    h.Type,
			URL:     h.URL,
			Message: h.Message,
			Command: h.Command,
			Mode:    h.Mode,
			Prompt:  h.Prompt,
		}
	}
	handler := api.NewHandler(hub, workflowsDir, store, aiCfg, globalHooks, cfg.CheckOrigin())

	// Runbook manager — trigger/schedule management.
	runbookMgr := workflow.NewRunbookManager(runbooksDir, workflowsDir,
		api.MakeTriggerCallback(store, hub, workflowsDir, aiCfg),
		func(format string, args ...interface{}) { log.Printf(format, args...) },
	)
	runbookMgr.LoadDir()
	go runbookMgr.Watch(10 * time.Second)
	runbookHandler := api.NewRunbookHandler(runbookMgr, runbooksDir, workflowsDir)

	// Setup routes using gorilla/mux. The entire API route table (including
	// runbook routes and /api/ws) is registered by api.RegisterRoutes — the
	// single source of truth shared with the e2e tests. Only the SPA static
	// fallback below is registered here (it needs the embedded web FS).
	r := mux.NewRouter()
	api.RegisterRoutes(r, handler, runbookHandler)

	// Static files - SPA with fallback to index.html
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create static FS: %v", err)
	}

	fileServer := http.FileServer(http.FS(staticFS))

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		_, err := staticFS.Open(path)
		if err != nil {
			indexData, readErr := fs.ReadFile(staticFS, "index.html")
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			w.Write(indexData)
			return
		}

		fileServer.ServeHTTP(w, r)
	})

	addr := cfg.Addr()
	fmt.Printf("\n")
	fmt.Printf("  ╔═══════════════════════════════════════════╗\n")
	fmt.Printf("  ║     seneschal Web UI Server               ║\n")
	fmt.Printf("  ╚═══════════════════════════════════════════╝\n")
	fmt.Printf("\n")
	// Display URL: for 0.0.0.0 show localhost (most useful), otherwise the
	// actual host so users know how to reach it.
	displayHost := cfg.Host
	if displayHost == "0.0.0.0" || displayHost == "" {
		displayHost = "localhost"
	}
	fmt.Printf("  🌐 Server:  http://%s:%s\n", displayHost, cfg.Port)
	fmt.Printf("  📁 Workflows: %s\n", workflowsDir)
	fmt.Printf("  📋 Config: %s\n", configPath)
	if len(cfg.AllowedOrigins) > 0 {
		fmt.Printf("  🔒 Origins: %v\n", cfg.AllowedOrigins)
	}
	if cfg.AuthToken != "" {
		fmt.Printf("  🔑 Auth: Bearer token required for /api/*\n")
	}
	fmt.Printf("\n")
	fmt.Printf("  Press Ctrl+C to stop\n")
	fmt.Printf("\n")

	// Informed-design guardrail: binding a shell-executing server to a
	// non-loopback interface without auth is almost certainly a mistake.
	// Warn loudly but do not fatal — the operator may have an authenticating
	// reverse proxy in front.
	if cfg.AuthToken == "" && !isLoopbackHost(cfg.Host) {
		log.Printf("⚠️  WARNING: no auth_token configured and binding to %s — anyone who can reach this port can run arbitrary shell commands on this host. Set auth_token in %s.", addr, configPath)
	}

	// Middleware chain: CORS outermost (handles preflight before auth), then
	// Bearer-token auth on /api/*, then the router.
	var root http.Handler = r
	root = cfg.AuthMiddleware(root)
	root = cfg.CORSMiddleware(root)

	srv := &http.Server{
		Addr:    addr,
		Handler: root,
		// No WriteTimeout: it would kill long-lived WebSocket/SSE connections.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown failed: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

// isLoopbackHost reports whether host binds only to loopback (empty host
// defaults to 127.0.0.1 via ServerConfig.Addr).
func isLoopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
