//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newUsageLogOutboxTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newBalancePrechargeTestDB(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "243_usage_log_outbox.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	// Use the same insert shape as the production repository. Every write stays
	// in the UUID schema supplied by newBalancePrechargeTestDB.
	columns := strings.Split(usageLogSelectColumns, ", ")[1:]
	require.Len(t, columns, len(usageLogInsertArgTypes))
	definitions := []string{"id BIGSERIAL PRIMARY KEY"}
	for i, column := range columns {
		columnType := usageLogInsertArgTypes[i]
		if column == "actual_cost" || column == "total_cost" {
			columnType = "numeric(20,10)"
		}
		definitions = append(definitions, column+" "+columnType)
	}
	definitions = append(definitions, "UNIQUE (request_id, api_key_id)")
	_, err = db.Exec("CREATE TABLE usage_logs (" + strings.Join(definitions, ", ") + ")")
	require.NoError(t, err)
	return db
}

func outboxBillingCommand(requestID string) *service.UsageBillingCommand {
	return &service.UsageBillingCommand{
		RequestID: requestID, UserID: 1, APIKeyID: 1, AccountID: 1, BalanceCost: .125,
		UsageLogSnapshot: &service.UsageLog{
			RequestID: requestID, UserID: 1, APIKeyID: 1, AccountID: 1,
			Model: "test-model", RequestedModel: "requested-model", InputTokens: 10,
			ActualCost: .125, TotalCost: .125, CreatedAt: time.Date(2026, 9, 19, 1, 2, 3, 0, time.UTC),
		},
	}
}

func TestUsageLogOutbox_BillingPersistsSanitizedSnapshotAtomically(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("snapshot")
	// Apply must defend its transaction boundary even if a non-gateway caller
	// passes a snapshot that still holds sensitive relation objects.
	cmd.UsageLogSnapshot.APIKey = &service.APIKey{Key: "secret-key"}
	cmd.UsageLogSnapshot.Account = &service.Account{Credentials: map[string]any{"access_token": "secret-token"}}
	result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	var payload []byte
	require.NoError(t, db.QueryRow(`SELECT payload FROM usage_log_outbox WHERE request_id='snapshot'`).Scan(&payload))
	require.NotContains(t, string(payload), "secret-")
	var envelope struct {
		Version int               `json:"version"`
		Usage   *service.UsageLog `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(payload, &envelope))
	require.Equal(t, 1, envelope.Version)
	require.NotNil(t, envelope.Usage)
	require.Equal(t, cmd.UsageLogSnapshot.CreatedAt, envelope.Usage.CreatedAt)
	require.Equal(t, .125, envelope.Usage.ActualCost)
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
	result, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestUsageLogOutbox_InsertFailureRollsBackBillingAndDedup(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	_, err := db.Exec(`ALTER TABLE usage_log_outbox ADD CONSTRAINT simulate_disk_error CHECK (request_id <> 'rollback')`)
	require.NoError(t, err)
	result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), outboxBillingCommand("rollback"))
	require.Error(t, err)
	require.Nil(t, result)
	assertPrechargeWallet(t, db, "5.00000000", "0.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
	require.Zero(t, count)
}

func TestUsageLogOutbox_InsertFailureAlsoRollsBackPrechargeCapture(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	_, err := balancePrechargeRepo(t, db).ReserveBalancePrecharge(context.Background(), prechargeCommand("outbox-hold"))
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE usage_log_outbox ADD CONSTRAINT simulate_capture_error CHECK (request_id <> 'capture-rollback')`)
	require.NoError(t, err)
	cmd := outboxBillingCommand("capture-rollback")
	cmd.BalancePrechargeID = "outbox-hold"
	_, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.Error(t, err)
	assertPrechargeWallet(t, db, "3.00000000", "2.00000000")
	var status string
	require.NoError(t, db.QueryRow(`SELECT state FROM balance_precharges WHERE id='outbox-hold'`).Scan(&status))
	require.Equal(t, "reserved", status)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Zero(t, count)
}

func makeUsageLogOutboxReady(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`UPDATE usage_log_outbox SET available_at=NOW()-INTERVAL '1 second', lease_until=NULL`)
	require.NoError(t, err)
}

