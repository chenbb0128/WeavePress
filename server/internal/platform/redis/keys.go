package redis

import (
	"errors"
	"fmt"
	"strings"
)

var ErrEmptyKeyPart = errors.New("redis key part must not be empty")

type KeyBuilder struct {
	prefix string
}

func NewKeyBuilder(prefix string) KeyBuilder {
	return KeyBuilder{prefix: normalizeKeyPart(prefix)}
}

func (b KeyBuilder) Prefix() string {
	return b.prefix
}

func (b KeyBuilder) Build(parts ...string) (string, error) {
	keyParts := make([]string, 0, len(parts)+1)
	if b.prefix != "" {
		keyParts = append(keyParts, b.prefix)
	}

	for i, part := range parts {
		normalized := normalizeKeyPart(part)
		if normalized == "" {
			return "", fmt.Errorf("%w at index %d", ErrEmptyKeyPart, i)
		}
		keyParts = append(keyParts, normalized)
	}

	if len(keyParts) == 0 {
		return "", ErrEmptyKeyPart
	}
	return strings.Join(keyParts, ":"), nil
}

func (b KeyBuilder) MustBuild(parts ...string) string {
	key, err := b.Build(parts...)
	if err != nil {
		panic(err)
	}
	return key
}

func normalizeKeyPart(value string) string {
	return strings.Trim(strings.TrimSpace(value), ":")
}
