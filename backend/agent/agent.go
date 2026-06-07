package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/churka/llm-analytics/security"
)

type DataAgent struct {
	LLM     *LLMClient
	Sandbox *Sandbox
}

func NewDataAgent(llm *LLMClient, sandbox *Sandbox) *DataAgent {
	return &DataAgent{
		LLM:     llm,
		Sandbox: sandbox,
	}
}

type AnalysisResult struct {
	Report string
	Charts []string
}

type StreamCallback func(event StreamEvent)

func (a *DataAgent) AnalyzeStream(ctx context.Context, summary, instructions, filename string, cb StreamCallback) (*AnalysisResult, error) {
	systemPrompt := security.BuildSystemPrompt(security.PromptBuilder{
		DatasetSummary:   summary,
		UserInstructions: instructions,
		DatasetFilename:  filename,
	})

	messages := []any{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": "Please analyze this dataset thoroughly. Start by exploring the data with run_python, then perform deeper analysis, create visualizations, and provide insights."},
	}

	tools := []ToolDef{RunPythonToolDef()}
	var allCharts []string

	maxIterations := 20
	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		cb(StreamEvent{Type: "status", Content: fmt.Sprintf("Thinking... (step %d/%d)", i+1, maxIterations)})

		forceFinalTools := tools
		if i >= 15 {
			cb(StreamEvent{Type: "status", Content: "Time is running out — producing final report..."})
			forceFinalTools = nil
			messages = append(messages,
				map[string]string{
					"role":    "user",
					"content": "STOP calling tools. You MUST now produce your final analysis report with all findings, metrics, and insights you've gathered so far. Use proper markdown formatting with tables and bullet points. Do NOT call run_python again.",
				},
			)
		}

		log.Printf("[Agent] Iteration %d: sending to LLM (stream, %d messages)", i+1, len(messages))
		streamCh, err := a.LLM.ChatStream(ctx, messages, forceFinalTools)
		if err != nil {
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		var assistantMsg AssistantMessage
		var finalEvent StreamEvent

		for event := range streamCh {
			if event.Type == "text" {
				cb(event)
			}
			if event.Type == "final" {
				finalEvent = event
				if len(event.Messages) > 0 {
					if am, ok := event.Messages[0].(AssistantMessage); ok {
						assistantMsg = am
					}
				}
			}
		}

		if len(assistantMsg.ToolCalls) > 0 {
			cb(StreamEvent{Type: "status", Content: "Running Python code..."})

			messages = append(messages, assistantMsg)

			for _, tc := range assistantMsg.ToolCalls {
				if tc.Function.Name != "run_python" {
					result := ToolResultMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Error: unknown tool '%s'. Only run_python is available.", tc.Function.Name),
					}
					messages = append(messages, result)
					continue
				}

				var args struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					result := ToolResultMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Error: invalid arguments: %v", err),
					}
					messages = append(messages, result)
					continue
				}

				log.Printf("[Agent] Python code:\n%s", args.Code)
				codeResult, err := a.Sandbox.Execute(ctx, args.Code)
				if err != nil {
					log.Printf("[Agent] Sandbox error: %v", err)
					result := ToolResultMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Error executing code: %v", err),
					}
					messages = append(messages, result)
					continue
				}

				resultText := strings.Builder{}
				if codeResult.Stdout != "" {
					resultText.WriteString("STDOUT:\n")
					resultText.WriteString(codeResult.Stdout)
					resultText.WriteString("\n")
				}
				if codeResult.Stderr != "" {
					resultText.WriteString("STDERR:\n")
					resultText.WriteString(codeResult.Stderr)
					resultText.WriteString("\n")
				}
				if len(codeResult.Charts) > 0 {
					resultText.WriteString("Generated charts: ")
					resultText.WriteString(strings.Join(codeResult.Charts, ", "))
					resultText.WriteString("\n")
					allCharts = append(allCharts, codeResult.Charts...)
				}
				if !codeResult.Success {
					resultText.WriteString(fmt.Sprintf("Exit code: %d\n", codeResult.ExitCode))
				}

				log.Printf("[Agent] Result: stdout=%d stderr=%d charts=%v success=%v",
					len(codeResult.Stdout), len(codeResult.Stderr), codeResult.Charts, codeResult.Success)
				if codeResult.Stderr != "" {
					log.Printf("[Agent] STDERR: %s", codeResult.Stderr)
				}

				toolResult := ToolResultMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    resultText.String(),
				}
				messages = append(messages, toolResult)

				cb(StreamEvent{Type: "status", Content: "Analyzing results..."})
			}
			continue
		}

		if finalEvent.Content != "" {
			cb(StreamEvent{Type: "done", Done: true})
			charts := uniqueCharts(allCharts)
			return &AnalysisResult{
				Report: finalEvent.Content,
				Charts: charts,
			}, nil
		}

		return nil, fmt.Errorf("LLM returned empty response")
	}

	return nil, fmt.Errorf("exceeded maximum iterations (%d)", maxIterations)
}

