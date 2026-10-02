package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"
)

var (
	ErrModelBudget          = errors.New("model monitoring daily request budget exhausted")
	ErrModelGroupGone       = errors.New("model monitoring upstream group disappeared")
	errModelIdentityChanged = fmt.Errorf("model target identity changed: %w", ErrConflict)
)

func modelConfigHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validateModelConfig(c *ModelTestConfig) error {
	if c.ManagedKeyID <= 0 || !validProbeModel(c.Model) || c.Samples < 1 || c.Samples > 100 || c.Concurrency < 1 || c.Concurrency > 32 || c.MaxOutputTokens < 1 || c.MaxOutputTokens > 65536 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 1800 || c.InputTokens < 0 || c.InputTokens > 500000 {
		return ErrInvalid
	}
	if c.FirstContentTimeoutSeconds < 0 || c.FirstContentTimeoutSeconds > c.TimeoutSeconds || c.IdleTimeoutSeconds < 0 || c.IdleTimeoutSeconds > c.TimeoutSeconds {
		return ErrInvalid
	}
	if math.IsNaN(c.TokenTolerancePercent) || math.IsInf(c.TokenTolerancePercent, 0) || c.TokenTolerancePercent < 0 || c.TokenTolerancePercent > 100 {
		return ErrInvalid
	}
	switch c.APIMode {
	case "chat_completions", "responses", "anthropic", "gemini":
	default:
		return ErrInvalid
	}
	switch c.Tokenizer {
	case "auto", "o200k_base", "cl100k_base", "unknown", "none", "":
	default:
		return ErrInvalid
	}
	if c.Tokenizer == "" {
		c.Tokenizer = "auto"
	}
	if len(c.Efforts) < 1 || len(c.Efforts) > 3 || len(c.Templates) < 1 || len(c.Templates) > 5 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, e := range c.Efforts {
		if (e != "low" && e != "medium" && e != "high") || seen[e] {
			return ErrInvalid
		}
		seen[e] = true
	}
	seen = map[string]bool{}
	for _, template := range c.Templates {
		if (template != "candy" && template != "pelican" && template != "token_audit" && template != "context" && template != "probe") || seen[template] {
			return ErrInvalid
		}
		seen[template] = true
	}
	if (seen["token_audit"] || seen["context"]) && c.InputTokens < 64 {
		return ErrInvalid
	}
	if len(c.Efforts)*len(c.Templates)*c.Samples > 300 {
		return ErrInvalid
	}
	return nil
}

func (s *Service) modelTarget(ctx context.Context, siteID int64, c *ModelTestConfig) (*Site, *ManagedKey, modelIdentity, error) {
	var empty modelIdentity
	if !s.durableKey || s.cipher == nil {
		return nil, nil, empty, ErrEncryption
	}
	if _, ok := s.connector.(ModelRunner); !ok {
		return nil, nil, empty, ErrUnsupported
	}
	if err := validateModelConfig(c); err != nil {
		return nil, nil, empty, err
	}
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return nil, nil, empty, err
	}
	if !site.Enabled {
		return nil, nil, empty, ErrConflict
	}
	k, err := s.store.GetManagedKey(ctx, siteID, c.ManagedKeyID)
	if err != nil {
		return nil, nil, empty, err
	}
	if k.SiteID != siteID || k.KeyCipher == "" {
		return nil, nil, empty, ErrInvalid
	}
	session, err := s.session(*site)
	if err != nil {
		return nil, nil, empty, err
	}
	if session.UserID <= 0 || session.UserID != k.OwnerUserID {
		return nil, nil, empty, ErrConflict
	}
	if c.Platform != "" && c.Platform != k.Platform {
		return nil, nil, empty, ErrInvalid
	}
	c.Platform = k.Platform
	if !validSiteTransport(site.Platform, k.Platform) {
		return nil, nil, empty, ErrInvalid
	}
	if (c.APIMode == "anthropic" && k.Platform != "anthropic" && k.Platform != "antigravity") || (c.APIMode == "gemini" && k.Platform != "gemini" && k.Platform != "antigravity") || ((c.APIMode == "chat_completions" || c.APIMode == "responses") && (k.Platform == "anthropic" || k.Platform == "gemini")) {
		return nil, nil, empty, ErrInvalid
	}
	snapshot, err := s.store.LatestSnapshot(ctx, siteID)
	if err != nil {
		return nil, nil, empty, err
	}
	if snapshot.SiteVersion != site.Version {
		return nil, nil, empty, ErrConflict
	}
	found := false
	for _, g := range snapshot.Catalog.Groups {
		if g.ID == k.RemoteGroupID {
			found = true
			break
		}
	}
	if !found {
		if snapshot.Catalog.GroupsComplete {
			return nil, nil, empty, ErrModelGroupGone
		}
		return nil, nil, empty, ErrConflict
	}
	identity := modelIdentity{BaseURL: site.BaseURL, SitePlatform: site.Platform, ProxyID: site.ProxyID, ManagedKeyID: k.ID, GroupID: k.RemoteGroupID, Platform: k.Platform, KeyHash: modelConfigHash(k.KeyCipher), OwnerUserID: k.OwnerUserID}
	return site, k, identity, nil
}

