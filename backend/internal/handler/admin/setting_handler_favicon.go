package admin

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

func validateSiteFaviconSize(value string) error {
	const maxImageBytes = 1 << 20
	value = strings.TrimSpace(value)
	// Data URLs include base64 expansion and a short MIME/encoding prefix.
	if len(value) > base64.StdEncoding.EncodedLen(maxImageBytes)+256 {
		return errors.New("Favicon image is too large (max 1 MiB)")
	}
	metadata, payload, hasPayload := strings.Cut(value, ",")
	if !hasPayload || !strings.HasPrefix(strings.ToLower(metadata), "data:image/") {
		return nil
	}
	var size int
	if strings.HasSuffix(strings.ToLower(metadata), ";base64") {
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return errors.New("Favicon image has invalid base64 data")
		}
		size = len(decoded)
	} else {
		decoded, err := url.PathUnescape(payload)
		if err != nil {
			return errors.New("Favicon image has invalid URL-encoded data")
		}
		size = len(decoded)
	}
	if size > maxImageBytes {
		return errors.New("Favicon image is too large (max 1 MiB)")
	}
	return nil
}
