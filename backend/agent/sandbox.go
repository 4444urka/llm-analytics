package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/churka/llm-analytics/security"
)

type Sandbox struct {
	PythonBin string
	Timeout   time.Duration
	WorkDir   string
}

type CodeResult struct {
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	Success  bool     `json:"success"`
	Charts   []string `json:"charts"`
	ExitCode int      `json:"exit_code"`
}

func NewSandbox(pythonBin string, timeoutSec int, workDir string) *Sandbox {
	if !strings.Contains(pythonBin, "/") {
		if p, err := exec.LookPath(pythonBin); err == nil {
			pythonBin = p
		}
	} else if !filepath.IsAbs(pythonBin) {
		if abs, err := filepath.Abs(pythonBin); err == nil {
			pythonBin = abs
		}
	}
	return &Sandbox{
		PythonBin: pythonBin,
		Timeout:   time.Duration(timeoutSec) * time.Second,
		WorkDir:   workDir,
	}
}

func (s *Sandbox) Execute(ctx context.Context, code string) (*CodeResult, error) {
	if security.DetectCodeInjection(code) {
		return nil, fmt.Errorf("code injection detected in submitted code")
	}

	if err := os.MkdirAll(s.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workdir: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	existingFiles := s.listPNGFiles()

	scriptPath := filepath.Join(s.WorkDir, "_sandbox_script.py")
	wrapper := s.buildWrapper(code)
	if err := os.WriteFile(scriptPath, []byte(wrapper), 0644); err != nil {
		return nil, fmt.Errorf("failed to write script: %w", err)
	}
	defer os.Remove(scriptPath)

	cmd := exec.CommandContext(ctx, s.PythonBin, "_sandbox_script.py")
	cmd.Dir = s.WorkDir

	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"MPLCONFIGDIR=/tmp/matplotlib",
		"MPLBACKEND=Agg",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &CodeResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Stderr = "Execution timed out after " + s.Timeout.String()
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Stderr += "\n" + err.Error()
		}
	}

	if result.ExitCode == 0 && result.Stderr == "" {
		result.Success = true
	}

	newFiles := s.listPNGFiles()
	result.Charts = diffFiles(existingFiles, newFiles)

	return result, nil
}

func (s *Sandbox) buildWrapper(code string) string {
	return fmt.Sprintf(`import sys, os, io, traceback, warnings
warnings.filterwarnings("ignore")
os.environ["MPLCONFIGDIR"] = "/tmp/matplotlib"
import matplotlib
matplotlib.use("Agg")

%s
`, code)
}

func (s *Sandbox) listPNGFiles() map[string]bool {
	files := make(map[string]bool)
	entries, err := os.ReadDir(s.WorkDir)
	if err != nil {
		return files
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
			files[e.Name()] = true
		}
	}
	return files
}

func (s *Sandbox) CleanWorkDir() error {
	entries, err := os.ReadDir(s.WorkDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(s.WorkDir, e.Name()))
	}
	return nil
}

func diffFiles(before, after map[string]bool) []string {
	var newFiles []string
	for name := range after {
		if !before[name] {
			newFiles = append(newFiles, name)
		}
	}
	return newFiles
}

func (s *Sandbox) ReadPNGFile(filename string) ([]byte, error) {
	path := filepath.Join(s.WorkDir, filename)
	if !strings.HasSuffix(strings.ToLower(filename), ".png") {
		return nil, fmt.Errorf("only PNG files are allowed")
	}
	filename = filepath.Base(filename)
	path = filepath.Join(s.WorkDir, filename)
	return os.ReadFile(path)
}
