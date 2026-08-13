package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultOpenAIBaseURL is the OpenAI REST API base URL.
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

// DefaultOpenAIModel is used when neither configuration nor the
// OPENAI_MODEL environment variable provides a model.
const DefaultOpenAIModel = "gpt-4o"

// OpenAIEnvAPIKey is the environment variable holding the OpenAI API key.
// v0.14.2 resolves keys from the environment; v0.14.5 replaces this with the
// CredentialStore.
const OpenAIEnvAPIKey = "OPENAI_API_KEY"

// OpenAIEnvModel optionally overrides the default model via environment.
const OpenAIEnvModel = "OPENAI_MODEL"

// OpenAIConfig holds construction parameters for OpenAIProvider.
// All fields are optional — zero values fall back to defaults.
type OpenAIConfig struct {
	BaseURL    string        // default DefaultOpenAIBaseURL
	APIKey     string        // default: OPENAI_API_KEY env
	Model      string        // default: OPENAI_MODEL env, then DefaultOpenAIModel
	Timeout    time.Duration // default 60s
	MaxRetries int           // reserved for shared retry layer (future)
	HTTPClient *http.Client  // override transport (tests, proxy, etc.)
}

// OpenAIProvider implements IProvider against the OpenAI Chat Completions
// API. It also serves as the shared core for OpenAI-compatible providers
// (DeepSeek, Groq, Mistral, Ollama, ...) via NewOpenAICompat, which only
// differs in base URL.
type OpenAIProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
}

var _ IProvider = (*OpenAIProvider)(nil)

// init registers the "openai" engine into the engine registry. The factory is
// parameterless by design: the provider self-bootstraps from configuration
// defaults and the OPENAI_API_KEY / OPENAI_MODEL environment variables.
func init() {
	_ = RegisterEngine("openai", func() (IProvider, error) {
		return NewOpenAI(OpenAIConfig{}), nil
	})
}

// NewOpenAI creates an OpenAIProvider from the given config, applying
// defaults for any zero fields.
func NewOpenAI(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultOpenAIBaseURL
	}
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.ExpandEnv(os.Getenv(OpenAIEnvAPIKey))
	}
	model := cfg.Model
	if model == "" {
		model = os.Getenv(OpenAIEnvModel)
	}
	if model == "" {
		model = DefaultOpenAIModel
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &OpenAIProvider{
		client:  client,
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		apiKey:  apiKey,
		model:   model,
	}
}

// ---------------------------------------------------------------------------
// IProvider
// ---------------------------------------------------------------------------

// Generate performs a non-streaming chat completion and returns the response.
func (p *OpenAIProvider) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("%w: %s not set", ErrAPIKeyMissing, OpenAIEnvAPIKey)
	}

	reqBody := buildChatRequest(p.model, messages, false)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Response{}, parseAPIError(resp)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return Response{}, fmt.Errorf("openai: decode response: %w", err)
	}
	return chatResponseToResponse(chatResp), nil
}

// Stream performs a streaming chat completion and returns a channel of
// chunks. The channel is closed when the stream completes or errors.
func (p *OpenAIProvider) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("%w: %s not set", ErrAPIKeyMissing, OpenAIEnvAPIKey)
	}

	reqBody := buildChatRequest(p.model, messages, true)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, parseAPIError(resp)
	}

	ch := make(chan StreamChunk, 100)
	go p.readSSE(ctx, resp.Body, ch)
	return ch, nil
}

// ModelInfo returns the static capability declaration of this provider.
func (p *OpenAIProvider) ModelInfo() ModelInfo {
	return ModelInfo{
		Name:           p.model,
		Provider:       "openai",
		DisplayName:    "OpenAI",
		ContextSize:    128000,
		SupportsTool:   true,
		SupportsVision: true,
		Models: []string{
			"gpt-4o", "gpt-4o-mini", "gpt-4-turbo",
			"o3-mini", "o4-mini",
		},
	}
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// doRequest sends the JSON body to /chat/completions and returns the raw
// response (caller must close Body).
func (p *OpenAIProvider) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: request: %w", err)
	}
	return resp, nil
}

// parseAPIError reads a non-2xx response body into a *ProviderError.
func parseAPIError(resp *http.Response) error {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	_ = json.Unmarshal(body, &apiErr)

	msg := apiErr.Error.Message
	if msg == "" {
		msg = strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
	}
	typ := apiErr.Error.Type
	if typ == "" {
		typ = apiErr.Error.Code
	}

	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	return &ProviderError{
		Code:      resp.StatusCode,
		Type:      typ,
		Message:   msg,
		Retryable: retryable,
	}
}