func TestUsageLogOutbox_MissingUsageWriteRecoversAfterRestart(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("crashed-before-usage")
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	worker := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db))
	completed, err := worker.RunOnce(context.Background(), 32)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	var amount float64
	var created time.Time
	var model string
	require.NoError(t, db.QueryRow(`SELECT actual_cost, created_at, requested_model FROM usage_logs WHERE request_id=$1`, cmd.RequestID).Scan(&amount, &created, &model))
	require.Equal(t, .125, amount)
	require.Equal(t, cmd.UsageLogSnapshot.CreatedAt, created.UTC())
	require.Equal(t, "requested-model", model)
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestUsageLogOutbox_FailedUsageInsertRemainsDurableAndRetries(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), outboxBillingCommand("write-failure"))
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE usage_logs ADD CONSTRAINT simulate_usage_failure CHECK (request_id <> 'write-failure')`)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	worker := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db))
	completed, err := worker.RunOnce(context.Background(), 32)
	require.Error(t, err)
	require.Zero(t, completed)
	var attempts int
	var reason, claim string
	var deferred bool
	require.NoError(t, db.QueryRow(`SELECT attempts, last_error_code, claim_token, available_at > NOW() FROM usage_log_outbox`).Scan(&attempts, &reason, &claim, &deferred))
	require.Equal(t, 1, attempts)
	require.Equal(t, "usage_write_failed", reason)
	require.Empty(t, claim)
	require.True(t, deferred)
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
	_, err = db.Exec(`ALTER TABLE usage_logs DROP CONSTRAINT simulate_usage_failure`)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	completed, err = service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 32)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
}

func TestUsageLogOutbox_CrashAfterUsageInsertIsIdempotent(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("crashed-before-complete")
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	repo := NewUsageLogOutboxRepository(db)
	items, err := repo.ClaimUsageLogOutbox(context.Background(), 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, items, 1)
	inserted, err := NewUsageLogRepository(nil, db).Create(context.Background(), cmd.UsageLogSnapshot)
	require.NoError(t, err)
	require.True(t, inserted)
	_, err = db.Exec(`UPDATE usage_log_outbox SET lease_until=NOW()-INTERVAL '1 second'`)
	require.NoError(t, err)
	completed, err := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_logs`).Scan(&count))
	require.Equal(t, 1, count)
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
}

func TestUsageLogOutbox_ExpiredClaimIsFencedFromNewWorker(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), outboxBillingCommand("fenced"))
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	repo := NewUsageLogOutboxRepository(db)
	first, err := repo.ClaimUsageLogOutbox(context.Background(), 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)
	busy, err := repo.ClaimUsageLogOutbox(context.Background(), 1, time.Minute)
	require.NoError(t, err)
	require.Empty(t, busy)
	_, err = db.Exec(`UPDATE usage_log_outbox SET lease_until=NOW()-INTERVAL '1 second'`)
	require.NoError(t, err)
	second, err := repo.ClaimUsageLogOutbox(context.Background(), 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.NotEqual(t, first[0].ClaimToken, second[0].ClaimToken)
	require.Equal(t, 2, second[0].Attempts)
	completed, err := repo.CompleteUsageLogOutbox(context.Background(), first[0].ID, first[0].ClaimToken)
	require.NoError(t, err)
	require.False(t, completed)
	retried, err := repo.RetryUsageLogOutbox(context.Background(), first[0].ID, first[0].ClaimToken, time.Second, "usage_write_failed")
	require.NoError(t, err)
	require.False(t, retried)
	_, err = NewUsageLogRepository(nil, db).Create(context.Background(), outboxBillingCommand("fenced").UsageLogSnapshot)
	require.NoError(t, err)
	completed, err = repo.CompleteUsageLogOutbox(context.Background(), second[0].ID, second[0].ClaimToken)
	require.NoError(t, err)
	require.True(t, completed)
}

func TestUsageLogOutbox_RepairsZeroCostFailurePlaceholderFromCommittedEvidence(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("ambiguous-commit")
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	placeholder := service.SanitizeUsageLogForRecovery(cmd.UsageLogSnapshot)
	placeholder.ActualCost = 0
	_, err = NewUsageLogRepository(nil, db).Create(context.Background(), placeholder)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	completed, err := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
	require.Zero(t, count)
	var amount float64
	require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE request_id=$1`, cmd.RequestID).Scan(&amount))
	require.Equal(t, .125, amount)
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
}

func TestUsageLogOutbox_ConflictingNonzeroBillRemainsForReview(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("conflicting-cost")
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	conflict := service.SanitizeUsageLogForRecovery(cmd.UsageLogSnapshot)
	conflict.ActualCost = .5
	_, err = NewUsageLogRepository(nil, db).Create(context.Background(), conflict)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	completed, err := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 1)
	require.Error(t, err)
	require.Zero(t, completed)
	var reason string
	require.NoError(t, db.QueryRow(`SELECT last_error_code FROM usage_log_outbox`).Scan(&reason))
	require.Equal(t, "usage_conflict", reason)
	var amount float64
	require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE request_id=$1`, cmd.RequestID).Scan(&amount))
	require.Equal(t, .5, amount, "recovery must not overwrite a conflicting nonzero bill")
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
}

