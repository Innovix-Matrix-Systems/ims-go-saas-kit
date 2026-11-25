package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

const bkashTokenCacheKey = "billing:bkash:token"

// BkashDriver implements the bKash tokenized checkout API.
type BkashDriver struct {
	cfg    config.Bkash
	store  cache.Store
	client *http.Client
	// tokenMu makes concurrent requests share one token grant instead of
	// each asking bKash for a new token (M18).
	tokenMu sync.Mutex
}

// NewBkashDriver creates the bKash driver (token cached 55 minutes).
func NewBkashDriver(cfg config.Bkash, store cache.Store) *BkashDriver {
	return &BkashDriver{cfg: cfg, store: store, client: &http.Client{Timeout: 20 * time.Second, Transport: tracing.Transport(nil)}}
}

// Initiate implements domain.PaymentDriver.
func (d *BkashDriver) Initiate(ctx context.Context, payment *domain.Payment, data map[string]any) (domain.InitiateResult, error) {
	token, err := d.token(ctx)
	if err != nil {
		return domain.InitiateResult{}, err
	}

	payload := map[string]any{
		"mode":                  "0011",
		"payerReference":        fmt.Sprintf("%d", payment.TenantID),
		"amount":                fmt.Sprintf("%.2f", float64(payment.Amount)/100),
		"currency":              payment.Currency,
		"intent":                "sale",
		"merchantInvoiceNumber": payment.InvoiceNumber,
	}
	if d.cfg.CallbackURL != "" {
		payload["callbackURL"] = d.cfg.CallbackURL
	}

	response, err := d.post(ctx, "/tokenized/checkout/create", token, payload, nil)
	if err != nil {
		return domain.InitiateResult{}, err
	}
	return domain.InitiateResult{
		Success:     strValue(response["statusCode"]) == "0000",
		Reference:   strPtr(strValue(response["paymentID"])),
		RedirectURL: strPtr(strValue(response["bkashURL"])),
		Message:     strPtr(strValue(response["statusMessage"])),
		RawResponse: response,
	}, nil
}

// Verify implements domain.PaymentDriver.
func (d *BkashDriver) Verify(ctx context.Context, payment *domain.Payment) (domain.VerificationResult, error) {
	token, err := d.token(ctx)
	if err != nil {
		return domain.VerificationResult{}, err
	}
	reference := ""
	if payment.GatewayRef != nil {
		reference = *payment.GatewayRef
	}
	if reference == "" {
		return domain.VerificationResult{}, apperr.New(apperr.InvalidInput, "The payment has no gateway reference.")
	}

	response, err := d.post(ctx, "/tokenized/checkout/payment/status", token, map[string]any{"paymentID": reference}, nil)
	if err != nil {
		return domain.VerificationResult{}, err
	}
	status := strValue(response["transactionStatus"])
	return domain.VerificationResult{
		IsCompleted:   status == "Completed",
		IsFailed:      status == "Failed" || status == "Cancelled",
		GatewayStatus: strPtr(status),
		TransactionID: strPtr(strValue(response["trxID"])),
		RawResponse:   response,
	}, nil
}

// HandleCallback implements domain.PaymentDriver. The callback is only a
// hint: the outcome always comes from bKash itself (H8). A "success" callback
// executes the payment; anything else asks bKash for the payment status, and
// only a status bKash reports as Failed/Cancelled fails the payment.
func (d *BkashDriver) HandleCallback(ctx context.Context, payload map[string]any) (domain.CallbackResult, error) {
	paymentID := strValue(payload["paymentID"])
	if paymentID == "" {
		return domain.CallbackResult{}, apperr.New(apperr.InvalidInput, "The callback is missing its payment reference.")
	}
	token, err := d.token(ctx)
	if err != nil {
		return domain.CallbackResult{}, err
	}

	path := "/tokenized/checkout/payment/status"
	if strings.EqualFold(strValue(payload["status"]), "success") {
		path = "/tokenized/checkout/execute"
	}
	response, err := d.post(ctx, path, token, map[string]any{"paymentID": paymentID}, nil)
	if err != nil {
		return domain.CallbackResult{}, err
	}
	transactionStatus := strValue(response["transactionStatus"])
	return domain.CallbackResult{
		IsSuccessful:  transactionStatus == "Completed",
		IsFailed:      transactionStatus == "Failed" || transactionStatus == "Cancelled",
		PaymentRef:    strPtr(paymentID),
		GatewayRef:    strPtr(strValue(response["trxID"])),
		GatewayStatus: strPtr(transactionStatus),
		RawResponse:   response,
	}, nil
}

