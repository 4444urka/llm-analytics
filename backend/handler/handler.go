package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/churka/llm-analytics/agent"
	"github.com/churka/llm-analytics/config"
	"github.com/churka/llm-analytics/db"
	"github.com/churka/llm-analytics/security"
)

type Handlers struct {
	cfg       *config.Config
	db        *db.DB
	dataAgent *agent.DataAgent
	mu        sync.Mutex
	statuses  map[string]*AnalysisStatus
}

type AnalysisStatus struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Progress  string `json:"progress"`
	Error     string `json:"error,omitempty"`
}

func NewHandlers(cfg *config.Config, database *db.DB, da *agent.DataAgent) *Handlers {
	return &Handlers{
		cfg:       cfg,
		db:        database,
		dataAgent: da,
		statuses:  make(map[string]*AnalysisStatus),
	}
}

func CORSMiddleware(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowOrigin := allowedOrigin
		if origin == "" {
			allowOrigin = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/upload", h.handleUpload)
	mux.HandleFunc("/api/analyze", h.handleAnalyze)
	mux.HandleFunc("/api/analyze/stream", h.handleAnalyzeStream)
	mux.HandleFunc("/api/status", h.handleStatus)
	mux.HandleFunc("/api/results", h.handleResults)
	mux.HandleFunc("/api/chat", h.handleChat)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/charts/", h.handleChart)
}

func (h *Handlers) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"model":  h.cfg.LLMModel,
	})
}

func (h *Handlers) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	if err := r.ParseMultipartForm(h.cfg.MaxUploadSizeMB << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "File too large or invalid form"})
		return
	}

	file, header, err := r.FormFile("dataset")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No file uploaded"})
		return
	}
	defer file.Close()

	filename := strings.ToLower(header.Filename)
	if !strings.HasSuffix(filename, ".csv") && !strings.HasSuffix(filename, ".xlsx") && !strings.HasSuffix(filename, ".xls") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Only CSV and Excel (.xlsx/.xls) files are supported"})
		return
	}

	instructions := r.FormValue("instructions")
	if sanitized, injected := security.SanitizeInstructions(instructions); injected {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Potentially malicious instructions detected"})
		return
	} else {
		instructions = sanitized
	}

	csvData, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to read file"})
		return
	}

	summary := generateSummaryFromBytes(csvData, header.Filename)

	sess, err := h.db.CreateSession(header.Filename, csvData, summary, instructions)
	if err != nil {
		log.Printf("ERROR creating session: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.ID,
		"filename":   sess.DatasetName,
		"summary":    sess.Summary,
		"status":     sess.Status,
	})
}

func (h *Handlers) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		return
	}

	sess, err := h.db.GetSession(req.SessionID)
	if err != nil || sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Session not found"})
		return
	}

	sandboxDir, sandbox, err := h.prepareSandbox(sess)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer sandbox.CleanWorkDir()
	defer os.RemoveAll(sandboxDir)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	h.dataAgent.Sandbox = sandbox

	result, err := h.dataAgent.Analyze(ctx, sess.Summary, sess.Instructions, sess.DatasetName)
	if err != nil {
		h.db.UpdateSessionStatus(sess.ID, "error")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var chartNames []string
	for _, chart := range result.Charts {
		data, err := sandbox.ReadPNGFile(chart)
		if err != nil {
			continue
		}
		if err := h.db.SaveChart(sess.ID, chart, data); err != nil {
			log.Printf("Failed to save chart %s: %v", chart, err)
			continue
		}
		chartNames = append(chartNames, chart)
	}

	if err := h.db.SaveResults(sess.ID, result.Report); err != nil {
		log.Printf("Failed to save results: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.ID,
		"status":     "completed",
		"report":     result.Report,
		"charts":     chartNames,
	})
}

