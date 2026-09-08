package llm

import "context"

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
