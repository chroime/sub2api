package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyBalancePrecharge        = "balance_precharge"
	balancePrechargeGroupKeyPrefix    = "balance_precharge_group_"
	balancePrechargeSettingsCacheTTL  = 5 * time.Second
	balancePrechargeSettingsDBTimeout = time.Second
	balancePrechargeSettingsMaxGroups = 4096
	// Keep eight-decimal money values within a useful, safely representable range.
	BalancePrechargeMaxSettingUSD = 1000000
)

type BalancePrechargeSettings struct {
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold"`
	Amount    float64 `json:"amount"`
}

type GroupBalancePrechargeSettings struct {
	Mode      string  `json:"mode"`
	Threshold float64 `json:"threshold"`
	Amount    float64 `json:"amount"`
}

type cachedBalancePrechargeSettings struct {
	settings  BalancePrechargeSettings
	expiresAt time.Time
}

type cachedGroupBalancePrechargeSettings struct {
	settings  GroupBalancePrechargeSettings
	expiresAt time.Time
}

func validateBalancePrechargeMoney(threshold, amount float64, required bool) error {
	invalid := func(message string) error {
		return infraerrors.BadRequest("INVALID_BALANCE_PRECHARGE_SETTINGS", message)
	}
	for _, value := range []float64{threshold, amount} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > BalancePrechargeMaxSettingUSD {
			return invalid("Precharge amounts must be finite USD values between 0 and 1000000")
		}
		decimal := strconv.FormatFloat(value, 'f', -1, 64)
		if dot := strings.IndexByte(decimal, '.'); dot >= 0 && len(decimal)-dot-1 > 8 {
			return invalid("Precharge amounts may have at most eight decimal places")
		}
	}
	if required && (threshold <= 0 || amount <= 0 || amount > threshold) {
		return invalid("Precharge settings require 0 < amount <= threshold")
	}
	return nil
}

func validateGroupBalancePrechargeSettings(settings GroupBalancePrechargeSettings) error {
	if settings.Mode != "inherit" && settings.Mode != "custom" {
		return infraerrors.BadRequest("INVALID_BALANCE_PRECHARGE_SETTINGS", "Precharge group mode must be inherit or custom")
	}
	return validateBalancePrechargeMoney(settings.Threshold, settings.Amount, settings.Mode == "custom")
}

func (s *SettingService) balancePrechargeRepositoryAvailable() error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("balance precharge settings repository is unavailable")
	}
	return nil
}

// Group administration validates the referenced, persisted group. Runtime reads
// already receive the authenticated billing group and avoid a second group query.
func (s *SettingService) validateBalancePrechargeGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 {
		return infraerrors.BadRequest("INVALID_GROUP_ID", "Group ID must be positive")
	}
	if s.defaultSubGroupReader == nil {
		return fmt.Errorf("balance precharge group reader is unavailable")
	}
	group, err := s.defaultSubGroupReader.GetByID(ctx, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return ErrGroupNotFound
	}
	return nil
}

func (s *SettingService) readBalancePrechargeSettings(ctx context.Context) (BalancePrechargeSettings, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyBalancePrecharge)
	if errors.Is(err, ErrSettingNotFound) {
		return BalancePrechargeSettings{}, nil
	}
	if err != nil {
		return BalancePrechargeSettings{}, fmt.Errorf("get balance precharge settings: %w", err)
	}
	var stored struct {
		Enabled   *bool    `json:"enabled"`
		Threshold *float64 `json:"threshold"`
		Amount    *float64 `json:"amount"`
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || stored.Enabled == nil || stored.Threshold == nil || stored.Amount == nil {
		return BalancePrechargeSettings{}, fmt.Errorf("invalid stored balance precharge settings")
	}
	settings := BalancePrechargeSettings{Enabled: *stored.Enabled, Threshold: *stored.Threshold, Amount: *stored.Amount}
	if err := validateBalancePrechargeMoney(settings.Threshold, settings.Amount, settings.Enabled); err != nil {
		return BalancePrechargeSettings{}, fmt.Errorf("invalid stored balance precharge settings: %v", err)
	}
	return settings, nil
}

