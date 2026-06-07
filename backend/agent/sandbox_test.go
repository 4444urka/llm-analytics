package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findPython(t *testing.T) string {
	t.Helper()

	candidates := []string{
		"/Users/churka/llm-analytics/.venv/bin/python3",
		"python3",
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append([]string{
			filepath.Join(cwd, "..", ".venv", "bin", "python3"),
			filepath.Join(cwd, ".venv", "bin", "python3"),
		}, candidates...)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			t.Logf("Using python: %s", p)
			return p
		}
	}
	t.Skip("python3 not found; set PYTHON_BIN env")
	return ""
}

const testCSV = `name,age,salary,department
Alice,28,75000,Engineering
Bob,34,82000,Engineering
Charlie,45,120000,Management
Diana,31,67000,Design
Eve,52,110000,Management
Frank,29,71000,Engineering
Grace,38,95000,Engineering
Henry,41,88000,Design
Ivan,26,65000,Design
Julia,47,115000,Management
`

func TestSandboxBasic(t *testing.T) {
	tmpDir := t.TempDir()
	py := findPython(t)
	sb := NewSandbox(py, 30, tmpDir)

	result, err := sb.Execute(context.Background(), `
print("hello sandbox")
x = 2 + 2
print(f"2+2={x}")
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello sandbox") {
		t.Errorf("missing 'hello sandbox' in: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "2+2=4") {
		t.Errorf("missing '2+2=4' in: %s", result.Stdout)
	}
	if result.Stderr != "" {
		t.Errorf("unexpected stderr: %s", result.Stderr)
	}
	t.Logf("stdout: %s", result.Stdout)
}

func TestSandboxReadCSV(t *testing.T) {
	tmpDir := t.TempDir()
	py := findPython(t)
	sb := NewSandbox(py, 30, tmpDir)

	csvPath := filepath.Join(tmpDir, "employees.csv")
	if err := os.WriteFile(csvPath, []byte(testCSV), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	code := fmt.Sprintf(`
import pandas as pd
df = pd.read_csv("%s")
print(f"shape={df.shape}")
print(f"mean_age={df['age'].mean():.1f}")
print(f"mean_salary={df['salary'].mean():.0f}")
`, filepath.Base(csvPath))

	result, err := sb.Execute(context.Background(), code)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Stderr != "" {
		t.Logf("stderr: %s", result.Stderr)
	}
	if !strings.Contains(result.Stdout, "mean_age=37.1") {
		t.Errorf("expected mean_age=37.1, got: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "mean_salary=88800") {
		t.Errorf("expected mean_salary=88800, got: %s", result.Stdout)
	}
	t.Logf("stdout: %s", result.Stdout)
}

func TestSandboxChart(t *testing.T) {
	tmpDir := t.TempDir()
	py := findPython(t)
	sb := NewSandbox(py, 30, tmpDir)

	csvPath := filepath.Join(tmpDir, "employees.csv")
	os.WriteFile(csvPath, []byte(testCSV), 0644)

	code := fmt.Sprintf(`
import pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

df = pd.read_csv("%s")
avg_salary = df.groupby('department')['salary'].mean()
plt.figure(figsize=(8,5))
avg_salary.plot(kind='bar', color=['#6366f1','#a855f7','#ec4899'])
plt.title("Avg Salary by Department")
plt.ylabel("Salary")
plt.tight_layout()
plt.savefig("chart_salary.png", dpi=150, bbox_inches='tight')
print("chart saved")
`, filepath.Base(csvPath))

	result, err := sb.Execute(context.Background(), code)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Stderr != "" {
		t.Logf("stderr: %s", result.Stderr)
	}
	if len(result.Charts) == 0 {
		t.Fatalf("no charts generated, charts=%v", result.Charts)
	}
	if result.Charts[0] != "chart_salary.png" {
		t.Errorf("expected 'chart_salary.png', got %v", result.Charts)
	}
	t.Logf("Charts: %v", result.Charts)
}

func TestSandboxFileNotFoundReturnsStderr(t *testing.T) {
	tmpDir := t.TempDir()
	py := findPython(t)
	sb := NewSandbox(py, 30, tmpDir)

	result, err := sb.Execute(context.Background(), `
import pandas as pd
df = pd.read_csv("nonexistent.csv")
`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Success {
		t.Error("expected failure for missing file")
	}
	if !strings.Contains(result.Stderr, "FileNotFoundError") &&
		!strings.Contains(result.Stderr, "No such file") {
		t.Errorf("expected file-not-found error in stderr, got: %s", result.Stderr)
	}
	t.Logf("stderr: %s", result.Stderr)
}