// ---------------------------------------------------------------------------
// SSE streaming
// ---------------------------------------------------------------------------

// readSSE reads the streaming response body line by line, parses "data: "
// events, and forwards chunks to ch until "[DONE]" or ctx cancellation.
func (p *OpenAIProvider) readSSE(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
	defer close(ch)
	defer body.(io.Closer).Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Streaming tool-call fragments are keyed by index and accumulated until
	// the arguments JSON is complete. The ToolCall object itself is carried
	// across events: the id/name arrive in the first fragment, the arguments
	// arrive split across subsequent fragments.
	toolBufs := make(map[int]*strings.Builder)
	ordered := make([]int, 0)
	var toolCall *ToolCall

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return
		}

		var evt streamEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		if len(evt.Choices) == 0 {
			continue
		}
		choice := evt.Choices[0]

		chunk := StreamChunk{Delta: choice.Delta.Content}

		for _, tc := range choice.Delta.ToolCalls {
			if toolCall == nil {
				toolCall = &ToolCall{}
			}
			if tc.ID != "" {
				toolCall.ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCall.Name = tc.Function.Name
			}
			buf, ok := toolBufs[tc.Index]
			if !ok {
				buf = &strings.Builder{}
				toolBufs[tc.Index] = buf
				ordered = append(ordered, tc.Index)
			}
			buf.WriteString(tc.Function.Arguments)
		}

		// Flush accumulated tool-call arguments once the stream finishes.
		if choice.FinishReason != "" {
			if toolCall != nil {
				if len(ordered) > 0 {
					buf := toolBufs[ordered[0]]
					args := make(map[string]any)
					if err := json.Unmarshal([]byte(buf.String()), &args); err == nil {
						toolCall.Args = args
					}
				}
				chunk.ToolCall = toolCall
			}
			chunk.Done = true
		}

		if chunk.Delta != "" || chunk.ToolCall != nil || chunk.Done {
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Request/response model and conversion
// ---------------------------------------------------------------------------

// chatRequest mirrors the OpenAI Chat Completions request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string          `json:"role"`
	Content string          `json:"content,omitempty"`
	Tools   []chatTool      `json:"-"`
	ToolID  string          `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string        `json:"type"`
	Function chatFunction  `json:"function"`
}

type chatFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// buildChatRequest converts []ProviderMessage into the OpenAI wire format.
// Tool definitions are hoisted from the last message that carries them into
// the top-level tools array (OpenAI requires tools at request level).
func buildChatRequest(model string, messages []ProviderMessage, stream bool) chatRequest {
	req := chatRequest{
		Model:    model,
		Messages: make([]chatMessage, 0, len(messages)),
		Stream:   stream,
	}

	for _, m := range messages {
		cm := chatMessage{Role: m.Role, Content: m.Content}
		req.Messages = append(req.Messages, cm)
		if len(m.Tools) > 0 && len(req.Tools) == 0 {
			for _, t := range m.Tools {
				req.Tools = append(req.Tools, chatTool{
					Type: "function",
					Function: chatFunction{
						Name:        t.Name,
						Description: t.Description,
						Parameters:  t.Parameters,
					},
				})
			}
		}
	}
	return req
}

// chatResponse mirrors the OpenAI Chat Completions response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// chatResponseToResponse converts the decoded non-streaming response into a
// provider.Response.
func chatResponseToResponse(cr chatResponse) Response {
	resp := Response{
		FinishReason: "stop",
		Usage: Usage{
			PromptTokens:     cr.Usage.PromptTokens,
			CompletionTokens: cr.Usage.CompletionTokens,
			TotalTokens:      cr.Usage.TotalTokens,
		},
	}
	if len(cr.Choices) > 0 {
		choice := cr.Choices[0]
		resp.Content = choice.Message.Content
		if choice.FinishReason != "" {
			resp.FinishReason = choice.FinishReason
		}
		for _, tc := range choice.Message.ToolCalls {
			args := make(map[string]any)
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{"_raw": tc.Function.Arguments}
			}
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			})
		}
	}
	return resp
}

// streamEvent mirrors a single SSE chat.completion.chunk event.
type streamEvent struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
