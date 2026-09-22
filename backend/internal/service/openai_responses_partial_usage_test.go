//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise the public forwarding boundary: the stream readers already retained
// usage, but their callers discarded it whenever the upstream stream failed.
func TestOpenAIResponsesForwardPreservesAuthoritativeUsageOnFailure(t *testing.T) {
	const output = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
	const failedPrefix = `data: {"type":"response.failed","response":{"id":"resp_partial","status":"failed","error":{"code":"server_error","message":"stream failed"}`
	for _, passthrough := range []bool{false, true} {
		mode := map[bool]string{false: "native", true: "passthrough"}[passthrough]
		for _, tt := range []struct {
			name       string
			stream     string
			readErr    error
			wantResult bool
			wantInput  int
			wantOutput int
		}{
			{
				name:       "failed-with-usage",
				stream:     output + failedPrefix + `,"usage":{"input_tokens":7,"output_tokens":2}}}` + "\n\n",
				wantResult: true, wantInput: 7, wantOutput: 2,
			},
			{
				name:       "failed-with-authoritative-zero",
				stream:     failedPrefix + `,"usage":{"input_tokens":0,"output_tokens":0}}}` + "\n\n",
				wantResult: true,
			},
			{
				name:   "failed-missing-usage",
				stream: output + failedPrefix + `}}` + "\n\n",
			},
			{
				name:   "failed-null-usage",
				stream: output + failedPrefix + `,"usage":null}}` + "\n\n",
			},
			{
				name:   "failed-empty-usage",
				stream: output + failedPrefix + `,"usage":{}}}` + "\n\n",
			},
			{
				name:   "failed-invalid-token-counts",
				stream: output + failedPrefix + `,"usage":{"input_tokens":"unknown","output_tokens":null}}}` + "\n\n",
			},
			{
				name:   "failed-negative-token-counts",
				stream: output + failedPrefix + `,"usage":{"input_tokens":-1,"output_tokens":0}}}` + "\n\n",
			},
			{
				name:   "failed-fractional-token-counts",
				stream: output + failedPrefix + `,"usage":{"input_tokens":0.5,"output_tokens":0}}}` + "\n\n",
			},
			{
				name:   "failed-progressive-zero-is-not-final-usage",
				stream: `data: {"type":"response.in_progress","response":{"usage":{"input_tokens":0,"output_tokens":0}}}` + "\n\n" + output + failedPrefix + `}}` + "\n\n",
			},
			{
				name: "failed-invalid-final-usage-preserves-earlier-counts",
				stream: output + `data: {"type":"response.in_progress","response":{"id":"resp_partial","usage":{"input_tokens":11,"output_tokens":3}}}` + "\n\n" +
					failedPrefix + `,"usage":{"input_tokens":"unknown","output_tokens":1}}}` + "\n\n",
				wantResult: true, wantInput: 11, wantOutput: 3,
			},
			{
				name:       "failed-with-chat-compatible-usage",
				stream:     output + failedPrefix + `,"usage":{"prompt_tokens":8,"completion_tokens":3}}}` + "\n\n",
				wantResult: true, wantInput: 8, wantOutput: 3,
			},
			{
				name: "bare-error-drains-authoritative-failed-usage",
				stream: output + `data: {"type":"error","error":{"code":"server_error","message":"stream failed"}}` + "\n\n" +
					failedPrefix + `,"usage":{"input_tokens":9,"output_tokens":4}}}` + "\n\n",
				wantResult: true, wantInput: 9, wantOutput: 4,
			},
			{
				name:   "bare-error-missing-usage",
				stream: output + `data: {"type":"error","error":{"code":"rate_limit_exceeded","message":"upstream limit"}}` + "\n\ndata: [DONE]\n\n",
			},
			{
				name:    "interrupted-after-usage",
				stream:  output + `data: {"type":"response.in_progress","response":{"id":"resp_partial","usage":{"input_tokens":11,"output_tokens":3}}}` + "\n\n",
				readErr: io.ErrUnexpectedEOF, wantResult: true, wantInput: 11, wantOutput: 3,
			},
			{
				name:    "interrupted-missing-usage",
				stream:  output,
				readErr: io.ErrUnexpectedEOF,
			},
			{
				name:   "failover-before-output-retains-no-billable-result",
				stream: failedPrefix + `,"usage":{"input_tokens":7,"output_tokens":0}}}` + "\n\n",
			},
		} {
			t.Run(mode+"/"+tt.name, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.1","instructions":"test","input":"hello","stream":true}`)
				c := newOpenAIRejectedFieldTestContext(body)
				account := newOpenAIRejectedFieldTestAccount()
				account.Extra["openai_passthrough"] = passthrough
				stream := tt.stream
				if tt.name == "failed-with-authoritative-zero" {
					// Policy rejection is final rather than replayable. A genuine zero
					// usage must still complete settlement and release its reservation.
					stream = strings.ReplaceAll(stream, "server_error", "content_policy")
				}
				var upstreamBody io.ReadCloser = io.NopCloser(strings.NewReader(stream))
				if tt.readErr != nil {
					upstreamBody = &passthroughFlushTestErrorBody{payload: []byte(stream), err: tt.readErr}
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": {"text/event-stream"},
						"X-Request-Id": {"req_partial_usage"},
					},
					Body: upstreamBody,
				}}
				result, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), c, account, body)
				require.Error(t, err)
				if !tt.wantResult {
					require.Nil(t, result, "unknown consumption and retryable attempts must not be submitted as zero usage")
					if strings.HasPrefix(tt.name, "failover-") {
						var failover *UpstreamFailoverError
						require.ErrorAs(t, err, &failover)
					}
					return
				}
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover))
				require.NotNil(t, result, "authoritative partial usage must reach the existing handler's error billing path")
				require.NotNil(t, result.UsagePresent)
				require.True(t, *result.UsagePresent)
				require.Equal(t, tt.wantInput, result.Usage.InputTokens)
				require.Equal(t, tt.wantOutput, result.Usage.OutputTokens)
				require.Equal(t, "req_partial_usage", result.RequestID)
				require.Equal(t, "resp_partial", result.ResponseID)
				require.Equal(t, "gpt-5.1", result.Model)
				require.True(t, result.Stream)
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}

