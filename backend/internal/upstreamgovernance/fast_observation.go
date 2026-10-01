package upstreamgovernance

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// ObserveGroups performs the minimum authenticated reads needed to establish
// a complete visible group/rate fact. It intentionally never calls channels,
// plaza, model, or pricing expansion endpoints.
func (c *platformConnector) ObserveGroups(ctx context.Context, s Site, session Session) (GroupObservation, error) {
	verified, account, err := c.profile(ctx, s, session)
	if err != nil {
		return GroupObservation{}, err
	}
	var groups []RemoteGroup
	switch s.Platform {
	case "sub2api":
		groups, err = c.fastSubGroups(ctx, s, verified)
	case "newapi":
		groups, err = c.fastNewAPIGroups(ctx, s, verified)
	default:
		return GroupObservation{}, ErrUnsupported
	}
	if err != nil {
		return GroupObservation{}, err
	}
	if !groupsComplete(groups) {
		return GroupObservation{}, ErrUnsupported
	}
	return GroupObservation{ObservedAt: time.Now().UTC(), SourceUserID: account.UserID, GroupsComplete: true, Groups: groups}, nil
}

func (c *platformConnector) fastSubGroups(ctx context.Context, s Site, session Session) ([]RemoteGroup, error) {
	var available []struct {
		ID          int64    `json:"id"`
		Name        string   `json:"name"`
		Platform    string   `json:"platform"`
		Rate        *float64 `json:"rate_multiplier"`
		PeakEnabled bool     `json:"peak_rate_enabled"`
		PeakStart   string   `json:"peak_start"`
		PeakEnd     string   `json:"peak_end"`
		PeakRate    *float64 `json:"peak_rate_multiplier"`
	}
	if err := c.data(ctx, s, session, "GET", "/api/v1/groups/available", nil, &available); err != nil {
		return nil, err
	}
	if available == nil || len(available) > connectorMaxGroups {
		return nil, ErrUnsupported
	}
	var rates map[string]*float64
	if err := c.data(ctx, s, session, "GET", "/api/v1/groups/rates", nil, &rates); err != nil {
		return nil, err
	}
	if rates == nil {
		return nil, ErrUnsupported
	}
	seen := make(map[string]bool, len(available))
	out := make([]RemoteGroup, 0, len(available))
	for _, group := range available {
		if group.ID <= 0 || group.Name == "" || !connectorValidRate(group.Rate) || !connectorValidRate(group.PeakRate) {
			return nil, ErrUnsupported
		}
		id := strconv.FormatInt(group.ID, 10)
		if seen[id] {
			return nil, ErrUnsupported
		}
		seen[id] = true
		rate, ok := rates[id]
		if !ok || rate == nil || !connectorValidRate(rate) {
			return nil, ErrUnsupported
		}
		out = append(out, RemoteGroup{ID: id, Name: group.Name, Platform: group.Platform, RateMultiplier: group.Rate, UserRateMultiplier: rate, ResolvedRateMultiplier: rate, PeakRateEnabled: group.PeakEnabled, PeakStart: group.PeakStart, PeakEnd: group.PeakEnd, PeakRateMultiplier: group.PeakRate, Models: []string{}, Prices: []RemotePrice{}, Source: "sub2api:user-visible-groups"})
	}
	return out, nil
}

func (c *platformConnector) fastNewAPIGroups(ctx context.Context, s Site, session Session) ([]RemoteGroup, error) {
	var groups map[string]struct {
		Ratio json.RawMessage `json:"ratio"`
		Desc  string          `json:"desc"`
	}
	if err := c.data(ctx, s, session, "GET", "/api/user/self/groups", nil, &groups); err != nil {
		return nil, err
	}
	if groups == nil || len(groups) > connectorMaxGroups {
		return nil, ErrUnsupported
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]RemoteGroup, 0, len(ids))
	for _, id := range ids {
		value, ok := parseNewAPIRatio(groups[id].Ratio)
		if !ok {
			return nil, ErrUnsupported
		}
		out = append(out, RemoteGroup{ID: id, Name: groups[id].Desc, Platform: "unknown", RateMultiplier: value, UserRateMultiplier: value, ResolvedRateMultiplier: value, Models: []string{}, Prices: []RemotePrice{}, Source: "newapi:user-self-groups"})
	}
	return out, nil
}

func parseNewAPIRatio(raw json.RawMessage) (*float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil && connectorValidRate(&number) {
		return &number, true
	}
	// New API represents automatic pricing as a string. It is deliberately
	// unknown for margin calculations, never silently converted to zero.
	return nil, false
}

func groupsComplete(groups []RemoteGroup) bool {
	if groups == nil || len(groups) > connectorMaxGroups {
		return false
	}
	for _, group := range groups {
		if group.ID == "" || group.Name == "" || group.ResolvedRateMultiplier == nil || !connectorValidRate(group.ResolvedRateMultiplier) {
			return false
		}
	}
	return true
}

func groupObservationFingerprint(observation GroupObservation) (string, error) {
	if !observation.GroupsComplete || !groupsComplete(observation.Groups) {
		return "", ErrUnsupported
	}
	groups := append([]RemoteGroup(nil), observation.Groups...)
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	type fact struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Platform    string   `json:"platform"`
		Rate        *float64 `json:"rate"`
		PeakEnabled bool     `json:"peak_enabled"`
		PeakStart   string   `json:"peak_start,omitempty"`
		PeakEnd     string   `json:"peak_end,omitempty"`
		PeakRate    *float64 `json:"peak_rate,omitempty"`
	}
	facts := make([]fact, 0, len(groups))
	for _, group := range groups {
		facts = append(facts, fact{ID: group.ID, Name: group.Name, Platform: group.Platform, Rate: group.ResolvedRateMultiplier, PeakEnabled: group.PeakRateEnabled, PeakStart: group.PeakStart, PeakEnd: group.PeakEnd, PeakRate: group.PeakRateMultiplier})
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

var _ FastObservationConnector = (*platformConnector)(nil)
