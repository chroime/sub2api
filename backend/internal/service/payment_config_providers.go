package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// validateProviderConfig runs the provider's constructor to surface config-level
// errors at save time (e.g. wxpay missing certSerial), instead of only failing
// when an order is created. Returns the structured ApplicationError from the
// constructor so the frontend i18n layer can localize it.
//
// Only validates enabled instances — a disabled instance may be a half-filled
// draft the admin will complete later.
func (s *PaymentConfigService) validateProviderConfig(providerKey string, config map[string]string) error {
	_, err := provider.CreateProvider(providerKey, "_validate_", config)
	return err
}

// --- Provider Instance CRUD ---

func (s *PaymentConfigService) ListProviderInstances(ctx context.Context) ([]*dbent.PaymentProviderInstance, error) {
	return s.entClient.PaymentProviderInstance.Query().Order(paymentproviderinstance.BySortOrder()).All(ctx)
}

// ProviderInstanceResponse is the API response for a provider instance.
type ProviderInstanceResponse struct {
	ID              int64             `json:"id"`
	ProviderKey     string            `json:"provider_key"`
	Name            string            `json:"name"`
	Config          map[string]string `json:"config"`
	SupportedTypes  []string          `json:"supported_types"`
	Limits          string            `json:"limits"`
	Enabled         bool              `json:"enabled"`
	RefundEnabled   bool              `json:"refund_enabled"`
	AllowUserRefund bool              `json:"allow_user_refund"`
	SortOrder       int               `json:"sort_order"`
	PaymentMode     string            `json:"payment_mode"`
}

// ListProviderInstancesWithConfig returns provider instances with decrypted config.
func (s *PaymentConfigService) ListProviderInstancesWithConfig(ctx context.Context) ([]ProviderInstanceResponse, error) {
	instances, err := s.entClient.PaymentProviderInstance.Query().
		Order(paymentproviderinstance.BySortOrder()).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ProviderInstanceResponse, 0, len(instances))
	for _, inst := range instances {
		resp := ProviderInstanceResponse{
			ID: int64(inst.ID), ProviderKey: inst.ProviderKey, Name: inst.Name,
			SupportedTypes: splitTypes(inst.SupportedTypes), Limits: inst.Limits,
			Enabled: inst.Enabled, RefundEnabled: inst.RefundEnabled, AllowUserRefund: inst.AllowUserRefund,
			SortOrder: inst.SortOrder, PaymentMode: inst.PaymentMode,
		}
		resp.Config, err = s.decryptAndMaskConfig(inst.ProviderKey, inst.Config)
		if err != nil {
			return nil, fmt.Errorf("decrypt config for instance %d: %w", inst.ID, err)
		}
		result = append(result, resp)
	}
	return result, nil
}

// decryptAndMaskConfig returns the stored config with sensitive fields omitted.
// Admin UIs display masked placeholders for these; the raw values never leave
// the server. Callers that need the full config (e.g. payment runtime) must
// use decryptProviderConfig directly.
func (s *PaymentConfigService) decryptAndMaskConfig(providerKey, encrypted string) (map[string]string, error) {
	cfg, err := s.decryptProviderConfig(providerKey, encrypted)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}
	masked := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if isSensitiveProviderConfigField(providerKey, k) {
			continue
		}
		masked[k] = v
	}
	if providerKey == payment.TypeAlipayBill {
		delete(masked, "epayKeyConfigured")
		if strings.TrimSpace(cfg["epayKey"]) != "" {
			masked["epayKeyConfigured"] = "true"
		}
	}
	return masked, nil
}

// pendingOrderStatuses are order statuses considered "in progress".
var pendingOrderStatuses = []string{
	payment.OrderStatusPending,
	payment.OrderStatusPaid,
	payment.OrderStatusRecharging,
}

