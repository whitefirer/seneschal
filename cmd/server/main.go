package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"goworkflow/api"
	"goworkflow/config"
	"goworkflow/web"
	"goworkflow/workflow"
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

	// Create API handler with a file-backed execution store so history
	// survives restarts.
	store := workflow.NewFileStore(executionsDir)
	handler := api.NewHandler(hub, workflowsDir, store, cfg.CheckOrigin())

	// Setup routes using gorilla/mux
	r := mux.NewRouter()

	// API routes
	r.HandleFunc("/api/workflows", handler.ListWorkflows).Methods("GET")
	r.HandleFunc("/api/workflows/{name}", handler.GetWorkflow).Methods("GET")
	r.HandleFunc("/api/workflows/{name}", handler.SaveWorkflow).Methods("PUT")
	r.HandleFunc("/api/workflows/{name}", handler.DeleteWorkflow).Methods("DELETE")
	r.HandleFunc("/api/workflows/{name}/validate", handler.ValidateWorkflow).Methods("POST")
	r.HandleFunc("/api/workflows/{name}/run", handler.RunWorkflow).Methods("POST")
	r.HandleFunc("/api/executions", handler.GetExecutions).Methods("GET")
	r.HandleFunc("/api/executions/{id}", handler.GetExecution).Methods("GET")
	r.HandleFunc("/api/executions/{id}", handler.DeleteExecution).Methods("DELETE")
	r.HandleFunc("/api/executions/{id}/replay", handler.ReplayExecution).Methods("POST")
	r.HandleFunc("/api/chat", handler.ChatHandler).Methods("POST")
	r.HandleFunc("/api/ws", handler.WSHandler)

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
	fmt.Printf("  ║     goworkflow Web UI Server              ║\n")
	fmt.Printf("  ╚═══════════════════════════════════════════╝\n")
	fmt.Printf("\n")
	fmt.Printf("  🌐 Server:  http://localhost%s\n", addr)
	fmt.Printf("  📁 Workflows: %s\n", workflowsDir)
	fmt.Printf("  📋 Config: %s\n", configPath)
	if len(cfg.AllowedOrigins) > 0 {
		fmt.Printf("  🔒 Origins: %v\n", cfg.AllowedOrigins)
	}
	fmt.Printf("\n")
	fmt.Printf("  Press Ctrl+C to stop\n")
	fmt.Printf("\n")

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
