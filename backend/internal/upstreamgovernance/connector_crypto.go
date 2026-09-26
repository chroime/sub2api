package upstreamgovernance

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"unicode/utf8"
)

// connectorEncryptPassword implements the pinned New API legacy RSA-OAEP
// password wire format (SPKI PEM, SHA-256/MGF1, empty label, standard Base64).
// It deliberately does not guess a hybrid envelope for an oversized password.
func connectorEncryptPassword(password, publicPEM, keyID string) (string, error) {
	if len(publicPEM) > 8192 || len(keyID) != 32 {
		return "", ErrUnsupported
	}
	block, rest := pem.Decode([]byte(publicPEM))
	if block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || strings.TrimSpace(string(rest)) != "" {
		return "", ErrUnsupported
	}
	parsed, e := x509.ParsePKIXPublicKey(block.Bytes)
	if e != nil {
		return "", ErrUnsupported
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N == nil || key.N.BitLen() < 2048 || key.N.BitLen() > 4096 || key.E != 65537 {
		return "", ErrUnsupported
	}
	digest := sha256.Sum256(block.Bytes)
	if keyID != hex.EncodeToString(digest[:16]) {
		return "", ErrUnsupported
	}
	if len(password) == 0 || !utf8.ValidString(password) || len([]byte(password)) > key.Size()-2*sha256.Size-2 {
		return "", ErrInvalid
	}
	ciphertext, e := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, []byte(password), nil)
	if e != nil {
		return "", ErrUnsupported
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