// Refund implements domain.PaymentDriver.
func (d *BkashDriver) Refund(ctx context.Context, payment *domain.Payment, amount int64) (bool, error) {
	token, err := d.token(ctx)
	if err != nil {
		return false, err
	}
	reference := ""
	if payment.GatewayRef != nil {
		reference = *payment.GatewayRef
	}
	response, err := d.post(ctx, "/tokenized/checkout/payment/refund", token, map[string]any{
		"paymentID": reference,
		"amount":    fmt.Sprintf("%.2f", float64(amount)/100),
		"reason":    "refund",
	}, nil)
	if err != nil {
		return false, err
	}
	return strValue(response["statusCode"]) == "0000", nil
}

func (d *BkashDriver) token(ctx context.Context) (string, error) {
	var cached string
	if found, err := d.store.Get(ctx, bkashTokenCacheKey, &cached); err == nil && found && cached != "" {
		return cached, nil
	}
	d.tokenMu.Lock()
	defer d.tokenMu.Unlock()
	// Another request may have refreshed the token while we waited.
	if found, err := d.store.Get(ctx, bkashTokenCacheKey, &cached); err == nil && found && cached != "" {
		return cached, nil
	}
	if !d.cfg.Configured() {
		return "", apperr.New(apperr.ServiceUnavailable, "bKash is not configured.")
	}

	response, err := d.post(ctx, "/tokenized/checkout/token/grant", "", map[string]any{
		"app_key":    d.cfg.AppKey,
		"app_secret": d.cfg.AppSecret,
	}, map[string]string{
		"username": d.cfg.Username,
		"password": d.cfg.Password,
	})
	if err != nil {
		return "", err
	}
	token := strValue(response["id_token"])
	if token == "" {
		return "", apperr.New(apperr.ServiceUnavailable, "bKash token grant failed.")
	}
	_ = d.store.Set(ctx, bkashTokenCacheKey, token, 55*time.Minute)
	return token, nil
}

func (d *BkashDriver) post(ctx context.Context, path, token string, body any, headers map[string]string) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "encode bKash request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(d.cfg.BaseURL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "create bKash request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if d.cfg.AppKey != "" {
		req.Header.Set("X-App-Key", d.cfg.AppKey)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "bKash request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, apperr.Wrap(apperr.ServiceUnavailable, "read bKash response", err)
	}
	// A non-2xx answer is never a business decision about the payment (H10):
	// the caller leaves the payment untouched and can retry. A rejected token
	// is dropped so the next call grants a fresh one (M18).
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			_ = d.store.Delete(ctx, bkashTokenCacheKey)
		}
		return nil, apperr.Newf(apperr.ServiceUnavailable, "bKash is unavailable (HTTP %d). Please retry.", resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, apperr.Newf(apperr.ServiceUnavailable, "bKash returned an invalid response (HTTP %d).", resp.StatusCode)
	}
	return decoded, nil
}

// CashDriver records manual cash payments awaiting staff approval.
type CashDriver struct{}

// Initiate implements domain.PaymentDriver.
func (CashDriver) Initiate(ctx context.Context, payment *domain.Payment, data map[string]any) (domain.InitiateResult, error) {
	reference, err := domain.ManualReference("CASH")
	if err != nil {
		return domain.InitiateResult{}, err
	}
	message := "Cash payment recorded and is awaiting approval."
	return domain.InitiateResult{
		Success:   true,
		Reference: &reference,
		Message:   &message,
		RawResponse: map[string]any{
			"notes":       data["notes"],
			"recorded_by": data["recorded_by"],
		},
	}, nil
}

