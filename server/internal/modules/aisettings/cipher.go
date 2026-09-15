package aisettings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
)

const (
	cipherVersion byte = 1
	keyContext         = "weavepress/ai-provider-settings/v1"
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(mediaSigningKey string) (*Cipher, error) {
	if len(mediaSigningKey) < 32 {
		return nil, ErrCipherUnavailable
	}
	mac := hmac.New(sha256.New, []byte(mediaSigningKey))
	_, _ = mac.Write([]byte(keyContext))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, ErrCipherUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCipherUnavailable
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrCipherUnavailable
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrCipherUnavailable
	}
	result := make([]byte, 1, 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	result[0] = cipherVersion
	result = append(result, nonce...)
	result = c.aead.Seal(result, nonce, plaintext, []byte{cipherVersion})
	return result, nil
}

func (c *Cipher) Decrypt(encoded []byte) ([]byte, error) {
	if c == nil || c.aead == nil || len(encoded) < 1+c.aead.NonceSize()+c.aead.Overhead() || encoded[0] != cipherVersion {
		return nil, ErrDecryptFailed
	}
	nonceEnd := 1 + c.aead.NonceSize()
	plaintext, err := c.aead.Open(nil, encoded[1:nonceEnd], encoded[nonceEnd:], []byte{cipherVersion})
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}
