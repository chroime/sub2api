//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type billingMaintenanceFixture struct {
	UsageBillingRepository
	recovered, archived int
	err                 error
	cutoff              time.Time
}

func (f *billingMaintenanceFixture) RenewBalancePrechargeLease(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *billingMaintenanceFixture) EndBalancePrechargeLease(context.Context, string, string) error {
	return nil
}
func (f *billingMaintenanceFixture) RecoverBalancePrecharges(context.Context, int) (int, error) {
	f.recovered++
	return 1, f.err
}
func (f *billingMaintenanceFixture) ArchiveBalancePrecharges(_ context.Context, cutoff time.Time, _ int) (int, error) {
	f.archived++
	f.cutoff = cutoff
	return 1, nil
}
func TestBillingMaintenance_RecoveryRunsBeforeArchival(t *testing.T) {
	f := &billingMaintenanceFixture{}
	s := NewBillingMaintenanceService(f, nil)
	require.NoError(t, s.RunOnce(context.Background()))
	require.Equal(t, 1, f.recovered)
	require.Equal(t, 1, f.archived)
	require.WithinDuration(t, time.Now().AddDate(0, 0, -90), f.cutoff, time.Second)
	f.err = errors.New("storage unavailable")
	require.Error(t, s.RunOnce(context.Background()))
	require.Equal(t, 1, f.archived)
}
