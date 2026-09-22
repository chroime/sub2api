//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/require"
)

type storageMonitorLog struct {
	level slog.Level
	text  string
	attrs []any
}

func storageMonitorFixture() (*BillingStorageMonitor, *[]storageMonitorLog) {
	m := NewBillingStorageMonitor(nil, nil)
	m.targets = []billingStorageTarget{{resource: "application_filesystem", path: "."}}
	m.diskUsage = func(context.Context, string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Total: 100 << 30, Free: 50 << 30, UsedPercent: 50}, nil
	}
	m.databaseSize = func(context.Context) (int64, error) { return 1 << 30, nil }
	logs := []storageMonitorLog{}
	m.emit = func(_ context.Context, level slog.Level, msg string, attrs ...any) {
		logs = append(logs, storageMonitorLog{level: level, text: msg, attrs: attrs})
	}
	return m, &logs
}

func TestBillingStorageMonitorThresholdsThrottleAndRecovery(t *testing.T) {
	m, logs := storageMonitorFixture()
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	stats := &disk.UsageStat{Total: 100 << 30, Free: 2 << 30, UsedPercent: 84.9}
	var databaseBytes int64 = (20 << 30) - 1
	m.diskUsage = func(context.Context, string) (*disk.UsageStat, error) { return stats, nil }
	m.databaseSize = func(context.Context) (int64, error) { return databaseBytes, nil }
	m.RunOnce(context.Background())
	require.Empty(t, *logs, "free bytes exactly at minimum is healthy")
	stats.UsedPercent, stats.Free, databaseBytes = 85, (2<<30)-1, 20<<30
	m.RunOnce(context.Background())
	require.Len(t, *logs, 3, "entry warnings for disk usage, free bytes, database bytes")
	for _, entry := range *logs {
		require.Equal(t, slog.LevelWarn, entry.level)
	}
	now = now.Add(59 * time.Minute)
	stats.UsedPercent = 90
	m.RunOnce(context.Background())
	require.Len(t, *logs, 3, "changing values within the same breach must stay throttled")
	now = now.Add(time.Minute)
	m.RunOnce(context.Background())
	require.Len(t, *logs, 6)
	stats.UsedPercent, stats.Free, databaseBytes = 50, 50<<30, 1<<30
	m.RunOnce(context.Background())
	require.Len(t, *logs, 9)
	for _, entry := range (*logs)[6:] {
		require.Equal(t, slog.LevelInfo, entry.level)
		require.Contains(t, entry.text, "recovered")
	}
	m.RunOnce(context.Background())
	require.Len(t, *logs, 9, "recovery emits once")
	stats.UsedPercent = 85
	m.RunOnce(context.Background())
	require.Len(t, *logs, 10, "reentry is not suppressed by old warning time")
}

