package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxErrorResponseBytes = 64 << 10

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
	return &openAICompatible{
		endpoint: strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/v1/chat/completions",
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{Timeout: timeout},
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
		return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "无法生成 AI 模型请求", false, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "无法创建 AI 模型请求", false, err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Response{}, providerError("AI_PROVIDER_TIMEOUT", "AI 模型请求超时", true, err)
		}
		return Response{}, providerError("AI_PROVIDER_UNAVAILABLE", "AI 模型服务暂时不可用", true, err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		_, readErr := io.Copy(io.Discard, io.LimitReader(httpResponse.Body, maxErrorResponseBytes))
		cause := fmt.Errorf("AI 模型服务返回 HTTP %d", httpResponse.StatusCode)
		if readErr != nil {
			cause = fmt.Errorf("读取 AI 模型错误响应失败: %w", readErr)
		}
		switch {
		case httpResponse.StatusCode == http.StatusUnauthorized || httpResponse.StatusCode == http.StatusForbidden:
			return Response{}, providerError("AI_PROVIDER_AUTH_FAILED", "AI 模型认证失败", false, cause)
		case httpResponse.StatusCode == http.StatusTooManyRequests:
			return Response{}, providerError("AI_PROVIDER_RATE_LIMITED", "AI 模型请求受到限流", true, cause)
		case httpResponse.StatusCode >= http.StatusInternalServerError:
			return Response{}, providerError("AI_PROVIDER_UNAVAILABLE", "AI 模型服务暂时不可用", true, cause)
		default:
			return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "AI 模型拒绝了请求", false, cause)
		}
	}

	var decoded openAIResponse
	decoder := json.NewDecoder(httpResponse.Body)
	if err := decoder.Decode(&decoded); err != nil {
		return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "AI 模型返回了无效响应", false, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "AI 模型返回了无效响应", false, err)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "AI 模型返回了空响应", false, nil)
	}
	content := decoded.Choices[0].Message.Content
	if request.JSON && !json.Valid([]byte(content)) {
		return Response{}, providerError("AI_PROVIDER_REQUEST_FAILED", "AI 模型返回的内容不是有效 JSON", false, nil)
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

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("响应包含多个 JSON 值")
		}
		return err
	}
	return nil
}

func providerError(code, message string, retryable bool, cause error) error {
	return &Error{Code: code, Message: message, Retryable: retryable, Cause: cause}
}
