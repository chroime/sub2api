// Package epay implements the signing convention shared by EasyPay-compatible
// gateways. Request field validation belongs to each protocol endpoint.
package epay

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strings"
)

// Sign signs decoded parameter values using the standard EasyPay MD5 format.
func Sign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k, value := range params {
		if k != "sign" && k != "sign_type" && value != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var base strings.Builder
	for i, k := range keys {
		if i > 0 {
			base.WriteByte('&')
		}
		base.WriteString(k)
		base.WriteByte('=')
		base.WriteString(params[k])
	}
	base.WriteString(key)
	digest := md5.Sum([]byte(base.String()))
	return hex.EncodeToString(digest[:])
}

func Verify(params map[string]string, key, signature string) bool {
	return len(signature) == 32 && hmac.Equal([]byte(Sign(params, key)), []byte(signature))
}
