package accountquality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type IdentityInput struct {
	ID, ParentAccountID       int64
	Platform, Type            string
	Credentials, Extra, Proxy map[string]any
	ProxyID                   *int64
}

// Fingerprint never returns credentials. Routine OAuth token rotation and
// observation-only extra updates do not change a known upstream identity.
func Fingerprint(in IdentityInput) string {
	credentials := map[string]any{}
	stable := false
	for _, key := range []string{"chatgpt_account_id", "account_id", "user_id", "email", "organization_id", "client_email", "identity_id"} {
		if v, ok := in.Credentials[key]; ok && v != "" && v != nil {
			stable = true
		}
	}
	for key, value := range in.Credentials {
		if in.Type != "apikey" && stable {
			switch key {
			case "access_token", "refresh_token", "id_token", "expires_at", "expires_in", "token_expiry", "_token_version", "last_refresh_at", "token_type":
				continue
			}
		}
		credentials[key] = value
	}
	extra := map[string]any{}
	for key, value := range in.Extra {
		if key == "model_mapping" || key == "header_overrides" || key == "openai_passthrough" || key == "openai_oauth_passthrough" || key == "openai_responses_mode" || key == "openai_responses_supported" || strings.HasPrefix(key, "tls_") || strings.HasPrefix(key, "http_") {
			extra[key] = value
		}
	}
	raw, _ := json.Marshal(struct {
		ID, ParentID              int64
		Platform, Type            string
		Credentials, Extra, Proxy map[string]any
		ProxyID                   *int64
	}{in.ID, in.ParentAccountID, in.Platform, in.Type, credentials, extra, in.Proxy, in.ProxyID})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