func TestUsageLogOutbox_ZeroCostRepairCannotCrossUsageOwnership(t *testing.T) {
	for _, field := range []string{"user_id", "account_id"} {
		t.Run(field, func(t *testing.T) {
			db := newUsageLogOutboxTestDB(t)
			cmd := outboxBillingCommand("ownership")
			_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			require.NoError(t, err)
			placeholder := service.SanitizeUsageLogForRecovery(cmd.UsageLogSnapshot)
			placeholder.ActualCost = 0
			if field == "user_id" {
				placeholder.UserID = 2
			} else {
				placeholder.AccountID = 2
			}
			_, err = NewUsageLogRepository(nil, db).Create(context.Background(), placeholder)
			require.NoError(t, err)
			makeUsageLogOutboxReady(t, db)
			completed, err := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 1)
			require.Error(t, err)
			require.Zero(t, completed)
			var amount float64
			require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE request_id='ownership'`).Scan(&amount))
			require.Zero(t, amount)
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
			require.Equal(t, 1, count)
			assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
		})
	}
}

func TestUsageLogOutbox_ConcurrentNormalWritesAndRecoveryAreIdempotent(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	billing := NewUsageBillingRepository(nil, db)
	for i := 0; i < 20; i++ {
		_, err := billing.Apply(context.Background(), outboxBillingCommand(fmt.Sprintf("concurrent-%d", i)))
		require.NoError(t, err)
	}
	makeUsageLogOutboxReady(t, db)
	start := make(chan struct{})
	errs := make(chan error, 22)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 16)
			errs <- err
		}()
	}
	writer := NewUsageLogRepository(nil, db)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := writer.Create(context.Background(), outboxBillingCommand(fmt.Sprintf("concurrent-%d", i)).UsageLogSnapshot)
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_logs`).Scan(&count))
	require.Equal(t, 20, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
	require.Zero(t, count)
	assertPrechargeWallet(t, db, "2.50000000", "0.00000000")
}

func TestUsageLogOutbox_NormalAcknowledgementRequiresDurableMatchingRecord(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("normal-ack")
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	usage := NewUsageLogRepository(nil, db)
	acknowledger, ok := usage.(interface {
		AcknowledgeUsageLogOutbox(context.Context, *service.UsageLog) error
	})
	require.True(t, ok, "normal durable usage writes should promptly remove completed outbox work")
	countPending := func() int {
		var count int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_log_outbox`).Scan(&count))
		return count
	}
	require.NoError(t, acknowledger.AcknowledgeUsageLogOutbox(context.Background(), cmd.UsageLogSnapshot))
	require.Equal(t, 1, countPending(), "a queue acknowledgement without a committed row must retain recovery work")
	placeholder := service.SanitizeUsageLogForRecovery(cmd.UsageLogSnapshot)
	placeholder.ActualCost = 0
	_, err = usage.Create(context.Background(), placeholder)
	require.NoError(t, err)
	require.NoError(t, acknowledger.AcknowledgeUsageLogOutbox(context.Background(), placeholder))
	require.Equal(t, 1, countPending(), "a zero-cost failure placeholder must not discard the paid snapshot")
	_, err = db.Exec(`UPDATE usage_logs SET actual_cost=.125 WHERE request_id='normal-ack'`)
	require.NoError(t, err)
	require.NoError(t, acknowledger.AcknowledgeUsageLogOutbox(context.Background(), cmd.UsageLogSnapshot))
	require.Zero(t, countPending())
	require.NoError(t, acknowledger.AcknowledgeUsageLogOutbox(context.Background(), cmd.UsageLogSnapshot))
	assertPrechargeWallet(t, db, "4.87500000", "0.00000000")
}

func TestUsageLogOutbox_AcknowledgementUsesUsageColumnPrecision(t *testing.T) {
	db := newUsageLogOutboxTestDB(t)
	cmd := outboxBillingCommand("precision")
	cmd.BalanceCost = .000078125055
	cmd.UsageLogSnapshot.ActualCost = cmd.BalanceCost
	cmd.UsageLogSnapshot.TotalCost = cmd.BalanceCost
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	makeUsageLogOutboxReady(t, db)
	completed, err := service.NewUsageLogRecoveryService(NewUsageLogOutboxRepository(db), NewUsageLogRepository(nil, db)).RunOnce(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	var amount string
	require.NoError(t, db.QueryRow(`SELECT actual_cost::text FROM usage_logs WHERE request_id='precision'`).Scan(&amount))
	require.Equal(t, "0.0000781251", amount)
	assertPrechargeWallet(t, db, "4.99992187", "0.00000000")
}