func (h *Handlers) handleAnalyzeStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		return
	}

	sess, err := h.db.GetSession(req.SessionID)
	if err != nil || sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Session not found"})
		return
	}

	sandboxDir, sandbox, err := h.prepareSandbox(sess)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer sandbox.CleanWorkDir()
	defer os.RemoveAll(sandboxDir)

	ctx := r.Context()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	h.dataAgent.Sandbox = sandbox

	sendSSE := func(event agent.StreamEvent) {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	result, err := h.dataAgent.AnalyzeStream(ctx, sess.Summary, sess.Instructions, sess.DatasetName, sendSSE)
	if err != nil {
		sendSSE(agent.StreamEvent{Type: "error", Content: err.Error()})
		return
	}

	var chartNames []string
	for _, chart := range result.Charts {
		data, err := sandbox.ReadPNGFile(chart)
		if err != nil {
			continue
		}
		if err := h.db.SaveChart(sess.ID, chart, data); err != nil {
			continue
		}
		chartNames = append(chartNames, chart)
	}

	h.db.SaveResults(sess.ID, result.Report)

	if len(chartNames) > 0 {
		chartJSON, _ := json.Marshal(chartNames)
		fmt.Fprintf(w, "data: {\"type\":\"charts\",\"content\":%s}\n\n", chartJSON)
		flusher.Flush()
	}

	sendSSE(agent.StreamEvent{Type: "complete", Content: result.Report, Done: true})
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *Handlers) prepareSandbox(sess *db.Session) (string, *agent.Sandbox, error) {
	sandboxDir := filepath.Join(os.TempDir(), fmt.Sprintf("llm_sandbox_%s_%d", sess.ID, time.Now().UnixNano()))
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create sandbox dir: %w", err)
	}

	csvData, filename, err := h.db.GetDatasetData(sess.ID)
	if err != nil {
		os.RemoveAll(sandboxDir)
		return "", nil, fmt.Errorf("failed to get dataset: %w", err)
	}

	dstPath := filepath.Join(sandboxDir, filename)
	if err := os.WriteFile(dstPath, csvData, 0644); err != nil {
		os.RemoveAll(sandboxDir)
		return "", nil, fmt.Errorf("failed to write dataset: %w", err)
	}

	sandbox := agent.NewSandbox(h.cfg.PythonBin, h.cfg.PythonTimeout, sandboxDir)
	return sandboxDir, sandbox, nil
}

func (h *Handlers) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id required"})
		return
	}

	h.mu.Lock()
	status, ok := h.statuses[sessionID]
	h.mu.Unlock()

	if !ok {
		sess, err := h.db.GetSession(sessionID)
		if err != nil || sess == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Session not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"session_id": sess.ID,
			"status":     sess.Status,
		})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

func (h *Handlers) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id required"})
		return
	}

	sess, err := h.db.GetSession(sessionID)
	if err != nil || sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Session not found"})
		return
	}

	charts, _ := h.db.GetCharts(sessionID)
	var chartNames []string
	for _, c := range charts {
		chartNames = append(chartNames, c.Name)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sess.ID,
		"status":     sess.Status,
		"report":     sess.Report,
		"charts":     chartNames,
		"filename":   sess.DatasetName,
		"summary":    sess.Summary,
	})
}

func (h *Handlers) handleChart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	chartName := filepath.Base(strings.TrimPrefix(r.URL.Path, "/charts/"))

	if sessionID == "" || chartName == "" {
		http.Error(w, "session_id and chart name required", http.StatusBadRequest)
		return
	}

	data, err := h.db.GetChartData(sessionID, chartName)
	if err != nil {
		http.Error(w, "Chart not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}

func (h *Handlers) handleChat(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"message": "Chat"})
}

func (h *Handlers) updateStatus(sessionID, status, progress string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.statuses[sessionID] = &AnalysisStatus{
		SessionID: sessionID,
		Status:    status,
		Progress:  progress,
	}
}

func generateSummaryFromBytes(data []byte, filename string) string {
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".csv") {
		return summarizeCSVFromBytes(data, filename)
	}
	return fmt.Sprintf("File: %s (Excel format). Use pandas.read_excel() to load it.", filename)
}

func summarizeCSVFromBytes(data []byte, filename string) string {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return fmt.Sprintf("File: %s (empty)", filename)
	}

	headers := strings.Split(lines[0], ",")
	previewLines := 0
	var preview strings.Builder

	for i := 1; i < len(lines) && i <= 3; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			preview.WriteString(fmt.Sprintf("  Row %d: %s\n", i, lines[i]))
			previewLines++
		}
	}

	totalRows := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			totalRows++
		}
	}

	summary := fmt.Sprintf("File: %s\nColumns: %s\nColumns count: %d\nPreview rows: %d\nSample data:\n",
		filename, strings.Join(headers, ", "), len(headers), totalRows)

	summary += preview.String()
	summary += fmt.Sprintf("Total rows (including header): %d\n", totalRows)

	return summary
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