// providerSensitiveConfigFields is the authoritative list of config keys that
// are treated as secrets per provider. Must stay in sync with the frontend
// definition at frontend/src/components/payment/providerConfig.ts
// (PROVIDER_CONFIG_FIELDS, fields with sensitive: true).
//
// Key matching is case-insensitive. Non-listed keys (e.g. appId, notifyUrl,
// stripe publishableKey) are returned in plaintext by the admin GET API.
var providerSensitiveConfigFields = map[string]map[string]struct{}{
	payment.TypeEasyPay:    {"pkey": {}},
	payment.TypeAlipay:     {"privatekey": {}, "publickey": {}, "alipaypublickey": {}},
	payment.TypeAlipayBill: {"privatekey": {}, "publickey": {}, "alipaypublickey": {}, "appauthtoken": {}, "epaykey": {}},
	payment.TypeWxpay:      {"privatekey": {}, "apiv3key": {}, "publickey": {}},
	payment.TypeStripe:     {"secretkey": {}, "webhooksecret": {}},
	payment.TypeAirwallex:  {"apikey": {}, "webhooksecret": {}},
}

// providerPendingOrderProtectedConfigFields lists config keys that cannot be
// changed while the instance has in-progress orders. This includes secrets plus
// all provider identity fields that are snapshotted into orders or used by
// webhook/refund verification.
var providerPendingOrderProtectedConfigFields = map[string]map[string]struct{}{
	payment.TypeEasyPay:    {"pkey": {}, "pid": {}},
	payment.TypeAlipay:     {"privatekey": {}, "publickey": {}, "alipaypublickey": {}, "appid": {}},
	payment.TypeAlipayBill: {"privatekey": {}, "publickey": {}, "alipaypublickey": {}, "appauthtoken": {}, "appid": {}, "receiverid": {}, "epaykey": {}},
	payment.TypeWxpay:      {"privatekey": {}, "apiv3key": {}, "publickey": {}, "appid": {}, "mpappid": {}, "mchid": {}, "publickeyid": {}, "certserial": {}},
	payment.TypeStripe:     {"secretkey": {}, "webhooksecret": {}, "currency": {}},
	payment.TypeAirwallex:  {"clientid": {}, "apikey": {}, "webhooksecret": {}, "apibase": {}, "accountid": {}, "currency": {}},
}

func isSensitiveProviderConfigField(providerKey, fieldName string) bool {
	fields, ok := providerSensitiveConfigFields[providerKey]
	if !ok {
		return false
	}
	_, found := fields[strings.ToLower(fieldName)]
	return found
}

func hasPendingOrderProtectedConfigChange(providerKey string, currentConfig, nextConfig map[string]string) bool {
	fields, ok := providerPendingOrderProtectedConfigFields[providerKey]
	if !ok {
		return false
	}
	for fieldName := range fields {
		if providerConfigFieldValue(currentConfig, fieldName) != providerConfigFieldValue(nextConfig, fieldName) {
			return true
		}
	}
	return false
}

func providerConfigFieldValue(config map[string]string, fieldName string) string {
	for key, value := range config {
		if strings.EqualFold(key, fieldName) {
			return value
		}
	}
	return ""
}

func (s *PaymentConfigService) countPendingOrders(ctx context.Context, providerInstanceID int64) (int, error) {
	return s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.ProviderInstanceIDEQ(strconv.FormatInt(providerInstanceID, 10)),
			paymentorder.StatusIn(pendingOrderStatuses...),
		).Count(ctx)
}

func (s *PaymentConfigService) countPendingOrdersByPlan(ctx context.Context, planID int64) (int, error) {
	return s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.PlanIDEQ(planID),
			paymentorder.StatusIn(pendingOrderStatuses...),
		).Count(ctx)
}

var validProviderKeys = map[string]bool{
	payment.TypeEasyPay: true, payment.TypeAlipay: true, payment.TypeAlipayBill: true, payment.TypeWxpay: true, payment.TypeStripe: true, payment.TypeAirwallex: true,
}

