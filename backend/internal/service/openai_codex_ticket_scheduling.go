package service

import (
	"context"
	"time"
)

// prioritizeOpenAICodex332Tickets reorders only the existing 332 slots within
// each priority. Other modes keep their positions, and the caller subsequently
// applies any configured upstream-rate and compact-support ordering. A global
// comparator that compares readiness only when both operands select 332 would
// not be transitive in a mixed-mode pool.
func (s *OpenAIGatewayService) prioritizeOpenAICodex332Tickets(accounts []*Account, requestedModel string, requireCompact bool) {
	if s == nil || len(accounts) < 2 {
		return
	}
	cfg := s.openAICodexTicketRuntimeConfig(context.Background(), openAICodexTicketMode332)
	if !cfg.Enabled {
		return
	}
	type group struct {
		positions []int
		ready     []*Account
		missing   []*Account
	}
	groups := map[int]*group{}
	for i, account := range accounts {
		if OpenAICodexTicketMode(account) != openAICodexTicketMode332 {
			continue
		}
		model := s.openAICodexTicketOutboundModel(account, requestedModel, requireCompact)
		if !openAICodexTicketGatesModel(cfg, model) {
			continue
		}
		g := groups[account.Priority]
		if g == nil {
			g = &group{}
			groups[account.Priority] = g
		}
		g.positions = append(g.positions, i)
		if s.lookupOpenAICodexTicket(account, model).valid(time.Now(), cfg.TargetLength) {
			g.ready = append(g.ready, account)
		} else {
			g.missing = append(g.missing, account)
		}
	}
	for _, g := range groups {
		ordered := append(g.ready, g.missing...)
		for i, position := range g.positions {
			accounts[position] = ordered[i]
		}
	}
}

func (s *OpenAIGatewayService) prioritizeOpenAICodex332LoadTickets(accounts []accountWithLoad, requestedModel string, requireCompact bool) {
	ordered := make([]*Account, len(accounts))
	loads := make(map[int64]*AccountLoadInfo, len(accounts))
	for i, account := range accounts {
		ordered[i] = account.account
		loads[account.account.ID] = account.loadInfo
	}
	s.prioritizeOpenAICodex332Tickets(ordered, requestedModel, requireCompact)
	for i, account := range ordered {
		accounts[i] = accountWithLoad{account: account, loadInfo: loads[account.ID]}
	}
}
