package redis

import (
	"errors"
	"testing"
)

func TestKeyBuilderBuildsWithPrefix(t *testing.T) {
	builder := NewKeyBuilder(" weavepress:local: ")

	key, err := builder.Build(" users ", ":42:", "profile")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if key != "weavepress:local:users:42:profile" {
		t.Fatalf("Build() = %q", key)
	}
}

func TestKeyBuilderRejectsEmptyParts(t *testing.T) {
	builder := NewKeyBuilder("weavepress:test")

	_, err := builder.Build("users", " ")
	if !errors.Is(err, ErrEmptyKeyPart) {
		t.Fatalf("Build() error = %v, want ErrEmptyKeyPart", err)
	}
}

func TestKeyBuilderRejectsEmptyKey(t *testing.T) {
	builder := NewKeyBuilder("")

	_, err := builder.Build()
	if !errors.Is(err, ErrEmptyKeyPart) {
		t.Fatalf("Build() error = %v, want ErrEmptyKeyPart", err)
	}
}