func (s *PaymentConfigService) CreateProviderInstance(ctx context.Context, req CreateProviderInstanceRequest) (*dbent.PaymentProviderInstance, error) {
	typesStr := joinTypes(req.SupportedTypes)
	if err := validateProviderRequest(req.ProviderKey, req.Name, typesStr); err != nil {
		return nil, err
	}
	if req.ProviderKey == payment.TypeAlipayBill {
		if typesStr == "" {
			typesStr = payment.TypeAlipay
		}
		if req.PaymentMode == "" {
			req.PaymentMode = "qrcode"
		}
		if err := validateAlipayBillProviderOptions(typesStr, req.PaymentMode, req.RefundEnabled, req.AllowUserRefund, req.Config); err != nil {
			return nil, err
		}
	}
	if req.ProviderKey == payment.TypeEasyPay {
		if err := validateEasyPayCustomMethods(req.Config, typesStr); err != nil {
			return nil, err
		}
	}
	if err := s.validateVisibleMethodEnablementConflicts(ctx, 0, req.ProviderKey, typesStr, req.Enabled); err != nil {
		return nil, err
	}
	if req.Enabled {
		if err := s.validateProviderConfig(req.ProviderKey, req.Config); err != nil {
			return nil, err
		}
	}
	enc, err := s.encryptProviderConfig(req.ProviderKey, req.Config)
	if err != nil {
		return nil, err
	}
	allowUserRefund := req.AllowUserRefund && req.RefundEnabled
	return s.entClient.PaymentProviderInstance.Create().
		SetProviderKey(req.ProviderKey).SetName(req.Name).SetConfig(enc).
		SetSupportedTypes(typesStr).SetEnabled(req.Enabled).SetPaymentMode(req.PaymentMode).
		SetSortOrder(req.SortOrder).SetLimits(req.Limits).SetRefundEnabled(req.RefundEnabled).
		SetAllowUserRefund(allowUserRefund).
		Save(ctx)
}

func validateProviderRequest(providerKey, name, supportedTypes string) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("VALIDATION_ERROR", "provider name is required")
	}
	if !validProviderKeys[providerKey] {
		return infraerrors.BadRequest("VALIDATION_ERROR", fmt.Sprintf("invalid provider key: %s", providerKey))
	}
	// supported_types can be empty (provider accepts no payment types until configured)
	return nil
}

var easyPayCustomMethodCodePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
var easyPayUpstreamMethodCodePattern = regexp.MustCompile(`^[a-z0-9_.-]+$`)

type easyPayCustomMethodConfig struct {
	Type         string `json:"type"`
	UpstreamType string `json:"upstreamType"`
	DisplayName  string `json:"displayName"`
}

func validateEasyPayCustomMethods(config map[string]string, supportedTypes string) error {
	if config == nil {
		config = map[string]string{}
	}
	raw := strings.TrimSpace(config["customMethods"])
	methods := make([]easyPayCustomMethodConfig, 0)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &methods); err != nil {
			return infraerrors.BadRequest("VALIDATION_ERROR", "customMethods must be a JSON array")
		}
	}

	customTypes := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		method.Type = strings.TrimSpace(method.Type)
		method.UpstreamType = strings.TrimSpace(method.UpstreamType)
		if method.Type == "" || method.UpstreamType == "" {
			return infraerrors.BadRequest("VALIDATION_ERROR", "customMethods upstreamType is required")
		}
		if !easyPayCustomMethodCodePattern.MatchString(method.Type) {
			return infraerrors.BadRequest("VALIDATION_ERROR", "customMethods type may only contain lowercase letters, digits, underscores, and hyphens")
		}
		if !easyPayUpstreamMethodCodePattern.MatchString(method.UpstreamType) {
			return infraerrors.BadRequest("VALIDATION_ERROR", "customMethods upstreamType may only contain lowercase letters, digits, periods, underscores, and hyphens")
		}
		if easyPayCustomMethodTypeConflictsWithBuiltin(method.Type) {
			return infraerrors.BadRequest("VALIDATION_ERROR", "customMethods type cannot start with alipay or wxpay")
		}
		if _, exists := customTypes[method.Type]; exists {
			return infraerrors.BadRequest("VALIDATION_ERROR", "duplicate customMethods type")
		}
		customTypes[method.Type] = struct{}{}
	}

	for _, supportedType := range splitTypes(supportedTypes) {
		supportedType = strings.TrimSpace(supportedType)
		if supportedType == "" || supportedType == payment.TypeAlipay || supportedType == payment.TypeWxpay {
			continue
		}
		if !easyPayCustomMethodCodePattern.MatchString(supportedType) {
			return infraerrors.BadRequest("VALIDATION_ERROR", fmt.Sprintf("supported EasyPay custom type %s may only contain lowercase letters, digits, underscores, and hyphens", supportedType))
		}
		if _, exists := customTypes[supportedType]; !exists {
			return infraerrors.BadRequest("VALIDATION_ERROR", fmt.Sprintf("supported EasyPay custom type %s has no customMethods mapping", supportedType))
		}
	}
	return nil
}

