package upstreamgovernance

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func connectorRSAFixture(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(der)
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), hex.EncodeToString(digest[:16])
}
func TestConnectorNewAPIEncryptedPasswordRoundTrip(t *testing.T) {
	key, public, kid := connectorRSAFixture(t)
	posts := 0
	password := "虚构-password-for-fixture"
	c := fixtureConnector(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/status":
			return 200, `{"success":true,"data":{}}`
		case "/api/user/login/encryption-key":
			body, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"enabled": true, "kid": kid, "public_key": public}})
			return 200, string(body)
		case "/api/user/login":
			posts++
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			if json.Unmarshal(body, &payload) != nil {
				t.Fatal("bad JSON")
			}
			if _, ok := payload["password"]; ok {
				t.Fatal("plaintext password field emitted")
			}
			if payload["encryption_key_id"] != kid || payload["username"] != "fixture" {
				t.Fatal("incorrect encryption identity")
			}
			ciphertext, e := base64.StdEncoding.DecodeString(payload["password_encrypted"])
			if e != nil {
				t.Fatal(e)
			}
			plain, e := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, ciphertext, nil)
			if e != nil || string(plain) != password {
				t.Fatalf("OAEP roundtrip failed %v", e)
			}
			return 200, `{"success":true,"data":{"access_token":"dashboard","user":{"id":42}}}`
		case "/api/user/self":
			return 200, `{"success":true,"data":{"id":42}}`
		}
		t.Fatalf("unexpected %s", r.URL)
		return 500, `{}`
	})
	session, ch, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: password})
	if e != nil || ch != nil || session.UserID != 42 || posts != 1 {
		t.Fatalf("session=%+v ch=%+v e=%v posts=%d", session, ch, e, posts)
	}
}
func TestConnectorNewAPIInvalidEncryptionNeverPosts(t *testing.T) {
	_, public, kid := connectorRSAFixture(t)
	for _, tc := range []struct{ name, public, kid, password string }{{"invalid_pem", "not-a-public-key", kid, "invented"}, {"kid_mismatch", public, strings.Repeat("0", 32), "invented"}, {"oversized_password", public, kid, strings.Repeat("x", 191)}, {"empty_password", public, kid, ""}, {"extra_pem", public + public, kid, "invented"}} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			c := fixtureConnector(t, func(r *http.Request) (int, string) {
				if r.Method == "POST" {
					posts++
					return 500, `{}`
				}
				if r.URL.Path == "/api/status" {
					return 200, `{"success":true,"data":{}}`
				}
				body, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"enabled": true, "kid": tc.kid, "public_key": tc.public}})
				return 200, string(body)
			})
			_, _, e := c.Login(context.Background(), Site{Platform: "newapi", BaseURL: "https://upstream.example"}, LoginInput{Username: "fixture", Password: tc.password})
			if e == nil || !errors.Is(e, ErrUnsupported) && !errors.Is(e, ErrInvalid) || posts != 0 {
				t.Fatalf("invalid encrypted flow e=%v posts=%d", e, posts)
			}
		})
	}
}
