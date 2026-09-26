package upstreamgovernance

import (
	"context"
	"strconv"
	"strings"
)

type connectorKey struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Group   string `json:"group"`
	GroupID *int64 `json:"group_id"`
	Key     string `json:"key"`
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
			return nil, e
		}
		if data.Total == nil || *data.Total < 0 || *data.Total > 2000 || len(data.Items) > 100 || data.Page != 0 && data.Page != page {
			return nil, ErrUnsupported
		}
		if expected < 0 {
			expected = *data.Total
		} else if expected != *data.Total {
			return nil, ErrConflict
		}
		for _, key := range data.Items {
			if key.ID <= 0 || seen[key.ID] {
				return nil, ErrUnsupported
			}
			seen[key.ID] = true
			out = append(out, key)
		}
		if len(out) == expected {
			return out, nil
		}
		if len(out) > expected || len(data.Items) == 0 {
			return nil, ErrUnsupported
		}
	}
	return nil, ErrUnsupported
}
func connectorFindKey(keys []connectorKey, group RemoteGroup, marker, platform string) (*connectorKey, error) {
	var match *connectorKey
	for i := range keys {
		key := &keys[i]
		if key.Name != marker {
			continue
		}
		correct := key.Group == group.ID
		if platform == "sub2api" {
			correct = key.GroupID != nil && strconv.FormatInt(*key.GroupID, 10) == group.ID
		}
		if !correct || match != nil {
			return nil, ErrConflict
		}
		match = key
	}
	return match, nil
}
func (c *platformConnector) EnsureKey(ctx context.Context, s Site, session Session, group RemoteGroup, marker string) (RemoteKey, error) {
	if marker == "" || len(marker) > 128 || group.ID == "" {
		return RemoteKey{}, ErrInvalid
	}
	session, e := c.identity(ctx, s, session)
	if e != nil {
		return RemoteKey{}, e
	}
	keys, e := c.keyList(ctx, s, session)
	if e != nil {
		return RemoteKey{}, e
	}
	match, e := connectorFindKey(keys, group, marker, s.Platform)
	if e != nil {
		return RemoteKey{}, e
	}
	if match == nil {
		path := "/api/token/"
		body := map[string]any{"name": marker, "group": group.ID, "expired_time": -1, "unlimited_quota": true, "model_limits_enabled": false, "cross_group_retry": false, "auto_groups": []string{}}
		if s.Platform == "sub2api" {
			id, e := strconv.ParseInt(group.ID, 10, 64)
			if e != nil || id <= 0 {
				return RemoteKey{}, ErrInvalid
			}
			path = "/api/v1/keys"
			body = map[string]any{"name": marker, "group_id": id}
		}
		// This POST is never retried. A later explicit Apply starts with a complete
		// list and can recover a committed key whose response was lost.
		if e = c.data(ctx, s, session, "POST", path, body, nil); e != nil {
			return RemoteKey{}, errConnectorUncertain
		}
		keys, e = c.keyList(ctx, s, session)
		if e != nil {
			return RemoteKey{}, errConnectorUncertain
		}
		match, e = connectorFindKey(keys, group, marker, s.Platform)
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
			return RemoteKey{}, e
		}
		key = response.Key
	} else if key == "" || strings.Contains(key, "*") {
		var response connectorKey
		if e = c.data(ctx, s, session, "GET", "/api/v1/keys/"+id, nil, &response); e != nil {
			return RemoteKey{}, e
		}
		if response.ID != match.ID {
			return RemoteKey{}, ErrUnsupported
		}
		key = response.Key
	}
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "*\r\n\t ") || strings.Contains(key, "…") || strings.Contains(key, "...") {
		return RemoteKey{}, ErrUnsupported
	}
	return RemoteKey{ID: id, Key: key}, nil
}