func easyPayCustomMethodTypeConflictsWithBuiltin(methodType string) bool {
	return strings.HasPrefix(methodType, payment.TypeAlipay) || strings.HasPrefix(methodType, payment.TypeWxpay)
}

// UpdateProviderInstance updates a provider instance by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update
// boilerplate and pending-order safety checks.
func (s *PaymentConfigService) UpdateProviderInstance(ctx context.Context, id int64, req UpdateProviderInstanceRequest) (*dbent.PaymentProviderInstance, error) {
	current, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load provider instance: %w", err)
	}
	if current.ProviderKey != payment.TypeAlipayBill {
		return s.updateProviderInstance(ctx, current, req)
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin provider configuration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	query := tx.PaymentProviderInstance.Query().Where(paymentproviderinstance.IDEQ(id))
	if s.entClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	current, err = query.Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock provider instance: %w", err)
	}
	transactionService := *s
	transactionService.entClient = tx.Client()
	updated, err := transactionService.updateProviderInstance(ctx, current, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit provider configuration: %w", err)
	}
	return updated.Unwrap(), nil
}

func (s *PaymentConfigService) updateProviderInstance(ctx context.Context, current *dbent.PaymentProviderInstance, req UpdateProviderInstanceRequest) (*dbent.PaymentProviderInstance, error) {
	id := current.ID
	var err error
	var pendingOrderCount *int
	getPendingOrderCount := func() (int, error) {
		if pendingOrderCount != nil {
			return *pendingOrderCount, nil
		}
		count, err := s.countPendingOrders(ctx, id)
		if err != nil {
			return 0, fmt.Errorf("check pending orders: %w", err)
		}
		pendingOrderCount = &count
		return count, nil
	}
	nextEnabled := current.Enabled
	if req.Enabled != nil {
		nextEnabled = *req.Enabled
	}
	nextSupportedTypes := current.SupportedTypes
	if req.SupportedTypes != nil {
		nextSupportedTypes = joinTypes(req.SupportedTypes)
	}
	if err := s.validateVisibleMethodEnablementConflicts(ctx, id, current.ProviderKey, nextSupportedTypes, nextEnabled); err != nil {
		return nil, err
	}
	var mergedConfig map[string]string
	if req.Config != nil {
		currentConfig, err := s.decryptProviderConfig(current.ProviderKey, current.Config)
		if err != nil {
			return nil, fmt.Errorf("decrypt existing config: %w", err)
		}
		mergedConfig, err = s.mergeConfig(ctx, id, req.Config)
		if err != nil {
			return nil, err
		}
		if current.ProviderKey == payment.TypeAlipayBill {
			if err := validateAlipayBillConfigFieldNames(mergedConfig); err != nil {
				return nil, err
			}
		}
		if hasPendingOrderProtectedConfigChange(current.ProviderKey, currentConfig, mergedConfig) {
			count, err := getPendingOrderCount()
			if err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, infraerrors.Conflict("PENDING_ORDERS", "instance has pending orders").
					WithMetadata(map[string]string{"count": strconv.Itoa(count)})
			}
			if current.ProviderKey == payment.TypeAlipayBill {
				if err := s.ensureBuiltinEasyPayCredentialsIdle(ctx, id); err != nil {
					return nil, err
				}
			}
		}
		if current.ProviderKey == payment.TypeAlipayBill && hasAlipayBillIdentityChange(currentConfig, mergedConfig) {
			if err := s.ensureAlipayBillHistoryUnchanged(ctx, id, providerConfigFieldValue(currentConfig, "receiverId")); err != nil {
				return nil, err
			}
		}
	}
	if req.Enabled != nil && !*req.Enabled {
		count, err := getPendingOrderCount()
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, infraerrors.Conflict("PENDING_ORDERS", "instance has pending orders").
				WithMetadata(map[string]string{"count": strconv.Itoa(count)})
		}
	}
	configToValidate := mergedConfig
	if configToValidate == nil {
		configToValidate, err = s.decryptProviderConfig(current.ProviderKey, current.Config)
		if err != nil {
			return nil, fmt.Errorf("decrypt existing config: %w", err)
		}
	}
	if current.ProviderKey == payment.TypeEasyPay {
		if err := validateEasyPayCustomMethods(configToValidate, nextSupportedTypes); err != nil {
			return nil, err
		}
	}
	if current.ProviderKey == payment.TypeAlipayBill {
		nextMode, nextRefund, nextUserRefund := current.PaymentMode, current.RefundEnabled, current.AllowUserRefund
		if req.PaymentMode != nil {
			nextMode = *req.PaymentMode
		}
		if req.RefundEnabled != nil {
			nextRefund = *req.RefundEnabled
		}
		if req.AllowUserRefund != nil {
			nextUserRefund = *req.AllowUserRefund
		}
		if err := validateAlipayBillProviderOptions(nextSupportedTypes, nextMode, nextRefund, nextUserRefund, configToValidate); err != nil {
			return nil, err
		}
		// Every save of this provider must preserve encryption, including a
		// metadata-only update of an older plaintext draft.
		mergedConfig = configToValidate
	}
	// Validate merged config when the instance will end up enabled.
	// This surfaces provider-level errors (e.g. wxpay missing certSerial) at save time,
	// so admins see them in the dialog instead of only when an order is created.
	finalEnabled := current.Enabled
	if req.Enabled != nil {
		finalEnabled = *req.Enabled
	}
	if finalEnabled {
		if err := s.validateProviderConfig(current.ProviderKey, configToValidate); err != nil {
			return nil, err
		}
	}
	u := s.entClient.PaymentProviderInstance.UpdateOneID(id)
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if mergedConfig != nil || current.ProviderKey == payment.TypeAlipayBill {
		enc, err := s.encryptProviderConfig(current.ProviderKey, mergedConfig)
		if err != nil {
			return nil, err
		}
		u.SetConfig(enc)
	}
	if req.SupportedTypes != nil {
		// Check pending orders before removing payment types
		count, err := getPendingOrderCount()
		if err != nil {
			return nil, err
		}
		if count > 0 {
			// Load current instance to compare types
			oldTypes := strings.Split(current.SupportedTypes, ",")
			newTypes := req.SupportedTypes
			for _, ot := range oldTypes {
				ot = strings.TrimSpace(ot)
				if ot == "" {
					continue
				}
				found := false
				for _, nt := range newTypes {
					if strings.TrimSpace(nt) == ot {
						found = true
						break
					}
				}
				if !found {
					return nil, infraerrors.Conflict("PENDING_ORDERS", "cannot remove payment types while instance has pending orders").
						WithMetadata(map[string]string{"count": strconv.Itoa(count)})
				}
			}
		}
		u.SetSupportedTypes(joinTypes(req.SupportedTypes))
	}
	if req.Enabled != nil {
		u.SetEnabled(*req.Enabled)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	if req.Limits != nil {
		u.SetLimits(*req.Limits)
	}
	if req.RefundEnabled != nil {
		u.SetRefundEnabled(*req.RefundEnabled)
		// Cascade: turning off refund_enabled also disables allow_user_refund
		if !*req.RefundEnabled {
			u.SetAllowUserRefund(false)
		}
	}
	if req.AllowUserRefund != nil {
		// Only allow enabling when refund_enabled is (or will be) true
		if *req.AllowUserRefund {
			refundEnabled := false
			if req.RefundEnabled != nil {
				refundEnabled = *req.RefundEnabled
			} else {
				refundEnabled = current.RefundEnabled
			}
			if refundEnabled {
				u.SetAllowUserRefund(true)
			}
		} else {
			u.SetAllowUserRefund(false)
		}
	}
	if req.PaymentMode != nil {
		u.SetPaymentMode(*req.PaymentMode)
	}
	return u.Save(ctx)
}

