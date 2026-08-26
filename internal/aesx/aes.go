package aesx

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
)

// This package mirrors the Java CryptoUtil used for OSS AK/SK in Apollo:
//
//	algorithm: AES/GCM/NoPadding
//	tag bits:  128 (Java GCMParameterSpec(128, ivBytes))
//	key:       secret-key string as UTF-8 bytes (16/24/32 bytes)
//	iv:        iv string as UTF-8 bytes, passed straight to GCM
//	cipher:    base64 string

var ErrInvalidKey = errors.New("secret key must be 16, 24 or 32 bytes (UTF-8)")

func gcm(secretKey, iv string) (cipher.AEAD, []byte, error) {
	kb := []byte(secretKey)
	switch len(kb) {
	case 16, 24, 32:
	default:
		return nil, nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKey, len(kb))
	}
	ivb := []byte(iv)
	if len(ivb) == 0 {
		return nil, nil, errors.New("iv must not be empty")
	}
	block, err := aes.NewCipher(kb)
	if err != nil {
		return nil, nil, err
	}
	g, err := cipher.NewGCMWithNonceSize(block, len(ivb))
	if err != nil {
		return nil, nil, err
	}
	return g, ivb, nil
}

// Encrypt mirrors CryptoUtil.aesEncrypt: returns base64 ciphertext.
func Encrypt(secretKey, iv, plaintext string) (string, error) {
	g, ivb, err := gcm(secretKey, iv)
	if err != nil {
		return "", err
	}
	ct := g.Seal(nil, ivb, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt mirrors CryptoUtil.aesDecrypt: base64 decode then AES/GCM decrypt.
func Decrypt(secretKey, iv, ciphertext string) (string, error) {
	g, ivb, err := gcm(secretKey, iv)
	if err != nil {
		return "", err
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid base64 ciphertext: %w", err)
	}
	pt, err := g.Open(nil, ivb, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed (wrong key/iv or corrupted ciphertext): %w", err)
	}
	return string(pt), nil
}
