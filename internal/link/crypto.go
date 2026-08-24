// Package link implements encrypted link handoff: a share zip is encrypted
// with AES-256-GCM, uploaded to a compatible link service or file provider,
// and the key travels in the URL fragment, never reaching the storage service.
package link

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// Crypto constants.
const (
	KeySize   = 32 // AES-256
	NonceSize = 12 // GCM standard
	KeyAlg    = "AES-256-GCM"
	KeyRef    = "url-fragment:k"
)

// ErrDecrypt is returned when the ciphertext fails authentication.
var ErrDecrypt = errors.New("decryption failed: ciphertext integrity check failed")

// EncryptResult is the output of Encrypt.
type EncryptResult struct {
	Ciphertext []byte
	Key        []byte // raw 32-byte key
	Nonce      []byte // raw 12-byte nonce
	NonceB64   string // base64url nonce for the manifest
	SHA256     string // hex sha256 of the ciphertext
}

// Encrypt seals the payload with a fresh random key and nonce.
func Encrypt(plaintext []byte) (*EncryptResult, error) {
	key := make([]byte, KeySize)
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	sum := sha256.Sum256(ct)
	return &EncryptResult{
		Ciphertext: ct,
		Key:        key,
		Nonce:      nonce,
		NonceB64:   base64.RawURLEncoding.EncodeToString(nonce),
		SHA256:     hex.EncodeToString(sum[:]),
	}, nil
}

// Decrypt opens the ciphertext with the given key and nonce.
func Decrypt(ciphertext, key, nonce []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("invalid key size %d", len(key))
	}
	if len(nonce) != NonceSize {
		return nil, fmt.Errorf("invalid nonce size %d", len(nonce))
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncodeKey renders a raw key as the base64url fragment value.
func EncodeKey(key []byte) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

// DecodeKey parses a base64url fragment value back into a raw key.
func DecodeKey(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
