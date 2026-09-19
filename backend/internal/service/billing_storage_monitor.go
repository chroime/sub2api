package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/shirou/gopsutil/v4/disk"
)

const billingStorageWarningInterval = time.Hour

var (
	errBillingStorageProbeBusy    = errors.New("storage probe still running")
	errBillingStorageProbeInvalid = errors.New("invalid storage probe result")
)

type billingStorageTarget struct {
	resource string // Fixed label; paths and connection details are never logged.
	path     string
}

type billingStorageWarningState struct {
	active   bool
	lastWarn time.Time
}

// BillingStorageMonitor only observes storage and emits operational logs. It
// has no wallet, billing or deletion dependencies. A remote database host's
// filesystem capacity cannot be inferred from pg_database_size.
type BillingStorageMonitor struct {
	settings     config.BillingStorageMonitorConfig
	interval     time.Duration
	probeTimeout time.Duration
	targets      []billingStorageTarget
	diskUsage    func(context.Context, string) (*disk.UsageStat, error)
	databaseSize func(context.Context) (int64, error)
	now          func() time.Time
	emit         func(context.Context, slog.Level, string, ...any)
	states       map[string]billingStorageWarningState
	probeGates   map[string]chan struct{}
	runMu        sync.Mutex
	lifecycleMu  sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	stopped      bool
}

func NewBillingStorageMonitor(db *sql.DB, cfg *config.Config) *BillingStorageMonitor {
	c := config.DefaultBillingStorageMonitorConfig()
	if cfg != nil {
		if cfg.BillingStorageMonitor.Validate() == nil {
			c = cfg.BillingStorageMonitor
		} else {
			// Load validates real configuration. Keep direct construction safe,
			// including tests that create a zero-valued Config.
			c.Enabled = cfg.BillingStorageMonitor.Enabled
		}
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	applicationPath := dataDir
	if applicationPath == "" {
		applicationPath = "."
	}
	logPath := ""
	if cfg != nil {
		logPath = strings.TrimSpace(cfg.Log.Output.FilePath)
	}
	if logPath == "" {
		logPath = logger.DefaultContainerLogPath
		if dataDir != "" {
			logPath = filepath.Join(dataDir, "logs", "sub2api.log")
		}
	}
	s := &BillingStorageMonitor{
		settings: c, interval: time.Duration(c.IntervalSeconds) * time.Second,
		probeTimeout: time.Duration(c.ProbeTimeoutSeconds) * time.Second,
		targets: []billingStorageTarget{
			{resource: "application_filesystem", path: applicationPath},
			{resource: "log_filesystem", path: filepath.Dir(logPath)},
		},
		diskUsage: disk.UsageWithContext, now: time.Now, emit: slog.Log,
		states: make(map[string]billingStorageWarningState), probeGates: make(map[string]chan struct{}),
	}
	if db != nil {
		s.databaseSize = func(ctx context.Context) (int64, error) {
			var bytes int64
			err := db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&bytes)
			return bytes, err
		}
	}
	return s
}

