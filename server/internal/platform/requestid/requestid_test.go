package requestid

import (
	"context"
	"testing"
)

func TestNewGeneratesValidID(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if len(id) != 32 {
		t.Fatalf("len(id) = %d, want 32", len(id))
	}
	if !IsValid(id) {
		t.Fatalf("generated id is invalid: %q", id)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "simple", value: "abc123", want: true},
		{name: "with separators", value: "abc-123.def:456_789", want: true},
		{name: "empty", value: "", want: false},
		{name: "bad first char", value: "-abc", want: false},
		{name: "space", value: "abc 123", want: false},
		{name: "too long", value: "a1234567890123456789012345678901234567890123456789012345678901234", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.value); got != tt.want {
				t.Fatalf("IsValid(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithContext(context.Background(), "req-1")
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext ok = false")
	}
	if got != "req-1" {
		t.Fatalf("FromContext = %q, want req-1", got)
	}
}