func TestOpenAIResponsesMissingUsagePreservesNormalBilling(t *testing.T) {
	billingErr := errors.New("billing unavailable")
	for _, tt := range []struct {
		name       string
		perRequest bool
		image      bool
		wantCost   float64
		billingErr error
	}{
		{name: "zero-token-zero-cost"},
		{name: "configured-per-request", perRequest: true, wantCost: .25},
		{name: "image-only", image: true, wantCost: .25},
		{name: "financial-failure-is-preserved", perRequest: true, wantCost: .25, billingErr: billingErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usage := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{err: tt.billingErr}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usage, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			price := .25
			groupID := int64(3)
			group := &Group{ID: groupID, RateMultiplier: 1}
			usagePresent := false
			result := &OpenAIForwardResult{
				RequestID: "req_missing_usage_" + tt.name, Model: "gpt-5.1", UsagePresent: &usagePresent, Stream: true,
			}
			if tt.perRequest {
				group.ModelPricing = []ChannelModelPricing{{
					Models: []string{"gpt-5.1"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price,
				}}
			}
			if tt.image {
				group.ImagePrice1K = &price
				result.Model = "gpt-image-2"
				result.ImageCount = 1
				result.ImageSize = "1K"
			}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				APIKey: &APIKey{ID: 2, GroupID: &groupID, Group: group}, User: &User{ID: 1}, Account: &Account{ID: 4}, Result: result,
			})
			if tt.billingErr != nil {
				require.ErrorIs(t, err, tt.billingErr, "only the actual billing failure must be propagated")
			} else {
				require.NoError(t, err, "missing upstream token usage must still follow the existing cost calculation")
			}
			if tt.wantCost == 0 {
				require.Zero(t, billing.calls)
				require.Zero(t, usage.calls, "zero usage and zero cost must not create a phantom usage row")
				return
			}
			require.Equal(t, 1, billing.calls)
			require.NotNil(t, billing.lastCmd)
			require.InDelta(t, tt.wantCost, billing.lastCmd.BalanceCost, 1e-12)
			require.Zero(t, billing.lastCmd.InputTokens)
			require.Zero(t, billing.lastCmd.OutputTokens)
			if tt.billingErr != nil {
				require.Equal(t, 1, usage.calls)
				require.InDelta(t, tt.wantCost, usage.lastLog.TotalCost, 1e-12)
				require.Zero(t, usage.lastLog.ActualCost, "retain the normal failed-bill record without claiming a successful deduction")
				return
			}
			require.Equal(t, 1, usage.calls)
			require.InDelta(t, tt.wantCost, usage.lastLog.ActualCost, 1e-12)
		})
	}
}