func uniqueCharts(charts []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, c := range charts {
		if !seen[c] {
			seen[c] = true
			result = append(result, c)
		}
	}
	return result
}

func (a *DataAgent) Analyze(ctx context.Context, summary, instructions, filename string) (*AnalysisResult, error) {
	systemPrompt := security.BuildSystemPrompt(security.PromptBuilder{
		DatasetSummary:   summary,
		UserInstructions: instructions,
		DatasetFilename:  filename,
	})

	messages := []any{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": "Please analyze this dataset thoroughly. Start by exploring the data with run_python, then perform deeper analysis, create visualizations, and provide insights."},
	}

	tools := []ToolDef{RunPythonToolDef()}

	maxIterations := 20
	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		forceFinalTools := tools
		if i >= 15 {
			forceFinalTools = nil
			messages = append(messages,
				map[string]string{
					"role":    "user",
					"content": "STOP calling tools. You MUST now produce your final analysis report with all findings, metrics, and insights you've gathered so far. Use proper markdown formatting with tables and bullet points. Do NOT call run_python again.",
				},
			)
		}

		log.Printf("[Agent] Iteration %d: sending to LLM (%d messages)", i+1, len(messages))
		resp, err := a.LLM.Chat(ctx, messages, forceFinalTools)
		if err != nil {
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("no choices in response")
		}

		choice := resp.Choices[0]
		msg := choice.Message

		if len(msg.ToolCalls) > 0 {
			toolCallMsg := AssistantMessage{
				Role:             "assistant",
				Content:          msg.Content,
				ReasoningContent: msg.ReasoningContent,
				ToolCalls:        msg.ToolCalls,
			}
			messages = append(messages, toolCallMsg)

			for _, tc := range msg.ToolCalls {
				if tc.Function.Name != "run_python" {
					result := ToolResultMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Error: unknown tool '%s'. Only run_python is available.", tc.Function.Name),
					}
					messages = append(messages, result)
					continue
				}

				var args struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					result := ToolResultMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Error: invalid arguments: %v", err),
					}
					messages = append(messages, result)
					continue
				}

				log.Printf("[Agent] Python code:\n%s", args.Code)
				codeResult, err := a.Sandbox.Execute(ctx, args.Code)
				if err != nil {
					log.Printf("[Agent] Sandbox error: %v", err)
					result := ToolResultMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Error executing code: %v", err),
					}
					messages = append(messages, result)
					continue
				}

				resultText := strings.Builder{}
				if codeResult.Stdout != "" {
					resultText.WriteString("STDOUT:\n")
					resultText.WriteString(codeResult.Stdout)
					resultText.WriteString("\n")
				}
				if codeResult.Stderr != "" {
					resultText.WriteString("STDERR:\n")
					resultText.WriteString(codeResult.Stderr)
					resultText.WriteString("\n")
				}
				if len(codeResult.Charts) > 0 {
					resultText.WriteString("Generated charts: ")
					resultText.WriteString(strings.Join(codeResult.Charts, ", "))
					resultText.WriteString("\n")
				}
				if !codeResult.Success {
					resultText.WriteString(fmt.Sprintf("Exit code: %d\n", codeResult.ExitCode))
				}

				log.Printf("[Agent] Result: stdout=%d stderr=%d charts=%v success=%v",
				len(codeResult.Stdout), len(codeResult.Stderr), codeResult.Charts, codeResult.Success)
			if codeResult.Stderr != "" {
				log.Printf("[Agent] STDERR: %s", codeResult.Stderr)
			}

				toolResult := ToolResultMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    resultText.String(),
				}
				messages = append(messages, toolResult)
			}
			continue
		}

		if msg.Content != "" {
			charts := collectCharts(messages)
			return &AnalysisResult{
				Report: msg.Content,
				Charts: charts,
			}, nil
		}

		return nil, fmt.Errorf("LLM returned empty response")
	}

	return nil, fmt.Errorf("exceeded maximum iterations (%d)", maxIterations)
}

func collectCharts(messages []any) []string {
	seen := make(map[string]bool)
	var charts []string

	for _, m := range messages {
		if tr, ok := m.(ToolResultMessage); ok {
			content := tr.Content
			if idx := strings.Index(content, "Generated charts:"); idx >= 0 {
				chartList := content[idx+len("Generated charts:"):]
				if endIdx := strings.Index(chartList, "\n"); endIdx >= 0 {
					chartList = chartList[:endIdx]
				}
				for _, name := range strings.Split(chartList, ",") {
					name = strings.TrimSpace(name)
					if name != "" && !seen[name] {
						seen[name] = true
						charts = append(charts, name)
					}
				}
			}
		}
	}

	return charts
}
