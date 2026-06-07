package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type LLMClient struct {
	APIKey  string
	APIBase string
	Model   string
	client  *http.Client
}

func NewLLMClient(apiKey, apiBase, model string) *LLMClient {
	return &LLMClient{
		APIKey:  apiKey,
		APIBase: apiBase,
		Model:   model,
		client: &http.Client{
			Timeout: 0,
		},
	}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

type ToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type AssistantMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type ToolResultMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}

type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Parameters  Parameters `json:"parameters"`
	} `json:"function"`
}

type Parameters struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []any     `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Role             string     `json:"role"`
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Role             string     `json:"role"`
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type StreamEvent struct {
	Type     string     `json:"type"`
	Content  string     `json:"content,omitempty"`
	ToolCall *ToolCall  `json:"tool_call,omitempty"`
	Done     bool       `json:"done"`
	Error    string     `json:"error,omitempty"`
	Messages []any      `json:"-"`
}

func RunPythonToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: struct {
			Name        string     `json:"name"`
			Description string     `json:"description"`
			Parameters  Parameters `json:"parameters"`
		}{
			Name:        "run_python",
			Description: "Execute Python code in a sandbox to analyze the dataset. The sandbox has pandas, numpy, matplotlib, seaborn, and scikit-learn. Use print() for output. Save plots with plt.savefig('name.png'). The dataset CSV is in the current directory.",
			Parameters: Parameters{
				Type: "object",
				Properties: map[string]Property{
					"code": {
						Type:        "string",
						Description: "Python code to execute in the sandbox",
					},
				},
				Required: []string{"code"},
			},
		},
	}
}

func (c *LLMClient) Chat(ctx context.Context, messages []any, tools []ToolDef) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := c.APIBase + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, bodyStr)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w, body: %s", err, string(body[:min(len(body), 500)]))
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("API error: %s (%s)", chatResp.Error.Message, chatResp.Error.Type)
	}

	return &chatResp, nil
}

func (c *LLMClient) ChatStream(ctx context.Context, messages []any, tools []ToolDef) (<-chan StreamEvent, error) {
	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := c.APIBase + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, bodyStr)
	}

	ch := make(chan StreamEvent, 64)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var (
			contentBuf    strings.Builder
			reasoningBuf  strings.Builder
			toolCallMap   = make(map[int]*ToolCall)
		)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if line == "data: [DONE]" {
				ch <- StreamEvent{Type: "done", Done: true}
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			jsonStr := strings.TrimPrefix(line, "data: ")
			var cr ChatResponse
			if err := json.Unmarshal([]byte(jsonStr), &cr); err != nil {
				continue
			}

			if len(cr.Choices) == 0 {
				continue
			}

			delta := cr.Choices[0].Delta

			if delta.Content != "" {
				contentBuf.WriteString(delta.Content)
				ch <- StreamEvent{Type: "text", Content: delta.Content}
			}

			if delta.ReasoningContent != "" {
				reasoningBuf.WriteString(delta.ReasoningContent)
			}

			for _, tc := range delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}

				existing, exists := toolCallMap[idx]
				if !exists {
					existing = &ToolCall{
						Type: "function",
					}
					toolCallMap[idx] = existing
				}

				if tc.ID != "" {
					existing.ID = tc.ID
				}
				if tc.Function.Name != "" {
					existing.Function.Name = tc.Function.Name
				}
				existing.Function.Arguments += tc.Function.Arguments
			}
		}

		var toolCalls []ToolCall
		for i := 0; ; i++ {
			if tc, ok := toolCallMap[i]; ok {
				toolCalls = append(toolCalls, *tc)
			} else {
				break
			}
		}

		ch <- StreamEvent{
			Type:     "final",
			Content:  contentBuf.String(),
			ToolCall: nil,
			Done:     true,
			Messages: []any{
				AssistantMessage{
					Role:             "assistant",
					Content:          contentBuf.String(),
					ReasoningContent: reasoningBuf.String(),
					ToolCalls:        toolCalls,
				},
			},
		}
	}()

	return ch, nil
}