func TestBillingStorageMonitorProbeErrorsDoNotInventRecoveryOrLeakDetails(t *testing.T) {
	m, logs := storageMonitorFixture()
	now := time.Now()
	m.now = func() time.Time { return now }
	stats := &disk.UsageStat{Total: 100 << 30, Free: 50 << 30, UsedPercent: 90}
	var probeErr error
	m.diskUsage = func(context.Context, string) (*disk.UsageStat, error) { return stats, probeErr }
	m.RunOnce(context.Background())
	require.Len(t, *logs, 1)
	probeErr = errors.New("postgres://private:secret@host/private-user-directory")
	m.RunOnce(context.Background())
	require.Len(t, *logs, 2)
	m.RunOnce(context.Background())
	require.Len(t, *logs, 2)
	now = now.Add(time.Hour)
	m.RunOnce(context.Background())
	require.Len(t, *logs, 3)
	for _, entry := range *logs {
		require.Equal(t, slog.LevelWarn, entry.level, "failed probe cannot recover prior capacity alarm")
		require.NotContains(t, fmt.Sprint(entry), "secret")
		require.NotContains(t, fmt.Sprint(entry), "private")
	}
	probeErr, stats.UsedPercent = nil, 50
	m.RunOnce(context.Background())
	require.Len(t, *logs, 5, "probe and capacity both recover once")
	m.RunOnce(context.Background())
	require.Len(t, *logs, 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.RunOnce(ctx)
	require.Len(t, *logs, 5, "normal shutdown is not probe failure")
}

func TestBillingStorageMonitorBoundsUncancellableProbeAndContinuesDatabase(t *testing.T) {
	m, logs := storageMonitorFixture()
	m.probeTimeout = 20 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	var diskCalls atomic.Int64
	var databaseCalls atomic.Int64
	m.diskUsage = func(context.Context, string) (*disk.UsageStat, error) {
		diskCalls.Add(1)
		<-release // Windows filesystem calls may ignore cancellation.
		return nil, errors.New("unblocked")
	}
	m.databaseSize = func(ctx context.Context) (int64, error) {
		_, ok := ctx.Deadline()
		if !ok {
			return 0, errors.New("missing deadline")
		}
		databaseCalls.Add(1)
		return 21 << 30, nil
	}
	started := time.Now()
	m.RunOnce(context.Background())
	m.RunOnce(context.Background())
	require.Less(t, time.Since(started), time.Second)
	require.EqualValues(t, 1, diskCalls.Load(), "stuck probes must not accumulate goroutines")
	require.EqualValues(t, 2, databaseCalls.Load())
	require.Len(t, *logs, 2, "disk failure and database capacity warnings")
}

func TestBillingStorageMonitorDatabaseQueryIsReadOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	m := NewBillingStorageMonitor(db, nil)
	m.targets = nil
	m.emit = func(context.Context, slog.Level, string, ...any) {}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_database_size(current_database())")).
		WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(int64(30 << 30)))
	m.RunOnce(context.Background())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBillingStorageMonitorInvalidReadingsStayUnavailable(t *testing.T) {
	for _, test := range []struct {
		name  string
		stats *disk.UsageStat
	}{
		{"nil stats", nil},
		{"empty volume", &disk.UsageStat{}},
		{"free larger than volume", &disk.UsageStat{Total: 10, Free: 11}},
		{"invalid percentage", &disk.UsageStat{Total: 10, UsedPercent: math.NaN()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, logs := storageMonitorFixture()
			m.diskUsage = func(context.Context, string) (*disk.UsageStat, error) { return test.stats, nil }
			m.databaseSize = func(context.Context) (int64, error) { return -1, nil }
			m.RunOnce(context.Background())
			require.Len(t, *logs, 2)
			for _, entry := range *logs {
				require.Equal(t, "billing storage probe unavailable", entry.text)
				require.Contains(t, fmt.Sprint(entry.attrs), "invalid_result")
			}
		})
	}
}

func TestBillingStorageMonitorLifecycleAndDisabled(t *testing.T) {
	m, _ := storageMonitorFixture()
	entered := make(chan struct{}, 1)
	m.diskUsage = func(ctx context.Context, _ string) (*disk.UsageStat, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	m.Start()
	m.Start()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("monitor did not run initial probe")
	}
	started := time.Now()
	m.Stop()
	m.Stop()
	m.Start()
	require.Less(t, time.Since(started), time.Second)
	select {
	case <-entered:
		t.Fatal("stopped monitor restarted")
	default:
	}

	disabled := NewBillingStorageMonitor(nil, &config.Config{})
	disabled.Start()
	require.Nil(t, disabled.cancel)
	disabled.Stop()
}

func TestBillingStorageMonitorPathsFollowLoggerDefaults(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "app-data")
	t.Setenv("DATA_DIR", dataDir)
	m := NewBillingStorageMonitor(nil, nil)
	require.Equal(t, dataDir, m.targets[0].path)
	require.Equal(t, filepath.Join(dataDir, "logs"), m.targets[1].path)

	cfg := &config.Config{}
	cfg.Log.Output.FilePath = filepath.Join(t.TempDir(), "custom", "service.log")
	m = NewBillingStorageMonitor(nil, cfg)
	require.Equal(t, filepath.Dir(cfg.Log.Output.FilePath), m.targets[1].path)
	t.Setenv("DATA_DIR", "")
	m = NewBillingStorageMonitor(nil, nil)
	require.Equal(t, filepath.Dir(logger.DefaultContainerLogPath), m.targets[1].path)

	parent := t.TempDir()
	path, err := resolveBillingStorageProbePath(filepath.Join(parent, "not-created", "logs"))
	require.NoError(t, err)
	require.Equal(t, parent, path, "a not-yet-created directory uses its nearest existing filesystem")
	_, err = os.Stat(filepath.Join(parent, "not-created"))
	require.True(t, os.IsNotExist(err), "monitor never creates directories")
}
