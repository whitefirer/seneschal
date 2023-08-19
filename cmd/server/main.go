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
	"goworkflow/web"
)

var staticFiles = web.StaticFiles

func main() {
	// Determine workflows directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/tmp"
	}
	workflowsDir := filepath.Join(homeDir, "Desktop", "Devspace", "goworkflow", "workflows", "user")

	// Create workflows directory if not exists
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		log.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Copy example workflows if directory is empty
	entries, _ := os.ReadDir(workflowsDir)
	if len(entries) == 0 {
		examplesDir := filepath.Join(homeDir, "Desktop", "Devspace", "goworkflow", "examples")
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

	// Create WebSocket hub
	hub := api.NewWSHub()
	go hub.Run()

	// Create API handler
	handler := api.NewHandler(hub, workflowsDir)

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
	r.HandleFunc("/api/ws", handler.WSHandler)

	// Static files - SPA with fallback to index.html
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create static FS: %v", err)
	}
	
	// Create a custom file server that handles SPA routing
	fileServer := http.FileServer(http.FS(staticFS))
	
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if file exists in the filesystem
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		
		_, err := staticFS.Open(path)
		if err != nil {
			// File doesn't exist, serve index.html for SPA routing
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
		
		// File exists, serve it
		fileServer.ServeHTTP(w, r)
	})

	// Start server
	port := "8888"
	if len(os.Args) > 2 && os.Args[1] == "--port" {
		port = os.Args[2]
	}

	addr := fmt.Sprintf(":%s", port)
	fmt.Printf("\n")
	fmt.Printf("  ╔═══════════════════════════════════════════╗\n")
	fmt.Printf("  ║     goworkflow Web UI Server              ║\n")
	fmt.Printf("  ╚═══════════════════════════════════════════╝\n")
	fmt.Printf("\n")
	fmt.Printf("  🌐 Server:  http://localhost%s\n", addr)
	fmt.Printf("  📁 Workflows: %s\n", workflowsDir)
	fmt.Printf("\n")
	fmt.Printf("  Press Ctrl+C to stop\n")
	fmt.Printf("\n")

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