func (s *BillingStorageMonitor) Start() {
	if s == nil || !s.settings.Enabled {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.cancel != nil || s.stopped {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			s.RunOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *BillingStorageMonitor) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.stopped = true
	cancel, done := s.cancel, s.done
	s.lifecycleMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// RunOnce gives every resource its own bounded context. Probe errors are
// throttled independently and cannot turn an existing capacity alarm healthy.
func (s *BillingStorageMonitor) RunOnce(ctx context.Context) {
	if s == nil || !s.settings.Enabled || ctx.Err() != nil || !s.runMu.TryLock() {
		return
	}
	defer s.runMu.Unlock()
	for _, target := range s.targets {
		if ctx.Err() != nil {
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, s.probeTimeout)
		stats, err := runBillingStorageProbe(probeCtx, s.probeGate(target.resource), func() (*disk.UsageStat, error) {
			path, err := resolveBillingStorageProbePath(target.path)
			if err != nil {
				return nil, err
			}
			return s.diskUsage(probeCtx, path)
		})
		cancel()
		if err == nil && (stats == nil || stats.Total == 0 || stats.Free > stats.Total || math.IsNaN(stats.UsedPercent) || math.IsInf(stats.UsedPercent, 0) || stats.UsedPercent < 0 || stats.UsedPercent > 100) {
			err = errBillingStorageProbeInvalid
		}
		if !s.probeAvailable(ctx, target.resource, err) {
			continue
		}
		s.observe(ctx, target.resource, "used_percent", stats.UsedPercent >= s.settings.LocalUsedPercent,
			"value", stats.UsedPercent, "threshold", s.settings.LocalUsedPercent)
		s.observe(ctx, target.resource, "free_bytes", stats.Free < uint64(s.settings.LocalMinFreeBytes),
			"value", stats.Free, "threshold", s.settings.LocalMinFreeBytes)
	}
	if s.databaseSize == nil || ctx.Err() != nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.probeTimeout)
	bytes, err := runBillingStorageProbe(probeCtx, s.probeGate("database"), func() (int64, error) {
		return s.databaseSize(probeCtx)
	})
	cancel()
	if err == nil && bytes < 0 {
		err = errBillingStorageProbeInvalid
	}
	if s.probeAvailable(ctx, "database", err) {
		s.observe(ctx, "database", "size_bytes", bytes >= s.settings.DatabaseWarnBytes,
			"value", bytes, "threshold", s.settings.DatabaseWarnBytes)
	}
}

func (s *BillingStorageMonitor) probeGate(resource string) chan struct{} {
	gate := s.probeGates[resource]
	if gate == nil {
		gate = make(chan struct{}, 1)
		s.probeGates[resource] = gate
	}
	return gate
}

// gopsutil's Windows disk syscall does not honor context cancellation. Keep at
// most one outstanding call per resource; timed-out calls never accumulate on
// later ticks and cannot delay shutdown. The buffered result can finish safely.
func runBillingStorageProbe[T any](ctx context.Context, gate chan struct{}, probe func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	select {
	case gate <- struct{}{}:
	default:
		return zero, errBillingStorageProbeBusy
	}
	type result struct {
		value T
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		out := func() result {
			defer func() { <-gate }()
			value, err := probe()
			return result{value: value, err: err}
		}()
		resultCh <- out
	}()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case out := <-resultCh:
		return out.value, out.err
	}
}

func (s *BillingStorageMonitor) probeAvailable(ctx context.Context, resource string, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	reason := "unavailable"
	switch {
	case err == nil:
		reason = "available"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "timeout"
	case errors.Is(err, errBillingStorageProbeBusy):
		reason = "still_running"
	case errors.Is(err, errBillingStorageProbeInvalid):
		reason = "invalid_result"
	}
	s.observe(ctx, resource, "probe_status", err != nil, "reason", reason)
	return err == nil
}

func (s *BillingStorageMonitor) observe(ctx context.Context, resource, metric string, active bool, attrs ...any) {
	key := resource + ":" + metric
	previous := s.states[key]
	now := s.now()
	if !active && !previous.active {
		return
	}
	if active && previous.active && now.Sub(previous.lastWarn) < billingStorageWarningInterval {
		return
	}
	level, message := slog.LevelWarn, "billing storage capacity threshold exceeded"
	if metric == "probe_status" {
		message = "billing storage probe unavailable"
	}
	if !active {
		level, message = slog.LevelInfo, "billing storage capacity recovered"
		if metric == "probe_status" {
			message = "billing storage probe recovered"
		}
	}
	fields := []any{"component", "billing.storage_monitor", "resource", resource, "metric", metric}
	fields = append(fields, attrs...)
	s.emit(ctx, level, message, fields...)
	s.states[key] = billingStorageWarningState{active: active, lastWarn: now}
}

// A future data/log directory is measured on its nearest existing parent. This
// is read-only and uses filepath so drive letters work on Windows as well.
func resolveBillingStorageProbePath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", errBillingStorageProbeInvalid
		}
		path = parent
	}
}
