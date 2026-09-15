package aisettings

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCipherRoundTripUsesRandomNonce(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	left, err := cipher.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := cipher.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(left, right) {
		t.Fatal("ciphertexts must use different nonces")
	}
	plain, err := cipher.Decrypt(left)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "secret" {
		t.Fatalf("plain = %q, want secret", plain)
	}
}

func TestCipherRejectsTamperingWithoutLeakingDetails(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := cipher.Encrypt([]byte("api-key-sensitive"))
	if err != nil {
		t.Fatal(err)
	}
	encoded[len(encoded)-1] ^= 1
	_, err = cipher.Decrypt(encoded)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Decrypt() error = %v, want %v", err, ErrDecryptFailed)
	}
	if strings.Contains(err.Error(), "api-key-sensitive") {
		t.Fatalf("error leaked plaintext: %v", err)
	}
}

func TestCipherRequiresStrongSigningKey(t *testing.T) {
	_, err := NewCipher("short")
	if !errors.Is(err, ErrCipherUnavailable) {
		t.Fatalf("NewCipher() error = %v, want %v", err, ErrCipherUnavailable)
	}
}
