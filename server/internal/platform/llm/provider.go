package llm

import "context"

const (
	ErrorCodeAuthFailed    = "AI_PROVIDER_AUTH_FAILED"
	ErrorCodeRequestFailed = "AI_PROVIDER_REQUEST_FAILED"
	ErrorCodeRateLimited   = "AI_PROVIDER_RATE_LIMITED"
	ErrorCodeUnavailable   = "AI_PROVIDER_UNAVAILABLE"
	ErrorCodeTimeout       = "AI_PROVIDER_TIMEOUT"
)

type Message struct {
	Role    string
	Content string
}

type Request struct {
	Messages    []Message
	MaxTokens   int
	Temperature float64
	JSON        bool
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type Response struct {
	Content string
	Usage   Usage
}

type Provider interface {
	Complete(context.Context, Request) (Response, error)
}

type Error struct {
	Code      string
	Message   string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}
