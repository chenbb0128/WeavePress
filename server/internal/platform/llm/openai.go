package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxErrorResponseBytes   int64 = 64 << 10
	maxSuccessResponseBytes int64 = 16 << 20
)

type openAICompatible struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	MaxTokens      int                   `json:"max_tokens"`
	Temperature    float64               `json:"temperature"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func NewOpenAICompatible(baseURL, apiKey, model string, timeout time.Duration) Provider {
	return newOpenAICompatibleWithClient(baseURL, apiKey, model, &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
}

func newOpenAICompatibleWithClient(baseURL, apiKey, model string, client *http.Client) Provider {
	return &openAICompatible{
		endpoint: openAIChatCompletionsEndpoint(baseURL),
		apiKey:   apiKey,
		model:    model,
		client:   client,
	}
}

func (p *openAICompatible) Complete(ctx context.Context, request Request) (Response, error) {
	messages := make([]openAIMessage, len(request.Messages))
	for index, message := range request.Messages {
		messages[index] = openAIMessage{Role: message.Role, Content: message.Content}
	}
	payload := openAIRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   request.MaxTokens,
		Temperature: request.Temperature,
	}
	if request.JSON {
		payload.ResponseFormat = &openAIResponseFormat{Type: "json_object"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, providerError(ErrorCodeRequestFailed, "无法生成 AI 模型请求", false, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, providerError(ErrorCodeRequestFailed, "无法创建 AI 模型请求", false, err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return Response{}, transportError(ctx, "AI 模型服务暂时不可用", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResponse.Body, maxErrorResponseBytes))
		cause := fmt.Errorf("AI 模型服务返回 HTTP %d", httpResponse.StatusCode)
		switch {
		case httpResponse.StatusCode == http.StatusUnauthorized || httpResponse.StatusCode == http.StatusForbidden:
			return Response{}, providerError(ErrorCodeAuthFailed, "AI 模型认证失败", false, cause)
		case httpResponse.StatusCode == http.StatusTooManyRequests:
			return Response{}, providerError(ErrorCodeRateLimited, "AI 模型请求受到限流", true, cause)
		case httpResponse.StatusCode >= http.StatusInternalServerError:
			return Response{}, providerError(ErrorCodeUnavailable, "AI 模型服务暂时不可用", true, cause)
		default:
			return Response{}, providerError(ErrorCodeRequestFailed, "AI 模型拒绝了请求", false, cause)
		}
	}

	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxSuccessResponseBytes+1))
	if err != nil {
		return Response{}, transportError(ctx, "读取 AI 模型响应失败", err)
	}
	if int64(len(responseBody)) > maxSuccessResponseBytes {
		return Response{}, providerError(ErrorCodeRequestFailed, "AI 模型响应超过大小限制", false, nil)
	}
	var decoded openAIResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Response{}, providerError(ErrorCodeRequestFailed, "AI 模型返回了无效响应", false, err)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return Response{}, providerError(ErrorCodeRequestFailed, "AI 模型返回了空响应", false, nil)
	}
	content := decoded.Choices[0].Message.Content
	if request.JSON && !json.Valid([]byte(content)) {
		return Response{}, providerError(ErrorCodeRequestFailed, "AI 模型返回的内容不是有效 JSON", false, nil)
	}

	return Response{
		Content: content,
		Usage: Usage{
			InputTokens:  decoded.Usage.PromptTokens,
			OutputTokens: decoded.Usage.CompletionTokens,
			TotalTokens:  decoded.Usage.TotalTokens,
		},
	}, nil
}

func openAIChatCompletionsEndpoint(baseURL string) string {
	normalized := strings.TrimSpace(baseURL)
	parsed, err := url.Parse(normalized)
	if err != nil {
		return strings.TrimRight(normalized, "/") + "/v1/chat/completions"
	}
	suffix := "/v1/chat/completions"
	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix
	if parsed.RawPath != "" {
		parsed.RawPath = strings.TrimRight(parsed.RawPath, "/") + suffix
	}
	return parsed.String()
}

func transportError(ctx context.Context, message string, cause error) error {
	switch {
	case errors.Is(cause, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return providerError(ErrorCodeTimeout, "AI 模型请求超时", true, cause)
	case errors.Is(cause, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return providerError(ErrorCodeRequestFailed, "AI 模型请求已取消", false, cause)
	default:
		return providerError(ErrorCodeUnavailable, message, true, cause)
	}
}

func providerError(code, message string, retryable bool, cause error) error {
	return &Error{Code: code, Message: message, Retryable: retryable, Cause: cause}
}
