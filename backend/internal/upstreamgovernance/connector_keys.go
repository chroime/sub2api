package upstreamgovernance

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

type connectorKey struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	Group   json.RawMessage `json:"group"`
	GroupID *int64          `json:"group_id"`
	Key     string          `json:"key"`
}

// Native Sub2API embeds a group object and identifies its binding through
// group_id. New API instead uses a string group name; retain its strict shape.
func (key connectorKey) newAPIGroup() (string, error) {
	var group string
	if len(key.Group) > 0 && json.Unmarshal(key.Group, &group) != nil {
		return "", ErrUnsupported
	}
	return group, nil
}

func connectorKeyFailure(site Site, stage string, err error) error {
	// Upstream payloads can contain full inference keys. Log only fixed stages
	// and our sanitized error category, never remote bodies or credentials.
	slog.Warn("upstream governance key operation failed", "site_id", site.ID, "platform", site.Platform, "stage", stage, "error_code", ErrorCode(err))
	return err
}

func (c *platformConnector) keyList(ctx context.Context, s Site, session Session) ([]connectorKey, error) {
	out := []connectorKey{}
	seen := map[int64]bool{}
	expected := -1
	for page := 1; page <= 20; page++ {
		path := "/api/v1/keys?page=" + strconv.Itoa(page) + "&page_size=100"
		if s.Platform == "newapi" {
			path = "/api/token/?p=" + strconv.Itoa(page) + "&page_size=100"
		}
		var data struct {
			Items []connectorKey `json:"items"`
			Total *int           `json:"total"`
			Page  int            `json:"page"`
		}
		if e := c.data(ctx, s, session, "GET", path, nil, &data); e != nil {
			return nil, connectorKeyFailure(s, "list", e)
		}
		if data.Total == nil || *data.Total < 0 || *data.Total > 2000 || len(data.Items) > 100 || data.Page != 0 && data.Page != page {
			slog.Warn("upstream governance key pagination unsupported", "site_id", s.ID, "has_total", data.Total != nil, "item_count", len(data.Items), "requested_page", page, "returned_page", data.Page)
			return nil, ErrUnsupported
		}
		if expected < 0 {
			expected = *data.Total
		} else if expected != *data.Total {
			return nil, ErrConflict
		}
		for _, key := range data.Items {
			if key.ID <= 0 || seen[key.ID] {
				return nil, connectorKeyFailure(s, "list_item_identity", ErrUnsupported)
			}
			if s.Platform == "newapi" {
				if _, e := key.newAPIGroup(); e != nil {
					return nil, connectorKeyFailure(s, "list_item_group", e)
				}
			}
			seen[key.ID] = true
			out = append(out, key)
		}
		if len(out) == expected {
			return out, nil
		}
		if len(out) > expected || len(data.Items) == 0 {
			return nil, connectorKeyFailure(s, "list_incomplete", ErrUnsupported)
		}
	}
	return nil, connectorKeyFailure(s, "list_page_limit", ErrUnsupported)
}
func connectorFindKey(keys []connectorKey, group RemoteGroup, marker, platform string) (*connectorKey, error) {
	var match *connectorKey
	for i := range keys {
		key := &keys[i]
		if key.Name != marker {
			continue
		}
		correct := false
		if platform == "sub2api" {
			correct = key.GroupID != nil && strconv.FormatInt(*key.GroupID, 10) == group.ID
		} else {
			name, e := key.newAPIGroup()
			if e != nil {
				return nil, e
			}
			correct = name == group.ID
		}
		if !correct || match != nil {
			return nil, ErrConflict
		}
		match = key
	}
	return match, nil
}
func connectorKeyInGroup(key connectorKey, group RemoteGroup, platform string) (bool, error) {
	if platform == "sub2api" {
		return key.GroupID != nil && strconv.FormatInt(*key.GroupID, 10) == group.ID, nil
	}
	name, err := key.newAPIGroup()
	return name == group.ID, err
}

func validKeyDisplayName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 100 && strings.IndexFunc(name, unicode.IsControl) < 0
}

// Capture same-name IDs before the first creation attempt. Human-readable names
// need not be unique: other groups, protocols or manually created keys may use
// the same name. Persist this inventory before POST so retries do not adopt them.
func (c *platformConnector) PrepareKey(ctx context.Context, s Site, session Session, group RemoteGroup, name string) ([]int64, error) {
	if !validKeyDisplayName(name) || group.ID == "" {
		return nil, ErrInvalid
	}
	session, err := c.identity(ctx, s, session)
	if err != nil {
		return nil, connectorKeyFailure(s, "identity", err)
	}
	keys, err := c.keyList(ctx, s, session)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, key := range keys {
		if key.Name != name {
			continue
		}
		matches, err := connectorKeyInGroup(key, group, s.Platform)
		if err != nil {
			return nil, err
		}
		if matches {
			ids = append(ids, key.ID)
		}
	}
	return ids, nil
}

