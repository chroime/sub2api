package upstreamgovernance

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Display metadata is deliberately separate from collection and monitoring.
// In particular, it can decorate a last-known snapshot after a login expires.
type quotaUSDReader interface {
	QuotaPerUSD(context.Context, Site) *float64
}

type quotaDisplayEntry struct {
	value   float64 // Zero means that no explicit USD conversion is available.
	expires time.Time
}

type quotaDisplayCache struct {
	mu      sync.Mutex
	entries map[string]quotaDisplayEntry
	flight  singleflight.Group
}

func quotaDisplayValue(value float64) *float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

// QuotaPerUSD uses only the upstream's explicit USD scale. Neither a universal
// quota divisor nor the site's top-up price/exchange rate is a safe substitute.
func (c *platformConnector) QuotaPerUSD(ctx context.Context, site Site) *float64 {
	if site.Platform != "newapi" || ctx.Err() != nil {
		return nil
	}
	proxyID := int64(0)
	if site.ProxyID != nil {
		proxyID = *site.ProxyID
	}
	key := site.BaseURL + "|" + strconv.FormatInt(proxyID, 10) + "|" + strconv.FormatInt(site.Version, 10)
	cache := &c.quotaDisplay
	cached := func() (float64, bool) {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		entry, ok := cache.entries[key]
		return entry.value, ok && time.Now().Before(entry.expires)
	}
	if value, ok := cached(); ok {
		return quotaDisplayValue(value)
	}
	result := cache.flight.DoChan(key, func() (any, error) {
		if value, ok := cached(); ok {
			return value, nil
		}
		requestCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		var status struct {
			QuotaPerUnit      float64 `json:"quota_per_unit"`
			QuotaDisplayType  string  `json:"quota_display_type"`
			DisplayInCurrency *bool   `json:"display_in_currency"`
		}
		value := 0.0
		err := c.data(requestCtx, site, Session{UserAgent: defaultSessionUserAgent}, http.MethodGet, "/api/status", nil, &status)
		if err == nil && strings.EqualFold(strings.TrimSpace(status.QuotaDisplayType), "USD") &&
			(status.DisplayInCurrency == nil || *status.DisplayInCurrency) && quotaDisplayValue(status.QuotaPerUnit) != nil {
			value = status.QuotaPerUnit
		}
		// A canceled UI read must not poison the shared cache for the next read.
		if ctx.Err() == nil {
			ttl := 30 * time.Second
			if value > 0 {
				ttl = 5 * time.Minute
			}
			cache.mu.Lock()
			if cache.entries == nil {
				cache.entries = make(map[string]quotaDisplayEntry)
			}
			if len(cache.entries) >= 128 {
				oldestKey := ""
				var oldest time.Time
				for k, entry := range cache.entries {
					if oldest.IsZero() || entry.expires.Before(oldest) {
						oldestKey, oldest = k, entry.expires
					}
				}
				delete(cache.entries, oldestKey)
			}
			cache.entries[key] = quotaDisplayEntry{value: value, expires: time.Now().Add(ttl)}
			cache.mu.Unlock()
		}
		return value, nil
	})
	select {
	case <-ctx.Done():
		return nil
	case r := <-result:
		value, _ := r.Val.(float64)
		return quotaDisplayValue(value)
	}
}

func (s *Service) decorateSiteBalances(ctx context.Context, sites []Site) {
	reader, ok := s.connector.(quotaUSDReader)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// A slow upstream must not serialize or indefinitely block the site rail.
	slots := make(chan struct{}, 4)
	var pending sync.WaitGroup
enqueue:
	for i := range sites {
		if sites[i].Platform != "newapi" || sites[i].Balance == nil || sites[i].Balance.Unit != "quota" {
			continue
		}
		select {
		case <-ctx.Done():
			break enqueue
		case slots <- struct{}{}:
		}
		pending.Add(1)
		go func(i int) {
			defer pending.Done()
			defer func() { <-slots }()
			balance := *sites[i].Balance
			balance.QuotaPerUSD = reader.QuotaPerUSD(ctx, sites[i])
			sites[i].Balance = &balance
		}(i)
	}
	pending.Wait()
}
