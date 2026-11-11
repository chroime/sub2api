package handler

import (
	"io"
	"mime"
	"net/http"
	"net/url"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

const builtinEasyPayMaxRequestBytes = 16 * 1024

// readBuiltinEasyPayParams decodes each transport exactly once. Legacy EasyPay
// queries send identical values in URL and body; disagreement or duplicate keys
// within either transport is rejected instead of choosing an ambiguous value.
func readBuiltinEasyPayParams(c *gin.Context) (map[string]string, error) {
	invalid := func() error {
		return infraerrors.BadRequest("BUILTIN_EPAY_INVALID_REQUEST", "invalid, duplicated, or oversized request parameters")
	}
	if len(c.Request.URL.RawQuery) > builtinEasyPayMaxRequestBytes {
		return nil, invalid()
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, invalid()
	}
	values := make(map[string]string)
	merge := func(source url.Values) error {
		for k, v := range source {
			if len(v) != 1 {
				return invalid()
			}
			if previous, ok := values[k]; ok && previous != v[0] {
				return invalid()
			}
			values[k] = v[0]
		}
		if len(values) > 20 {
			return invalid()
		}
		return nil
	}
	if err := merge(query); err != nil {
		return nil, err
	}
	if c.Request.Method == http.MethodPost {
		contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || contentType != "application/x-www-form-urlencoded" {
			return nil, invalid()
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, builtinEasyPayMaxRequestBytes+1))
		if err != nil || len(body) > builtinEasyPayMaxRequestBytes {
			return nil, invalid()
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, invalid()
		}
		if err := merge(form); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func writeBuiltinEasyPayError(c *gin.Context, err error) {
	message := "gateway temporarily unavailable"
	if infraerrors.Code(err) < 500 {
		message = infraerrors.Message(err)
	}
	// EasyPay clients expect a top-level protocol code, not Sub2API's data envelope.
	// Never echo request values, query strings, credentials, or raw upstream errors.
	c.JSON(http.StatusOK, gin.H{"code": -1, "msg": message})
}

func (h *PaymentHandler) BuiltinEasyPayCreate(c *gin.Context) {
	params, err := readBuiltinEasyPayParams(c)
	if err != nil {
		writeBuiltinEasyPayError(c, err)
		return
	}
	result, err := h.paymentService.CreateBuiltinEasyPayOrder(c.Request.Context(), params)
	if err != nil {
		writeBuiltinEasyPayError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *PaymentHandler) BuiltinEasyPayAPI(c *gin.Context) {
	params, err := readBuiltinEasyPayParams(c)
	if err != nil {
		writeBuiltinEasyPayError(c, err)
		return
	}
	if params["act"] != "order" {
		writeBuiltinEasyPayError(c, infraerrors.BadRequest("BUILTIN_EPAY_UNSUPPORTED_ACTION", "only order queries are supported; refunds are manual"))
		return
	}
	result, err := h.paymentService.QueryBuiltinEasyPayOrder(c.Request.Context(), params)
	if err != nil {
		writeBuiltinEasyPayError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
