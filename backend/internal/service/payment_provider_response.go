package service

import dbent "github.com/Wei-Shaw/sub2api/ent"

// ProviderInstanceResponseForAdmin uses the same secret filtering for create,
// update and list. An Ent entity's Config field is internal storage, not an API.
func (s *PaymentConfigService) ProviderInstanceResponseForAdmin(inst *dbent.PaymentProviderInstance) (*ProviderInstanceResponse, error) {
	cfg, err := s.decryptAndMaskConfig(inst.ProviderKey, inst.Config)
	if err != nil {
		return nil, err
	}
	return &ProviderInstanceResponse{ID: inst.ID, ProviderKey: inst.ProviderKey, Name: inst.Name, Config: cfg, SupportedTypes: splitTypes(inst.SupportedTypes), Limits: inst.Limits, Enabled: inst.Enabled, RefundEnabled: inst.RefundEnabled, AllowUserRefund: inst.AllowUserRefund, SortOrder: inst.SortOrder, PaymentMode: inst.PaymentMode}, nil
}