// GetUserRefundEligibleInstanceIDs returns provider instance IDs that allow user refund.
func (s *PaymentConfigService) GetUserRefundEligibleInstanceIDs(ctx context.Context) ([]string, error) {
	instances, err := s.entClient.PaymentProviderInstance.Query().
		Where(
			paymentproviderinstance.ProviderKeyNEQ(payment.TypeAlipayBill),
			paymentproviderinstance.RefundEnabledEQ(true),
			paymentproviderinstance.AllowUserRefundEQ(true),
		).Select(paymentproviderinstance.FieldID).All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(instances))
	for _, inst := range instances {
		ids = append(ids, strconv.FormatInt(int64(inst.ID), 10))
	}
	return ids, nil
}

func (s *PaymentConfigService) mergeConfig(ctx context.Context, id int64, newConfig map[string]string) (map[string]string, error) {
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load existing provider: %w", err)
	}
	existing, err := s.decryptProviderConfig(inst.ProviderKey, inst.Config)
	if err != nil {
		return nil, fmt.Errorf("decrypt existing config for instance %d: %w", id, err)
	}
	if existing == nil {
		existing = map[string]string{}
	}
	for k, v := range newConfig {
		// Preserve existing secrets when the client submits an empty value
		// (admin UI omits the value to indicate "leave unchanged").
		if v == "" && isSensitiveProviderConfigField(inst.ProviderKey, k) {
			continue
		}
		existing[k] = v
	}
	return existing, nil
}

