//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type checkoutRechargeAmountSettings struct {
	service.SettingRepository
	values map[string]string
}

func (s *checkoutRechargeAmountSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func TestGetCheckoutInfoReturnsConfiguredRechargeAmounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name string
		min  string
		max  string
		want []float64
	}{
		{name: "all presets", want: []float64{10, 20, 50, 100, 200, 500, 1000, 2000, 5000}},
		{name: "inclusive range", min: "20", max: "200", want: []float64{20, 50, 100, 200}},
		{name: "fractional range", min: "20.01", max: "199.99", want: []float64{50, 100}},
		{name: "no presets", min: "21", max: "49", want: []float64{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			_, err = db.Exec("PRAGMA foreign_keys = ON")
			require.NoError(t, err)
			client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
			t.Cleanup(func() { _ = client.Close() })
			configService := service.NewPaymentConfigService(client, &checkoutRechargeAmountSettings{values: map[string]string{
				service.SettingMinRechargeAmount: tt.min,
				service.SettingMaxRechargeAmount: tt.max,
			}}, nil)
			handler := NewPaymentHandler(nil, configService)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/checkout-info", nil)

			handler.GetCheckoutInfo(ctx)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var response struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			amountsJSON, ok := response.Data["recharge_amounts"]
			require.True(t, ok, "checkout-info must always include recharge_amounts")
			require.NotEqual(t, "null", string(amountsJSON), "an unavailable range must serialize as []")
			var amounts []float64
			require.NoError(t, json.Unmarshal(amountsJSON, &amounts))
			require.Equal(t, tt.want, amounts)
		})
	}
}