// Verify implements domain.PaymentDriver.
func (CashDriver) Verify(ctx context.Context, payment *domain.Payment) (domain.VerificationResult, error) {
	status := "manual"
	return domain.VerificationResult{
		IsCompleted:   payment.Status == domain.PaymentCompleted,
		GatewayStatus: &status,
	}, nil
}

// HandleCallback implements domain.PaymentDriver. Manual methods are settled
// by staff approval, never by a callback (H9).
func (CashDriver) HandleCallback(ctx context.Context, payload map[string]any) (domain.CallbackResult, error) {
	return domain.CallbackResult{}, errManualCallback()
}

// Refund implements domain.PaymentDriver.
func (CashDriver) Refund(ctx context.Context, payment *domain.Payment, amount int64) (bool, error) {
	return true, nil
}

// BankTransferDriver records manual bank transfers awaiting staff approval.
type BankTransferDriver struct{}

// Initiate implements domain.PaymentDriver.
func (BankTransferDriver) Initiate(ctx context.Context, payment *domain.Payment, data map[string]any) (domain.InitiateResult, error) {
	// The payer's bank reference is free text (and may repeat across
	// tenants), so it is kept in the response payload; gateway_ref is our own
	// unique, unguessable reference.
	reference, err := domain.ManualReference("BANK")
	if err != nil {
		return domain.InitiateResult{}, err
	}
	message := "Bank transfer recorded and is awaiting approval."
	return domain.InitiateResult{
		Success:   true,
		Reference: &reference,
		Message:   &message,
		RawResponse: map[string]any{
			"reference_number": data["reference_number"],
			"notes":            data["notes"],
			"recorded_by":      data["recorded_by"],
		},
	}, nil
}

// Verify implements domain.PaymentDriver.
func (BankTransferDriver) Verify(ctx context.Context, payment *domain.Payment) (domain.VerificationResult, error) {
	status := "manual"
	return domain.VerificationResult{
		IsCompleted:   payment.Status == domain.PaymentCompleted,
		GatewayStatus: &status,
	}, nil
}

// HandleCallback implements domain.PaymentDriver (see CashDriver).
func (BankTransferDriver) HandleCallback(ctx context.Context, payload map[string]any) (domain.CallbackResult, error) {
	return domain.CallbackResult{}, errManualCallback()
}

func errManualCallback() error {
	return apperr.New(apperr.InvalidInput, "Manual payment methods do not accept callbacks.")
}

// Refund implements domain.PaymentDriver.
func (BankTransferDriver) Refund(ctx context.Context, payment *domain.Payment, amount int64) (bool, error) {
	return true, nil
}

// NagadDriver is a placeholder until the Nagad integration is implemented.
type NagadDriver struct{}

// Initiate implements domain.PaymentDriver.
func (NagadDriver) Initiate(ctx context.Context, payment *domain.Payment, data map[string]any) (domain.InitiateResult, error) {
	return domain.InitiateResult{}, nagadNotImplemented()
}

// Verify implements domain.PaymentDriver.
func (NagadDriver) Verify(ctx context.Context, payment *domain.Payment) (domain.VerificationResult, error) {
	return domain.VerificationResult{}, nagadNotImplemented()
}

// HandleCallback implements domain.PaymentDriver.
func (NagadDriver) HandleCallback(ctx context.Context, payload map[string]any) (domain.CallbackResult, error) {
	return domain.CallbackResult{}, nagadNotImplemented()
}

// Refund implements domain.PaymentDriver.
func (NagadDriver) Refund(ctx context.Context, payment *domain.Payment, amount int64) (bool, error) {
	return false, nagadNotImplemented()
}

func nagadNotImplemented() error {
	return apperr.New(apperr.ServiceUnavailable, "The Nagad payment driver is not implemented yet.")
}

func strValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
