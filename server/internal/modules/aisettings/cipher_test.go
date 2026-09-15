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
	left, err := cipher.Encrypt(ProviderZhipu, zhipuBaseURL, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := cipher.Encrypt(ProviderZhipu, zhipuBaseURL, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(left, right) {
		t.Fatal("ciphertexts must use different nonces")
	}
	plain, err := cipher.Decrypt(ProviderZhipu, zhipuBaseURL, left)
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
	encoded, err := cipher.Encrypt(ProviderZhipu, zhipuBaseURL, []byte("api-key-sensitive"))
	if err != nil {
		t.Fatal(err)
	}
	encoded[len(encoded)-1] ^= 1
	_, err = cipher.Decrypt(ProviderZhipu, zhipuBaseURL, encoded)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Decrypt() error = %v, want %v", err, ErrDecryptFailed)
	}
	if strings.Contains(err.Error(), "api-key-sensitive") {
		t.Fatalf("error leaked plaintext: %v", err)
	}
}

func TestCipherBindsCredentialToProviderAndBaseURL(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := cipher.Encrypt(ProviderOpenAI, openAIBaseURL, []byte("openai-key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		provider string
		baseURL  string
	}{
		{name: "provider", provider: ProviderOpenAICompatible, baseURL: openAIBaseURL},
		{name: "base URL", provider: ProviderOpenAI, baseURL: "https://attacker.example/v1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := cipher.Decrypt(test.provider, test.baseURL, encoded)
			if !errors.Is(err, ErrDecryptFailed) {
				t.Fatalf("Decrypt() error = %v, want %v", err, ErrDecryptFailed)
			}
		})
	}
}

func TestCipherRequiresStrongSigningKey(t *testing.T) {
	_, err := NewCipher("short")
	if !errors.Is(err, ErrCipherUnavailable) {
		t.Fatalf("NewCipher() error = %v, want %v", err, ErrCipherUnavailable)
	}
}