func (s *Service) ListModelPolicies(ctx context.Context, siteID int64) ([]ModelPolicy, error) {
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, err
	}
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	return m.policies(ctx, siteID)
}
func (s *Service) SaveModelPolicy(ctx context.Context, siteID int64, p ModelPolicy) (*ModelPolicy, error) {
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.ID < 0 || p.Name == "" || len(p.Name) > 150 || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 || !validIntervalMinutes(p.IntervalMinutes) || p.DailyRequestLimit < 1 || p.DailyRequestLimit > 1000000 || p.FailureThreshold < 1 || p.FailureThreshold > 100 {
		return nil, ErrInvalid
	}
	if err = validateModelConfig(&p.Config); err != nil {
		return nil, err
	}
	if p.DailyRequestLimit < len(p.Config.Efforts)*len(p.Config.Templates)*p.Config.Samples {
		return nil, ErrInvalid
	}
	p.Recipients, err = normalizeBalanceRecipients(p.Recipients)
	if err != nil {
		return nil, err
	}
	_, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	var k *ManagedKey
	var identity modelIdentity
	var identityJSON any
	if p.Enabled || p.ID == 0 {
		_, k, identity, err = s.modelTarget(ctx, siteID, &p.Config)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(identity)
		identityJSON = string(raw)
	} else {
		// Disabling a broken policy remains possible after its upstream Key or
		// group disappears. It never schedules a billable operation.
		k, err = s.store.GetManagedKey(ctx, siteID, p.Config.ManagedKeyID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if k != nil {
			p.Config.Platform = k.Platform
		}
	}
	config, _ := json.Marshal(p.Config)
	recipients, _ := json.Marshal(p.Recipients)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return nil, err
	}
	if p.Enabled && k != nil {
		var legacy bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_governance_bindings WHERE site_id=$1 AND remote_group_id=$2 AND platform=$3 AND probe_model=$4 AND probe_enabled)`, siteID, k.RemoteGroupID, k.Platform, p.Config.Model).Scan(&legacy); err != nil {
			return nil, err
		}
		if legacy && !p.TakeOverLegacy {
			return nil, ErrConflict
		}
		if legacy {
			if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_bindings SET probe_enabled=FALSE WHERE site_id=$1 AND remote_group_id=$2 AND platform=$3 AND probe_model=$4`, siteID, k.RemoteGroupID, k.Platform, p.Config.Model); err != nil {
				return nil, err
			}
		}
	}
	if p.ID == 0 {
		if p.Version != 0 {
			return nil, ErrConflict
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_governance_model_policies(site_id,name,config,enabled,interval_minutes,daily_request_limit,notify_enabled,recipients,failure_threshold,next_run_at,identity) VALUES($1,$2,$3::jsonb,$4,$5,$6,$7,$8::jsonb,$9,$10,$11::jsonb) RETURNING id`, siteID, p.Name, string(config), p.Enabled, p.IntervalMinutes, p.DailyRequestLimit, p.NotifyEnabled, string(recipients), p.FailureThreshold, s.now(), identityJSON).Scan(&p.ID)
	} else {
		r, e := tx.ExecContext(ctx, `UPDATE upstream_governance_model_policies SET name=$3,config=$4::jsonb,enabled=$5,interval_minutes=$6,daily_request_limit=$7,notify_enabled=$8,recipients=$9::jsonb,failure_threshold=$10,version=version+1,next_run_at=$11,last_error='',updated_at=$11,failure_count=0,incident_state='healthy',quality_state='unknown',identity=COALESCE($13::jsonb,identity),notify_error='',notify_at=NULL WHERE site_id=$1 AND id=$2 AND version=$12`, siteID, p.ID, p.Name, string(config), p.Enabled, p.IntervalMinutes, p.DailyRequestLimit, p.NotifyEnabled, string(recipients), p.FailureThreshold, s.now(), p.Version, identityJSON)
		err = affected(r, e, ErrConflict)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.wakeModels()
	return m.policy(ctx, siteID, p.ID)
}
func (s *Service) DeleteModelPolicy(ctx context.Context, siteID, id, version int64) error {
	if siteID <= 0 || id <= 0 || version <= 0 {
		return ErrInvalid
	}
	m, err := s.models()
	if err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return err
	}
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM upstream_governance_model_policies WHERE site_id=$1 AND id=$2 FOR UPDATE`, siteID, id).Scan(&current); err != nil {
		return storeError(err)
	}
	if current != version {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_batches SET cancel_requested=TRUE WHERE site_id=$1 AND policy_id=$2 AND status IN ('queued','running')`, siteID, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM upstream_governance_model_policies WHERE site_id=$1 AND id=$2`, siteID, id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.wakeModels()
	return nil
}

func (s *Service) modelEnqueueInput(ctx context.Context, siteID int64, in ModelBatchInput, scheduled bool) (modelEnqueue, error) {
	e := modelEnqueue{SiteID: siteID, PolicyID: in.PolicyID, RequestID: in.RequestID, Config: in.Config, Now: s.now(), Scheduled: scheduled}
	if siteID <= 0 || len(in.RequestID) < 1 || len(in.RequestID) > 160 || strings.IndexFunc(in.RequestID, unicode.IsControl) >= 0 || in.PolicyID < 0 {
		return e, ErrInvalid
	}
	if in.PolicyID > 0 {
		m, err := s.models()
		if err != nil {
			return e, err
		}
		p, err := m.policy(ctx, siteID, in.PolicyID)
		if err != nil {
			return e, err
		}
		if !p.Enabled {
			return e, ErrConflict
		}
		e.Config = p.Config
		e.PolicyVersion = p.Version
	}
	_, _, identity, err := s.modelTarget(ctx, siteID, &e.Config)
	if err != nil {
		return e, err
	}
	e.Identity = identity
	if e.PolicyID > 0 {
		m, _ := s.models()
		var raw []byte
		if err = m.db.QueryRowContext(ctx, `SELECT identity FROM upstream_governance_model_policies WHERE site_id=$1 AND id=$2 AND version=$3`, siteID, e.PolicyID, e.PolicyVersion).Scan(&raw); err != nil {
			return e, storeError(err)
		}
		var saved modelIdentity
		if err = json.Unmarshal(raw, &saved); err != nil {
			return e, err
		}
		if !reflect.DeepEqual(identity, saved) {
			return e, errModelIdentityChanged
		}
	}
	e.TargetHash = modelConfigHash(struct {
		SiteID                       int64
		Group, Platform, Mode, Model string
	}{siteID, identity.GroupID, identity.Platform, e.Config.APIMode, e.Config.Model})
	e.RequestHash = modelConfigHash(struct {
		PolicyID int64
		Config   ModelTestConfig
	}{e.PolicyID, e.Config})
	return e, nil
}
func (s *Service) StartModelBatch(ctx context.Context, siteID int64, in ModelBatchInput) (*ModelBatch, error) {
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	_, release, err := s.siteLock(ctx, siteID)
	if err != nil {
		return nil, err
	}
	defer release()
	e, err := s.modelEnqueueInput(ctx, siteID, in, false)
	if err != nil {
		return nil, err
	}
	b, err := m.enqueue(ctx, e)
	if err == nil {
		s.wakeModels()
	}
	return b, err
}
func (s *Service) ListModelRuns(ctx context.Context, siteID int64, page, size int, batchID string) (*ModelRunPage, error) {
	if siteID <= 0 || page < 1 || size < 1 || size > 100 || page > 1000000 || len(batchID) > 100 {
		return nil, ErrInvalid
	}
	if _, err := s.store.GetSite(ctx, siteID); err != nil {
		return nil, err
	}
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	return m.listRuns(ctx, siteID, page, size, batchID)
}
func (s *Service) GetModelRun(ctx context.Context, siteID int64, id string) (*ModelRun, error) {
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	return m.getRun(ctx, siteID, id)
}
func (s *Service) CancelModelBatch(ctx context.Context, siteID int64, id string) error {
	m, err := s.models()
	if err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE upstream_governance_model_batches SET cancel_requested=TRUE WHERE site_id=$1 AND id=$2`, siteID, id)
	if err = affected(r, err, ErrNotFound); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_runs SET status='cancelled',finished_at=$3 WHERE site_id=$1 AND batch_id=$2 AND status='queued'`, siteID, id, s.now()); err != nil {
		return err
	}
	if err = modelSettleTx(ctx, tx, s.now()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.cancelActiveModels(id)
	s.wakeModels()
	return nil
}
func (s *Service) ReviewModelRun(ctx context.Context, siteID int64, id, review, note string, actorIDs ...int64) (*ModelRun, error) {
	if (review != "pending" && review != "pass" && review != "fail") || len(note) > 4000 {
		return nil, ErrInvalid
	}
	m, err := s.models()
	if err != nil {
		return nil, err
	}
	var actor any
	if len(actorIDs) > 0 && actorIDs[0] > 0 {
		actor = actorIDs[0]
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = modelAdmission(ctx, tx); err != nil {
		return nil, err
	}
	var policyID sql.NullInt64
	err = tx.QueryRowContext(ctx, `UPDATE upstream_governance_model_runs SET review=$3,review_note=$4,reviewer_id=$5,reviewed_at=$6,review_version=review_version+1 WHERE site_id=$1 AND id=$2 AND status NOT IN ('queued','running') RETURNING policy_id`, siteID, id, review, strings.TrimSpace(note), actor, s.now()).Scan(&policyID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if policyID.Valid {
		if err = s.recordModelQualityReview(ctx, tx, siteID, policyID.Int64); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return m.getRun(ctx, siteID, id)
}

func (s *Service) verifyModelIdentity(ctx context.Context, c modelClaim) (Site, RemoteKey, error) {
	config := c.Run.Request.Config
	site, k, identity, err := s.modelTarget(ctx, c.Run.SiteID, &config)
	if err != nil {
		return Site{}, RemoteKey{}, err
	}
	if !reflect.DeepEqual(identity, c.Identity) {
		return Site{}, RemoteKey{}, errModelIdentityChanged
	}
	key, err := s.decryptManagedKey(k.KeyCipher)
	if err != nil {
		return Site{}, RemoteKey{}, err
	}
	return *site, key, nil
}

func (s *Service) modelLegacyConflict(ctx context.Context, siteID int64, b *Binding, model string) (bool, error) {
	m, err := s.models()
	if errors.Is(err, ErrUnsupported) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var exists bool
	err = m.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_governance_model_policies p JOIN upstream_governance_keys k ON k.id=(p.config->>'managed_key_id')::bigint AND k.site_id=p.site_id WHERE p.site_id=$1 AND p.enabled AND k.remote_group_id=$2 AND k.platform=$3 AND p.config->>'model'=$4)
OR EXISTS(SELECT 1 FROM upstream_governance_model_batches WHERE site_id=$1 AND identity->>'group_id'=$2 AND identity->>'platform'=$3 AND config->>'model'=$4 AND status IN ('queued','running'))`, siteID, b.RemoteGroupID, b.Platform, model).Scan(&exists)
	return exists, err
}
