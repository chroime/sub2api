package service

import "context"

// AlipayBillEditCredentials is returned only by the authenticated administrator
// editor endpoint. Provider listings keep their existing secret filtering.
type AlipayBillEditCredentials struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

func (s *PaymentService) GetAlipayBillEditCredentials(ctx context.Context, id int64) (*AlipayBillEditCredentials, error) {
	_, config, err := s.loadBillProviderConfig(ctx, id, false)
	if err != nil {
		return nil, err
	}
	return &AlipayBillEditCredentials{
		PrivateKey: config["privateKey"],
		PublicKey:  config["publicKey"],
	}, nil
}