func (s *SettingService) readGroupBalancePrechargeSettings(ctx context.Context, groupID int64) (GroupBalancePrechargeSettings, error) {
	raw, err := s.settingRepo.GetValue(ctx, balancePrechargeGroupKeyPrefix+strconv.FormatInt(groupID, 10))
	if errors.Is(err, ErrSettingNotFound) {
		return GroupBalancePrechargeSettings{Mode: "inherit"}, nil
	}
	if err != nil {
		return GroupBalancePrechargeSettings{}, fmt.Errorf("get group balance precharge settings: %w", err)
	}
	var stored struct {
		Mode      string   `json:"mode"`
		Threshold *float64 `json:"threshold"`
		Amount    *float64 `json:"amount"`
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || stored.Threshold == nil || stored.Amount == nil {
		return GroupBalancePrechargeSettings{}, fmt.Errorf("invalid stored group balance precharge settings")
	}
	settings := GroupBalancePrechargeSettings{Mode: stored.Mode, Threshold: *stored.Threshold, Amount: *stored.Amount}
	if err := validateGroupBalancePrechargeSettings(settings); err != nil {
		return GroupBalancePrechargeSettings{}, fmt.Errorf("invalid stored group balance precharge settings: %v", err)
	}
	return settings, nil
}

// GetBalancePrechargeSettings reads persisted settings for administration.
func (s *SettingService) GetBalancePrechargeSettings(ctx context.Context) (BalancePrechargeSettings, error) {
	if err := s.balancePrechargeRepositoryAvailable(); err != nil {
		return BalancePrechargeSettings{}, err
	}
	s.balancePrechargeMu.Lock()
	defer s.balancePrechargeMu.Unlock()
	settings, err := s.readBalancePrechargeSettings(ctx)
	if err != nil {
		return BalancePrechargeSettings{}, err
	}
	s.balancePrechargeGlobal = cachedBalancePrechargeSettings{settings: settings, expiresAt: time.Now().Add(balancePrechargeSettingsCacheTTL)}
	return settings, nil
}

func (s *SettingService) SetBalancePrechargeSettings(ctx context.Context, settings BalancePrechargeSettings) error {
	if err := s.balancePrechargeRepositoryAvailable(); err != nil {
		return err
	}
	if err := validateBalancePrechargeMoney(settings.Threshold, settings.Amount, settings.Enabled); err != nil {
		return err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	s.balancePrechargeMu.Lock()
	defer s.balancePrechargeMu.Unlock()
	if err := s.settingRepo.Set(ctx, SettingKeyBalancePrecharge, string(raw)); err != nil {
		return fmt.Errorf("save balance precharge settings: %w", err)
	}
	s.balancePrechargeGlobal = cachedBalancePrechargeSettings{settings: settings, expiresAt: time.Now().Add(balancePrechargeSettingsCacheTTL)}
	return nil
}

func (s *SettingService) GetGroupBalancePrechargeSettings(ctx context.Context, groupID int64) (GroupBalancePrechargeSettings, error) {
	if err := s.balancePrechargeRepositoryAvailable(); err != nil {
		return GroupBalancePrechargeSettings{}, err
	}
	if err := s.validateBalancePrechargeGroup(ctx, groupID); err != nil {
		return GroupBalancePrechargeSettings{}, err
	}
	s.balancePrechargeMu.Lock()
	defer s.balancePrechargeMu.Unlock()
	settings, err := s.readGroupBalancePrechargeSettings(ctx, groupID)
	if err != nil {
		return GroupBalancePrechargeSettings{}, err
	}
	s.storeGroupBalancePrechargeSettings(groupID, settings)
	return settings, nil
}

func (s *SettingService) SetGroupBalancePrechargeSettings(ctx context.Context, groupID int64, settings GroupBalancePrechargeSettings) error {
	if err := s.balancePrechargeRepositoryAvailable(); err != nil {
		return err
	}
	if err := validateGroupBalancePrechargeSettings(settings); err != nil {
		return err
	}
	if err := s.validateBalancePrechargeGroup(ctx, groupID); err != nil {
		return err
	}
	if settings.Mode == "inherit" {
		settings.Threshold, settings.Amount = 0, 0
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	s.balancePrechargeMu.Lock()
	defer s.balancePrechargeMu.Unlock()
	// A separate key per group prevents one admin's edit overwriting another group.
	if err := s.settingRepo.Set(ctx, balancePrechargeGroupKeyPrefix+strconv.FormatInt(groupID, 10), string(raw)); err != nil {
		return fmt.Errorf("save group balance precharge settings: %w", err)
	}
	s.storeGroupBalancePrechargeSettings(groupID, settings)
	return nil
}

// Caller holds balancePrechargeMu. Limit both cache lifetime and cardinality.
func (s *SettingService) storeGroupBalancePrechargeSettings(groupID int64, settings GroupBalancePrechargeSettings) {
	if s.balancePrechargeGroups == nil || len(s.balancePrechargeGroups) >= balancePrechargeSettingsMaxGroups {
		s.balancePrechargeGroups = make(map[int64]cachedGroupBalancePrechargeSettings)
	}
	s.balancePrechargeGroups[groupID] = cachedGroupBalancePrechargeSettings{settings: settings, expiresAt: time.Now().Add(balancePrechargeSettingsCacheTTL)}
}

// GetEffectiveBalancePrechargeSettings is the request-path policy snapshot.
// Expired settings must refresh successfully: errors are never interpreted as a
// disabled policy. The global switch remains authoritative for custom groups.
func (s *SettingService) GetEffectiveBalancePrechargeSettings(ctx context.Context, groupID int64) (BalancePrechargeSettings, error) {
	if err := s.balancePrechargeRepositoryAvailable(); err != nil {
		return BalancePrechargeSettings{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(ctx, balancePrechargeSettingsDBTimeout)
	defer cancel()
	s.balancePrechargeMu.Lock()
	defer s.balancePrechargeMu.Unlock()
	if !time.Now().Before(s.balancePrechargeGlobal.expiresAt) {
		settings, err := s.readBalancePrechargeSettings(dbCtx)
		if err != nil {
			return BalancePrechargeSettings{}, err
		}
		s.balancePrechargeGlobal = cachedBalancePrechargeSettings{settings: settings, expiresAt: time.Now().Add(balancePrechargeSettingsCacheTTL)}
	}
	effective := s.balancePrechargeGlobal.settings
	if !effective.Enabled || groupID <= 0 {
		return effective, nil
	}
	group, ok := s.balancePrechargeGroups[groupID]
	if !ok || !time.Now().Before(group.expiresAt) {
		settings, err := s.readGroupBalancePrechargeSettings(dbCtx, groupID)
		if err != nil {
			return BalancePrechargeSettings{}, err
		}
		s.storeGroupBalancePrechargeSettings(groupID, settings)
		group = s.balancePrechargeGroups[groupID]
	}
	if group.settings.Mode == "custom" {
		effective.Threshold, effective.Amount = group.settings.Threshold, group.settings.Amount
	}
	return effective, nil
}
