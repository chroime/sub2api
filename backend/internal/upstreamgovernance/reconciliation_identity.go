package upstreamgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// This hash binds the saved governance Key to its original account and origin.
// Neither preview responses nor reconciliation metadata contain the Key itself.
func ManagedAccountIdentity(id int64, marker, platform, origin, key string) string {
	raw, _ := json.Marshal(struct {
		ID                            int64
		Marker, Platform, Origin, Key string
	}{id, marker, platform, origin, key})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) inspectReconciliationAccount(ctx context.Context, local ReconciliationLocal, site Site, binding Binding) (*ManagedLocalAccount, error) {
	a, err := local.InspectManagedAccount(ctx, binding)
	if err != nil || a == nil {
		return a, err
	}
	if s.cipher == nil || binding.KeyCipher == "" {
		return a, ErrConflict
	}
	raw, err := s.cipher.Decrypt(binding.KeyCipher)
	if err != nil {
		return a, ErrConflict
	}
	var key RemoteKey
	if json.Unmarshal([]byte(raw), &key) != nil || key.Key == "" {
		return a, ErrConflict
	}
	expected := ManagedAccountIdentity(binding.AccountID, binding.Marker, binding.Platform, site.BaseURL, key.Key)
	if a.Identity != expected {
		return a, ErrConflict
	}
	return a, nil
}