func connectorFindPlannedKey(keys []connectorKey, group RemoteGroup, marker, platform string, plan *KeyCreationPlan) (*connectorKey, error) {
	if plan == nil {
		return connectorFindKey(keys, group, marker, platform)
	}
	excluded := make(map[int64]bool, len(plan.ExistingIDs))
	for _, id := range plan.ExistingIDs {
		excluded[id] = true
	}
	var match *connectorKey
	for i := range keys {
		key := &keys[i]
		if key.Name != plan.Name || excluded[key.ID] {
			continue
		}
		correct, err := connectorKeyInGroup(*key, group, platform)
		if err != nil {
			return nil, err
		}
		if !correct {
			continue
		}
		if match != nil {
			return nil, ErrConflict
		}
		match = key
	}
	return match, nil
}

func (c *platformConnector) EnsureKey(ctx context.Context, s Site, session Session, group RemoteGroup, marker string, plan *KeyCreationPlan) (RemoteKey, error) {
	if marker == "" || len(marker) > 128 || group.ID == "" {
		return RemoteKey{}, ErrInvalid
	}
	name := marker
	if plan != nil {
		if !validKeyDisplayName(plan.Name) || plan.ExistingIDs == nil || len(plan.ExistingIDs) > 2000 {
			return RemoteKey{}, ErrInvalid
		}
		seen := map[int64]bool{}
		for _, id := range plan.ExistingIDs {
			if id <= 0 || seen[id] {
				return RemoteKey{}, ErrInvalid
			}
			seen[id] = true
		}
		name = plan.Name
	}
	session, e := c.identity(ctx, s, session)
	if e != nil {
		return RemoteKey{}, connectorKeyFailure(s, "identity", e)
	}
	keys, e := c.keyList(ctx, s, session)
	if e != nil {
		return RemoteKey{}, e
	}
	match, e := connectorFindPlannedKey(keys, group, marker, s.Platform, plan)
	if e != nil {
		return RemoteKey{}, e
	}
	if match == nil {
		path := "/api/token/"
		body := map[string]any{"name": name, "group": group.ID, "expired_time": -1, "unlimited_quota": true, "model_limits_enabled": false, "cross_group_retry": false, "auto_groups": []string{}}
		var headers http.Header
		if s.Platform == "sub2api" {
			id, e := strconv.ParseInt(group.ID, 10, 64)
			if e != nil || id <= 0 {
				return RemoteKey{}, ErrInvalid
			}
			path = "/api/v1/keys"
			body = map[string]any{"name": name, "group_id": id}
			headers = make(http.Header)
			headers.Set("Idempotency-Key", marker)
		}
		// Each explicit operation issues at most one creation POST. Native
		// Sub2API requires a stable idempotency key and unchanged payload to
		// join/replay an earlier request even before its key is list-visible.
		// New API only offers best-effort reconciliation through the key list.
		if _, _, e = c.request(ctx, s, session, "POST", path, body, headers, true); e != nil {
			connectorKeyFailure(s, "create", e)
			return RemoteKey{}, errConnectorUncertain
		}
		keys, e = c.keyList(ctx, s, session)
		if e != nil {
			return RemoteKey{}, errConnectorUncertain
		}
		match, e = connectorFindPlannedKey(keys, group, marker, s.Platform, plan)
		if e != nil || match == nil {
			return RemoteKey{}, errConnectorUncertain
		}
	}
	id := strconv.FormatInt(match.ID, 10)
	key := match.Key
	if s.Platform == "newapi" {
		var response struct {
			Key string `json:"key"`
		}
		if e = c.data(ctx, s, session, "POST", "/api/token/"+id+"/key", nil, &response); e != nil {
			return RemoteKey{}, connectorKeyFailure(s, "reveal", e)
		}
		key = response.Key
	} else if key == "" || strings.Contains(key, "*") {
		var response connectorKey
		if e = c.data(ctx, s, session, "GET", "/api/v1/keys/"+id, nil, &response); e != nil {
			return RemoteKey{}, connectorKeyFailure(s, "read", e)
		}
		if response.ID != match.ID {
			return RemoteKey{}, connectorKeyFailure(s, "read_identity", ErrUnsupported)
		}
		key = response.Key
	}
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "*\r\n\t ") || strings.Contains(key, "…") || strings.Contains(key, "...") {
		return RemoteKey{}, connectorKeyFailure(s, "plaintext_unavailable", ErrUnsupported)
	}
	return RemoteKey{ID: id, Key: key}, nil
}
