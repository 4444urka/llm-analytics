package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	LLMAPIKey       string
	LLMAPIBase      string
	LLMModel        string
	Port            string
	DBPath          string
	MaxUploadSizeMB int64
	PythonBin       string
	PythonTimeout   int
	FrontendOrigin  string
}

func Load() *Config {
	loadDotEnv()

	return &Config{
		LLMAPIKey:       getEnv("LLM_API_KEY", ""),
		LLMAPIBase:      getEnv("LLM_API_BASE", "https://api.deepseek.com/v1"),
		LLMModel:        getEnv("LLM_MODEL", "deepseek-v4-flash"),
		Port:            getEnv("PORT", "8080"),
		DBPath:          getEnv("DB_PATH", "./data.db"),
		MaxUploadSizeMB: getEnvInt64("MAX_UPLOAD_SIZE_MB", 50),
		PythonBin:       getEnv("PYTHON_BIN", "python3"),
		PythonTimeout:   getEnvInt("PYTHON_TIMEOUT_SEC", 60),
		FrontendOrigin:  getEnv("FRONTEND_ORIGIN", ""),
	}
}

func loadDotEnv() {
	candidates := []string{".env", "../.env"}

	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, ".env"))
	}

	for _, path := range candidates {
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			line = strings.TrimPrefix(line, "export ")

			eq := strings.Index(line, "=")
			if eq < 0 {
				continue
			}

			key := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])

			val = strings.Trim(val, `"'`)

			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
		f.Close()

		log.Printf("Loaded .env from %s", path)
		return
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