func TestOpenAIResponsesUsagePresenceDoesNotBlockZeroCostRelease(t *testing.T) {
	for _, usagePresent := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-usage", true: "explicit-zero"}[usagePresent], func(t *testing.T) {
			ctx, _, wallet, key := prechargeFixture(.2)
			require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
			StartBalancePrechargeUpstream(ctx)
			ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
			usage := &openAIRecordUsageLogRepoStub{}
			billing := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usage, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
				APIKey: key, User: key.User, Account: &Account{ID: 4},
				Result: &OpenAIForwardResult{
					Model: "gpt-5.1", UsagePresent: &usagePresent,
					RequestID: "req_usage_presence", Stream: true, UpstreamTerminalEvent: "response.failed",
				},
			})
			FinishBalancePrecharge(ctx)
			require.NoError(t, err)
			require.InDelta(t, .2, wallet.balance, 1e-8)
			require.Empty(t, wallet.holds)
			require.Zero(t, billing.calls)
			require.Zero(t, usage.calls)
		})
	}
}

func TestOpenAIResponsesFailoverPreservesNormalBillingAndReleasesUnusedHold(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, failedUsage := range []string{"", `,"usage":{"input_tokens":7,"output_tokens":0}`} {
			mode := map[bool]string{false: "native", true: "passthrough"}[passthrough]
			if failedUsage != "" {
				mode += "/unsettled-usage"
			} else {
				mode += "/unknown-usage"
			}
			t.Run(mode, func(t *testing.T) {
				ctx, _, wallet, key := prechargeFixture(.2)
				review := &prechargeReviewFixture{prechargeWalletFixture: wallet}
				requestPrecharge(ctx).repo = review
				require.NoError(t, ensureRequestBalancePrecharge(ctx, key.User, key))
				body := []byte(`{"model":"gpt-5.1","instructions":"test","input":"hello","stream":true}`)
				account := newOpenAIRejectedFieldTestAccount()
				account.Extra["openai_passthrough"] = passthrough
				first := `data: {"type":"response.failed","response":{"id":"resp_retry","error":{"code":"server_error","message":"retry another account"}` + failedUsage + `}}` + "\n\n"
				second := `data: {"type":"response.completed","response":{"id":"resp_zero","usage":{"input_tokens":0,"output_tokens":0}}}` + "\n\n"
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"req_first_retry"}}, Body: io.NopCloser(strings.NewReader(first))},
					{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(second))},
				}}
				forwardSvc := newOpenAIRejectedFieldTestService(upstream)
				StartBalancePrechargeUpstream(ctx)
				ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
				firstResult, firstErr := forwardSvc.Forward(ctx, newOpenAIRejectedFieldTestContext(body), account, body)
				var failover *UpstreamFailoverError
				require.ErrorAs(t, firstErr, &failover)
				require.Nil(t, firstResult)
				StartBalancePrechargeUpstream(ctx)
				ObserveBalancePrechargeUpstream(ctx, http.StatusOK, nil)
				secondResult, secondErr := forwardSvc.Forward(ctx, newOpenAIRejectedFieldTestContext(body), account, body)
				require.NoError(t, secondErr)
				require.NotNil(t, secondResult)
				usage := &openAIRecordUsageLogRepoStub{}
				billing := &openAIRecordUsageBillingRepoStub{}
				usageSvc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usage, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				require.NoError(t, usageSvc.RecordUsage(ctx, &OpenAIRecordUsageInput{
					APIKey: key, User: key.User, Account: account, Result: secondResult,
				}))
				FinishBalancePrecharge(ctx)
				require.InDelta(t, .2, wallet.balance, 1e-8)
				require.Empty(t, wallet.holds, "completed retries must release the unused reservation")
				require.Empty(t, review.reviews)
				require.Zero(t, billing.calls, "failover attempts must not be billed a second time")
				require.Zero(t, usage.calls)
			})
		}
	}
}
