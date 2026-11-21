package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cleanup := minimalCleanup(t, nil)
	require.NotPanics(t, cleanup)
}

func minimalCleanup(t *testing.T, lanes *service.UsageBillingLaneCombiner) func() {
	t.Helper()
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	codexVersionSyncSvc := service.NewOpenAICodexVersionSyncService(nil, nil, nil, time.Second)
	claudeCodeVersionSyncSvc := service.NewClaudeCodeVersionSyncService(nil, nil, nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)

	dependencies := []any{
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		nil, // opsService
		nil, // opsIngressRejectAggregator
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		nil, // cnProviderBalanceCheck
		codexVersionSyncSvc,
		claudeCodeVersionSyncSvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		service.NewGeminiCachedContentService(nil, nil, cfg),
		&service.BatchImageCleanupService{},
		nil, // batchImageWorker
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		nil, // billingMaintenance
		nil, // billingStorageMonitor
		nil, // usageLogRecovery
		service.ProvideBalanceNotifyService(nil, nil, nil, nil),
		service.NewDigestSessionStore(),
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // channelMonitorRunner
		nil, // channelMonitorV2Aggregator
		nil, // quotaFlusher
		nil, // upstreamBillingProbe
		nil, // upstreamGovernance
		nil, // accountQuality
		nil, // ollamaCloudUsage
		nil, // opencodeGoUsage
		nil, // auditLog
		nil, // openAIAutoReset
		nil, // promptAudit
		nil, // pluginManager
		nil, // notificationEmails
	}
	dependencies = append(dependencies, lanes)
	byType := make(map[reflect.Type]reflect.Value)
	for _, dependency := range dependencies {
		if dependency != nil {
			byType[reflect.TypeOf(dependency)] = reflect.ValueOf(dependency)
		}
	}
	constructor := reflect.ValueOf(provideCleanup)
	inputs := make([]reflect.Value, constructor.Type().NumIn())
	for i := range inputs {
		typeOf := constructor.Type().In(i)
		inputs[i] = reflect.Zero(typeOf)
		if dependency, ok := byType[typeOf]; ok {
			inputs[i] = dependency
		}
	}
	return constructor.Call(inputs)[0].Interface().(func())
}

type cleanupBlockingBillingRepo struct {
	service.UsageBillingBatchRepository
	started chan struct{}
	release chan struct{}
}

func (r *cleanupBlockingBillingRepo) Apply(context.Context, *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	close(r.started)
	<-r.release
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestProvideCleanupWaitsForInFlightBilling(t *testing.T) {
	repo := &cleanupBlockingBillingRepo{started: make(chan struct{}), release: make(chan struct{})}
	lanes := service.NewUsageBillingLaneCombiner(repo)
	cleanup := minimalCleanup(t, lanes)
	applied := make(chan error, 1)
	go func() {
		_, err := lanes.Apply(context.Background(), &service.UsageBillingCommand{UserID: 1, RequestID: "shutdown-billing"})
		applied <- err
	}()
	select {
	case <-repo.started:
	case <-time.After(2 * time.Second):
		t.Fatal("billing did not start")
	}
	done := make(chan struct{})
	go func() { cleanup(); close(done) }()
	returnedEarly := false
	select {
	case <-done:
		returnedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(repo.release)
	require.NoError(t, <-applied)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not finish after billing settled")
	}
	lanes.Stop()
	require.False(t, returnedEarly, "cleanup must drain billing before closing its database")
}