// decryptConfig parses a stored provider config.
// Existing providers support plaintext JSON and legacy AES-256-GCM ciphertext
// ("iv:authTag:ciphertext"). Fixed QR credentials also use AES-GCM storage.
// The generic legacy reader treats unreadable records as empty for re-entry;
// fixed QR admin reads and writes use decryptProviderConfig to fail explicitly.
func (s *PaymentConfigService) decryptConfig(stored string) (map[string]string, error) {
	if stored == "" {
		return nil, nil
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(stored), &cfg); err == nil {
		return cfg, nil
	}
	// AES-GCM remains required for fixed QR configurations and legacy records.
	if len(s.encryptionKey) == payment.AES256KeySize {
		//nolint:staticcheck // SA1019: fixed QR and legacy encrypted configuration support.
		if plaintext, err := payment.Decrypt(stored, s.encryptionKey); err == nil {
			if err := json.Unmarshal([]byte(plaintext), &cfg); err == nil {
				return cfg, nil
			}
		}
	}
	slog.Warn("payment provider config unreadable, treating as empty for re-entry",
		"stored_len", len(stored))
	return nil, nil
}

func (s *PaymentConfigService) DeleteProviderInstance(ctx context.Context, id int64) error {
	instance, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
	if err != nil {
		return err
	}
	if instance.ProviderKey != payment.TypeAlipayBill {
		return s.deleteProviderInstance(ctx, instance)
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin provider deletion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	query := tx.PaymentProviderInstance.Query().Where(paymentproviderinstance.IDEQ(id))
	if s.entClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	instance, err = query.Only(ctx)
	if err != nil {
		return fmt.Errorf("lock provider instance: %w", err)
	}
	transactionService := *s
	transactionService.entClient = tx.Client()
	if err := transactionService.deleteProviderInstance(ctx, instance); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit provider deletion: %w", err)
	}
	return nil
}

func (s *PaymentConfigService) deleteProviderInstance(ctx context.Context, instance *dbent.PaymentProviderInstance) error {
	id := instance.ID
	count, err := s.countPendingOrders(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this instance has %d in-progress orders and cannot be deleted — wait for orders to complete or disable the instance first", count))
	}
	if instance.ProviderKey == payment.TypeAlipayBill {
		cfg, err := s.decryptProviderConfig(instance.ProviderKey, instance.Config)
		if err != nil {
			return err
		}
		if err := s.ensureAlipayBillHistoryUnchanged(ctx, id, providerConfigFieldValue(cfg, "receiverId")); err != nil {
			return err
		}
	}
	return s.entClient.PaymentProviderInstance.DeleteOneID(id).Exec(ctx)
}

func validateAlipayBillProviderOptions(supportedTypes, paymentMode string, refundEnabled, allowUserRefund bool, cfg map[string]string) error {
	if err := validateAlipayBillConfigFieldNames(cfg); err != nil {
		return err
	}
	if supportedTypes != payment.TypeAlipay {
		return infraerrors.BadRequest("VALIDATION_ERROR", "Alipay fixed QR supports only the alipay payment type")
	}
	if paymentMode != "qrcode" {
		return infraerrors.BadRequest("VALIDATION_ERROR", "Alipay fixed QR requires qrcode payment mode")
	}
	if refundEnabled || allowUserRefund {
		return infraerrors.BadRequest("VALIDATION_ERROR", "Alipay fixed QR does not support automatic or user refunds")
	}
	if err := ValidateBuiltinEasyPayConfig(cfg); err != nil {
		return err
	}
	_, err := provider.ParseAlipayBillRules(cfg)
	return err
}

