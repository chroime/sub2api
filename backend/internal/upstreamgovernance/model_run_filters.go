package upstreamgovernance

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ModelRunFilters apply before pagination. Status counts retain the full
// site/batch scope so filters cannot hide active work from the scheduler UI.
type ModelRunFilters struct {
	Query       string
	Status      string
	Template    string
	Effort      string
	Review      string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

func (f ModelRunFilters) Validate() error {
	if utf8.RuneCountInString(f.Query) > 200 || strings.ContainsRune(f.Query, 0) {
		return ErrInvalid
	}
	for _, field := range []struct {
		value   string
		allowed []string
	}{
		{f.Status, []string{"queued", "running", "succeeded", "failed", "cancelled", "indeterminate", "skipped"}},
		{f.Template, []string{"candy", "pelican", "token_audit", "context", "probe"}},
		{f.Effort, []string{"low", "medium", "high", "default"}},
		{f.Review, []string{"pending", "pass", "fail"}},
	} {
		if field.value != "" && !slices.Contains(field.allowed, field.value) {
			return ErrInvalid
		}
	}
	if f.CreatedFrom != nil && f.CreatedTo != nil && !f.CreatedFrom.Before(*f.CreatedTo) {
		return ErrInvalid
	}
	return nil
}

func modelRunWhere(siteID int64, batchID string, f ModelRunFilters) (string, []any) {
	where := `site_id=$1 AND ($2='' OR batch_id=$2)`
	args := []any{siteID, batchID}
	add := func(predicate string, value any) {
		args = append(args, value)
		where += " AND " + fmt.Sprintf(predicate, len(args))
	}
	if query := strings.TrimSpace(f.Query); query != "" {
		query = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		// EXISTS avoids duplicate run rows. Upstream group names come only from
		// this site's latest catalog; local group IDs are compared as text to
		// remain safe for legacy JSON snapshots containing nonnumeric values.
		add(`(request->'config'->>'model' ILIKE $%[1]d
 OR batch_id ILIKE $%[1]d
 OR request->'config'->>'local_group_id' ILIKE $%[1]d
 OR EXISTS(SELECT 1 FROM groups g WHERE g.id::text=request->'config'->>'local_group_id' AND request->'config'->>'target_type'='local_group' AND g.deleted_at IS NULL AND g.name ILIKE $%[1]d)
 OR EXISTS(SELECT 1 FROM upstream_governance_keys k WHERE k.site_id=upstream_governance_model_runs.site_id AND k.id::text=request->'config'->>'managed_key_id' AND COALESCE(request->'config'->>'target_type','upstream')<>'local_group' AND (k.remote_group_id ILIKE $%[1]d OR EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE((SELECT s.catalog->'groups' FROM upstream_governance_snapshots s WHERE s.site_id=k.site_id ORDER BY s.id DESC LIMIT 1),'[]'::jsonb)) rg WHERE rg->>'id'=k.remote_group_id AND rg->>'platform'=k.platform AND rg->>'name' ILIKE $%[1]d))))`, "%"+query+"%")
	}
	if f.Status != "" {
		add(`status=$%d`, f.Status)
	}
	if f.Template != "" {
		add(`request->>'template'=$%d`, f.Template)
	}
	if f.Effort != "" {
		add(`request->>'effort'=$%d`, f.Effort)
	}
	if f.Review != "" {
		add(`COALESCE(NULLIF(review,''),'pending')=$%d`, f.Review)
	}
	if f.CreatedFrom != nil {
		add(`created_at>=$%d`, *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		add(`created_at<$%d`, *f.CreatedTo)
	}
	return where, args
}
