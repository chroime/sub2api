//go:build unit

package handler

import (
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestBillAmountPolicySurvivesAuthenticatedOrderResponse(t *testing.T) {
	for _, pk := range []string{"alipay_bill", "alipay"} {
		o := &dbent.PaymentOrder{ProviderKey: &pk, ProviderSnapshot: map[string]any{"bill_amount_matching": true, "privateKey": "never-return-this"}}
		result := sanitizePaymentOrderForResponse(o)
		require.Equal(t, pk == "alipay_bill", result.BillAmountMatching)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "never-return-this")
		if pk == "alipay_bill" {
			require.Contains(t, string(raw), `"bill_amount_matching":true`)
		}
	}
}
