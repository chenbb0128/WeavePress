package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

const (
	Header = "X-Request-ID"
	GinKey = "request_id"
)

type contextKey struct{}

var validPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

func New() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func IsValid(value string) bool {
	return validPattern.MatchString(value)
}

func FromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(contextKey{}).(string)
	return value, ok && value != ""
}

func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}
