package link

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// DecodeNonce parses a base64url nonce from the manifest.
func DecodeNonce(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func sha256Sum(b []byte) [32]byte {
	return sha256.Sum256(b)
}

func sha256hex(b []byte) string {
	sum := sha256Sum(b)
	return hex.EncodeToString(sum[:])
}
