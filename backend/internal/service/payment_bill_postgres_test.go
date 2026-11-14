//go:build unit

package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestBillPostgresConcurrentClaimsAndConfigHistory(t *testing.T) {
	dsn := os.Getenv("ALIPAY_BILL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated ALIPAY_BILL_TEST_DSN")
	}
	// The external runner creates a disposable database with this exact prefix.
	// Refuse accidental use of the developer or production database.
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	var database string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.True(t, strings.HasPrefix(database, "sub2api_billtest_"), "isolated database required")
	ctx := context.Background()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	require.NoError(t, client.Schema.Create(ctx))
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "282_payment_bill_receipts.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	epayMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "283_payment_epay_orders.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(epayMigration))
	require.NoError(t, err)
	amountMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "284_payment_bill_amount_reservations.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(amountMigration))
	require.NoError(t, err)
	closureMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "285_payment_bill_amount_closure.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(closureMigration))
	require.NoError(t, err)
	userCancelMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "286_payment_bill_user_cancel_release.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(userCancelMigration))
	require.NoError(t, err)
	deadlineMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "290_payment_bill_amount_no_cooldown.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(deadlineMigration))
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, configService: NewPaymentConfigService(client, nil, nil)}
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey("alipay_bill").SetName("Isolated fixture").SetConfig(`{"receiverId":"2088000000000001"}`).Save(ctx)
	require.NoError(t, err)
	a := billTestOrder(t, svc, inst.ID, "CONCURRENT_A")
	b := billTestOrder(t, svc, inst.ID, "CONCURRENT_B")
	receipt, _, err := svc.storeBillReceipt(ctx, inst.ID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "LOG_CONCURRENT", AlipayOrderNo: "TRADE_CONCURRENT", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	gate := make(chan struct{})
	outcomes := make(chan error, 2)
	var workers sync.WaitGroup
	for _, id := range []int64{a.ID, b.ID} {
		workers.Add(1)
		go func(orderID int64) {
			defer workers.Done()
			<-gate
			outcomes <- svc.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, orderID, "admin:1", "verified", true)
		}(id)
	}
	close(gate)
	workers.Wait()
	close(outcomes)
	successes := 0
	for err := range outcomes {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "a payment transaction may settle only one order even under concurrency")
	var claims int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM payment_bill_claims`).Scan(&claims))
	require.Equal(t, 1, claims)
	var paid int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM payment_orders WHERE status='PAID'`).Scan(&paid))
	require.Equal(t, 1, paid)
	// A delayed ordinary Alipay confirmation shares the same transaction lock
	// with bill review, even though it does not insert a bill claim of its own.
	old := billTestOrder(t, svc, inst.ID, "LEGACY_RACE")
	fixed := billTestOrder(t, svc, inst.ID, "FIXED_RACE")
	r, _, err := svc.storeBillReceipt(ctx, inst.ID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "RACE_LOG", AlipayOrderNo: "RACE_TRADE", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	raceGate := make(chan struct{})
	raceResults := make(chan error, 2)
	go func() {
		<-raceGate
		raceResults <- svc.claimBillReceipt(ctx, r.ReceiverID, r.ID, fixed.ID, "admin:1", "verified", true)
	}()
	go func() {
		<-raceGate
		_, err := svc.guardAlipayPaidTransition(ctx, old, "RACE_TRADE", func(c *dbent.Client) (int, error) {
			return c.PaymentOrder.Update().Where(paymentorder.ID(old.ID), paymentorder.Status(OrderStatusPending)).SetStatus(OrderStatusPaid).SetPaymentTradeNo("RACE_TRADE").Save(ctx)
		})
		raceResults <- err
	}()
	close(raceGate)
	raceSuccess := 0
	for i := 0; i < 2; i++ {
		if <-raceResults == nil {
			raceSuccess++
		}
	}
	require.Equal(t, 1, raceSuccess)
	var duplicatePayments int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM payment_orders WHERE payment_trade_no='RACE_TRADE'`).Scan(&duplicatePayments))
	require.Equal(t, 1, duplicatePayments)
	testBuiltinEasyPayPostgresClaims(t, svc, inst.ID, db)
	// Validate real driver/transaction SQL, lease CAS, and durable progress after error.
	now := time.Now().UTC().Truncate(time.Second)
	_, err = svc.acquireBillSyncLease(ctx, receipt.ReceiverID, "worker1", now)
	require.NoError(t, err)
	_, err = svc.acquireBillSyncLease(ctx, receipt.ReceiverID, "worker2", now)
	require.Error(t, err)
	cursor := now.Add(-time.Hour)
	svc.releaseBillSyncLease(receipt.ReceiverID, "worker1", cursor, false)
	stored, err := svc.acquireBillSyncLease(ctx, receipt.ReceiverID, "worker2", time.Now().UTC())
	require.NoError(t, err)
	require.True(t, cursor.Equal(stored))
}

func testBuiltinEasyPayPostgresClaims(t *testing.T, svc *PaymentService, instanceID int64, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, ordinaryCallback := range []bool{false, true} {
		name := "LOCAL_EXTERNAL"
		if ordinaryCallback {
			name = "CALLBACK_EXTERNAL"
		}
		t.Run(name, func(t *testing.T) {
			local := billTestOrder(t, svc, instanceID, name+"_LOCAL")
			externalID := epayReceiptTestOrder(t, svc, instanceID, name+"_EPAY")
			receipt, _, err := svc.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{
				AccountLogID: name + "_LOG", AlipayOrderNo: name + "_ALI", Amount: "100.00", Direction: "收入", TransAt: time.Now(),
			})
			require.NoError(t, err)
			gate := make(chan struct{})
			outcomes := make(chan error, 2)
			go func() {
				<-gate
				outcomes <- svc.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, externalID, "admin:1", "verified merchant receipt")
			}()
			go func() {
				<-gate
				if ordinaryCallback {
					_, err := svc.guardAlipayPaidTransition(ctx, local, receipt.AlipayOrderNo, func(client *dbent.Client) (int, error) {
						return client.PaymentOrder.Update().Where(paymentorder.ID(local.ID), paymentorder.Status(OrderStatusPending)).SetStatus(OrderStatusPaid).SetPaymentTradeNo(receipt.AlipayOrderNo).Save(ctx)
					})
					outcomes <- err
				} else {
					outcomes <- svc.claimBillReceipt(ctx, receipt.ReceiverID, receipt.ID, local.ID, "admin:1", "verified local payment", true)
				}
			}()
			close(gate)
			successes := 0
			for i := 0; i < 2; i++ {
				if <-outcomes == nil {
					successes++
				}
			}
			require.Equal(t, 1, successes, "one official transaction cannot fund local and external orders concurrently")
			var paidCount int
			require.NoError(t, db.QueryRow(`SELECT (SELECT COUNT(*) FROM payment_orders WHERE id=$1 AND status='PAID') + (SELECT COUNT(*) FROM payment_epay_orders WHERE id=$2 AND status='PAID')`, local.ID, externalID).Scan(&paidCount))
			require.Equal(t, 1, paidCount)
			external, err := getBuiltinEasyPayOrderForReceipt(ctx, svc.entClient, externalID)
			require.NoError(t, err)
			if external.Status == "PAID" {
				require.Equal(t, "pending", external.NotifyStatus)
				require.NotNil(t, external.NotifyNextAt)
				require.NoError(t, svc.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, externalID, "admin:1", "safe retry"))
			} else {
				require.Equal(t, "none", external.NotifyStatus)
			}
		})
	}
	// The actual PostgreSQL migration must enforce exactly one target, even for
	// direct SQL writes outside the application claim methods.
	local := billTestOrder(t, svc, instanceID, "XOR_LOCAL")
	externalID := epayReceiptTestOrder(t, svc, instanceID, "XOR_EPAY")
	receipt, _, err := svc.storeBillReceipt(ctx, instanceID, "2088000000000001", provider.AlipayBillEntry{AccountLogID: "XOR_LOG", AlipayOrderNo: "XOR_ALI", Amount: "100.00", Direction: "收入", TransAt: time.Now()})
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payment_bill_claims(receipt_id,receiver_id,alipay_order_no,order_id,epay_order_id,actor,reason) VALUES($1,$2,$3,$4,$5,'test','invalid dual target')`, receipt.ID, receipt.ReceiverID, receipt.AlipayOrderNo, local.ID, externalID)
	var violation *pq.Error
	require.ErrorAs(t, err, &violation)
	require.Equal(t, pq.ErrorCode("23514"), violation.Code)
	_, err = db.Exec(`INSERT INTO payment_bill_claims(receipt_id,receiver_id,alipay_order_no,actor,reason) VALUES($1,$2,$3,'test','invalid missing target')`, receipt.ID, receipt.ReceiverID, receipt.AlipayOrderNo)
	require.ErrorAs(t, err, &violation)
	require.Equal(t, pq.ErrorCode("23514"), violation.Code)
	require.NoError(t, svc.ClaimBuiltinEasyPayReceipt(ctx, instanceID, receipt.ID, externalID, "admin:1", "confirmed after rejected malformed writes"))
	external, err := getBuiltinEasyPayOrderForReceipt(ctx, svc.entClient, externalID)
	require.NoError(t, err)
	require.Equal(t, "PAID", external.Status)
	require.Equal(t, "pending", external.NotifyStatus)
	require.NotNil(t, external.NotifyNextAt)
}
