package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/churka/llm-analytics/agent"
	"github.com/churka/llm-analytics/config"
	"github.com/churka/llm-analytics/db"
	"github.com/churka/llm-analytics/handler"
	"github.com/churka/llm-analytics/security"
)

//go:embed frontend-dist/*
var frontendAssets embed.FS

func main() {
	cfg := config.Load()

	if cfg.LLMAPIKey == "" {
		fmt.Println("ERROR: LLM_API_KEY environment variable is required")
		fmt.Println("Get your API key at https://platform.deepseek.com")
		fmt.Println("Then: export LLM_API_KEY=sk-xxxxxxxxxxxx")
		os.Exit(1)
	}

	log.Printf("Initializing LLM Data Analytics server...")
	log.Printf("Model: %s", cfg.LLMModel)
	log.Printf("API Base: %s", cfg.LLMAPIBase)
	log.Printf("DB: %s", cfg.DBPath)

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()
	log.Printf("Database connected")

	if err := security.CheckPython(cfg.PythonBin); err != nil {
		log.Printf("WARNING: Python check failed: %v", err)
		log.Printf("Python execution will not work. Install python3 and required packages:")
		log.Printf("  pip install pandas numpy matplotlib seaborn scikit-learn openpyxl")
	}

	llm := agent.NewLLMClient(cfg.LLMAPIKey, cfg.LLMAPIBase, cfg.LLMModel)
	dataAgent := agent.NewDataAgent(llm, agent.NewSandbox(cfg.PythonBin, cfg.PythonTimeout, ""))

	h := handler.NewHandlers(cfg, database, dataAgent)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	serveEmbedded(mux)

	wrappedMux := handler.CORSMiddleware(mux, cfg.FrontendOrigin)

	addr := ":" + cfg.Port
	log.Printf("Server starting on http://localhost%s", addr)

	if err := http.ListenAndServe(addr, wrappedMux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func serveEmbedded(mux *http.ServeMux) {
	sub, err := fs.Sub(frontendAssets, "frontend-dist")
	if err != nil {
		log.Printf("WARNING: embedded frontend not available: %v", err)
		return
	}

	fileServer := http.FileServer(http.FS(sub))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") ||
			strings.HasPrefix(r.URL.Path, "/charts/") {
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := sub.Open(path)
		if err != nil {
			r.URL.Path = "/index.html"
		} else {
			f.Close()
		}

		fileServer.ServeHTTP(w, r)
	})
}
