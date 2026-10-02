package upstreamgovernance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Manual proof/visual verdicts are a separate incident stream from automated
// availability and token observations. They never rewrite the original answer.
func (s *Service) recordModelQualityReview(ctx context.Context, tx *sql.Tx, siteID, policyID int64) error {
	p, err := scanModelPolicy(tx.QueryRowContext(ctx, `SELECT `+modelPolicyColumns+` FROM upstream_governance_model_policies WHERE site_id=$1 AND id=$2 FOR UPDATE`, siteID, policyID))
	if err != nil {
		return err
	}
	var failed, passed int
	// Each template and reasoning level has its own most recent reviewed sample;
	// revisions of a policy never borrow the previous configuration's baseline.
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE review='fail'),count(*) FILTER(WHERE review='pass') FROM (
SELECT DISTINCT ON (r.request->>'template',r.request->>'effort') r.review FROM upstream_governance_model_runs r JOIN upstream_governance_model_batches b ON b.id=r.batch_id
WHERE r.policy_id=$1 AND b.policy_version=$2 AND r.review<>'pending' AND r.request->>'template' IN ('candy','pelican')
ORDER BY r.request->>'template',r.request->>'effort',r.created_at DESC,r.sequence DESC) latest`, policyID, p.Version).Scan(&failed, &passed)
	if err != nil {
		return err
	}
	state := "unknown"
	if failed > 0 {
		state = "failed"
	} else if passed > 0 {
		state = "healthy"
	}
	var old string
	var incident int64
	if err = tx.QueryRowContext(ctx, `SELECT quality_state,quality_incident_number FROM upstream_governance_model_policies WHERE id=$1`, policyID).Scan(&old, &incident); err != nil {
		return err
	}
	if state == old {
		return nil
	}
	incident++
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_governance_model_policies SET quality_state=$2,quality_incident_number=$3 WHERE id=$1`, policyID, state, incident); err != nil {
		return err
	}
	if !p.NotifyEnabled || !p.Enabled || old == "unknown" && state == "healthy" {
		return nil
	}
	kind := "quality_failed"
	if state == "healthy" {
		kind = "quality_recovered"
	} else if state == "unknown" {
		kind = "quality_review_retracted"
	}
	var name, baseURL string
	if err = tx.QueryRowContext(ctx, `SELECT name,base_url FROM upstream_governance_sites WHERE id=$1`, siteID).Scan(&name, &baseURL); err != nil {
		return err
	}
	notice := ModelNotice{SiteID: siteID, SiteName: name, BaseURL: baseURL, PolicyName: p.Name, Model: p.Config.Model, Kind: kind, Detail: fmt.Sprintf("Administrator-reviewed quality: failed tiers=%d, passed tiers=%d. Original model results are unchanged.", failed, passed), ObservedAt: s.now()}
	raw, _ := json.Marshal(notice)
	recipients, _ := json.Marshal(p.Recipients)
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_governance_model_notices(policy_id,incident_number,kind,notice,recipients,policy_version) VALUES($1,$2,$3,$4::jsonb,$5::jsonb,$6) ON CONFLICT DO NOTHING`, policyID, incident, kind, string(raw), string(recipients), p.Version)
	return err
}