func validateAlipayBillConfigFieldNames(cfg map[string]string) error {
	seen := make(map[string]struct{}, len(cfg))
	for key := range cfg {
		normalized := strings.ToLower(key)
		if _, exists := seen[normalized]; exists {
			return infraerrors.BadRequest("VALIDATION_ERROR", "Alipay fixed QR configuration contains duplicate field names")
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func hasAlipayBillIdentityChange(current, next map[string]string) bool {
	return providerConfigFieldValue(current, "appId") != providerConfigFieldValue(next, "appId") ||
		providerConfigFieldValue(current, "receiverId") != providerConfigFieldValue(next, "receiverId")
}

func (s *PaymentConfigService) ensureAlipayBillHistoryUnchanged(ctx context.Context, instanceID int64, receiverID string) error {
	hasOrders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.ProviderInstanceIDEQ(strconv.FormatInt(instanceID, 10))).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check payment reconciliation orders: %w", err)
	}
	hasHistory := hasOrders
	if !hasHistory {
		hasHistory, err = s.builtinEasyPayHasOrders(ctx, instanceID, false)
		if err != nil {
			return err
		}
	}
	if !hasHistory {
		rows, err := s.entClient.QueryContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM payment_bill_receipts WHERE provider_instance_id = $1 OR receiver_id = $2
		)`, instanceID, strings.TrimSpace(receiverID))
		if err != nil {
			return fmt.Errorf("check payment reconciliation receipts: %w", err)
		}
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&hasHistory); err != nil {
				return fmt.Errorf("scan payment reconciliation history: %w", err)
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read payment reconciliation history: %w", err)
		}
	}
	if hasHistory {
		return infraerrors.Conflict("PAYMENT_RECONCILIATION_HISTORY", "this provider or receiving account has payment history; keep its identity and create a new instance for a different account")
	}
	return nil
}

func (s *PaymentConfigService) encryptProviderConfig(providerKey string, cfg map[string]string) (string, error) {
	if providerKey == payment.TypeAlipayBill {
		// This marker is derived on reads; clients cannot persist or override it.
		stored := make(map[string]string, len(cfg))
		for key, value := range cfg {
			if key != "epayKeyConfigured" {
				stored[key] = value
			}
		}
		cfg = stored
	}
	plaintext, err := s.encryptConfig(cfg)
	if err != nil || providerKey != payment.TypeAlipayBill {
		return plaintext, err
	}
	if len(s.encryptionKey) != payment.AES256KeySize {
		return "", infraerrors.ServiceUnavailable("PAYMENT_CONFIG_ENCRYPTION_NOT_CONFIGURED", "Alipay fixed QR requires a configured 32-byte payment encryption key")
	}
	//nolint:staticcheck // SA1019: fixed QR credentials intentionally use AES-GCM storage.
	return payment.Encrypt(plaintext, s.encryptionKey)
}

func (s *PaymentConfigService) decryptProviderConfig(providerKey, stored string) (map[string]string, error) {
	if providerKey != payment.TypeAlipayBill || stored == "" {
		return s.decryptConfig(stored)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(stored), &cfg); err == nil {
		return cfg, nil
	}
	if len(s.encryptionKey) != payment.AES256KeySize {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_CONFIG_ENCRYPTION_NOT_CONFIGURED", "Alipay fixed QR requires a configured 32-byte payment encryption key")
	}
	//nolint:staticcheck // SA1019: fixed QR credentials intentionally use AES-GCM storage.
	plaintext, err := payment.Decrypt(stored, s.encryptionKey)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_CONFIG_DECRYPTION_FAILED", "Alipay fixed QR configuration could not be decrypted with the configured encryption key")
	}
	if err := json.Unmarshal([]byte(plaintext), &cfg); err != nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_CONFIG_DECRYPTION_FAILED", "Alipay fixed QR configuration is unreadable")
	}
	return cfg, nil
}

// encryptConfig serialises a provider config for storage.
// Existing providers keep plaintext storage compatibility. New fixed QR saves
// call encryptProviderConfig to wrap the serialized configuration in AES-GCM.
func (s *PaymentConfigService) encryptConfig(cfg map[string]string) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(data), nil
}
