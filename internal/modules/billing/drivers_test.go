package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache/memory"
)

func testBkashConfig() config.Bkash {
	return config.Bkash{
		BaseURL:   "https://bkash.invalid",
		AppKey:    "key",
		AppSecret: "secret",
		Username:  "user",
		Password:  "pass",
	}
}

func TestBkashDriverAgainstStub(t *testing.T) {
	calls := map[string]int{}
	gatewayStatus := "Completed" // what bKash reports for PAY-123
	httpStatus := http.StatusOK  // what the status endpoint answers with
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tokenized/checkout/token/grant":
			_ = json.NewEncoder(w).Encode(map[string]any{"id_token": "token-1", "statusCode": "0000"})
		case "/tokenized/checkout/create":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode":    "0000",
				"statusMessage": "Successful",
				"paymentID":     "PAY-123",
				"bkashURL":      "https://bkash.test/redirect",
			})
		case "/tokenized/checkout/execute":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode":        "0000",
				"transactionStatus": "Completed",
				"trxID":             "TRX-9",
			})
		case "/tokenized/checkout/payment/status":
			w.WriteHeader(httpStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"transactionStatus": gatewayStatus,
				"trxID":             "TRX-9",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testBkashConfig()
	cfg.BaseURL = server.URL
	store := memory.New("bkash-test-")
	defer store.Close()
	driver := NewBkashDriver(cfg, store)
	ctx := context.Background()

	payment := &domain.Payment{
		ID: 1, TenantID: 5, Amount: 125050, Currency: "BDT", InvoiceNumber: "INV-1",
		Status: domain.PaymentPending, PaymentMethod: domain.MethodBkash,
	}

	result, err := driver.Initiate(ctx, payment, nil)
	if err != nil {
		t.Fatalf("Initiate() error = %v", err)
	}
	if !result.Success || result.Reference == nil || *result.Reference != "PAY-123" {
		t.Fatalf("initiate result = %+v", result)
	}
	if result.RedirectURL == nil || *result.RedirectURL != "https://bkash.test/redirect" {
		t.Fatalf("redirect = %v", result.RedirectURL)
	}

	// Second initiate reuses the cached token (grant called once).
	if _, err := driver.Initiate(ctx, payment, nil); err != nil {
		t.Fatalf("second Initiate() error = %v", err)
	}
	if calls["/tokenized/checkout/token/grant"] != 1 {
		t.Fatalf("token grant calls = %d, want 1 (cached)", calls["/tokenized/checkout/token/grant"])
	}

	payment.GatewayRef = result.Reference
	verification, err := driver.Verify(ctx, payment)
	if err != nil || !verification.IsCompleted {
		t.Fatalf("Verify() = %+v, %v", verification, err)
	}

	callback, err := driver.HandleCallback(ctx, map[string]any{"status": "success", "paymentID": "PAY-123"})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !callback.IsSuccessful || callback.GatewayRef == nil || *callback.GatewayRef != "TRX-9" {
		t.Fatalf("callback = %+v", callback)
	}

	// A forged "failure" callback cannot fail a payment bKash reports as
	// completed: the outcome always comes from the gateway (H8).
	callback, err = driver.HandleCallback(ctx, map[string]any{"status": "failure", "paymentID": "PAY-123"})
	if err != nil || !callback.IsSuccessful || callback.IsFailed {
		t.Fatalf("forged failure callback = %+v, %v", callback, err)
	}

	// Still in progress at bKash: neither successful nor failed (stays pending).
	gatewayStatus = "Initiated"
	callback, err = driver.HandleCallback(ctx, map[string]any{"status": "cancel", "paymentID": "PAY-123"})
	if err != nil || callback.IsSuccessful || callback.IsFailed {
		t.Fatalf("in-progress callback = %+v, %v", callback, err)
	}

	// bKash confirms the failure: only now is the payment failed.
	gatewayStatus = "Cancelled"
	callback, err = driver.HandleCallback(ctx, map[string]any{"status": "cancel", "paymentID": "PAY-123"})
	if err != nil || !callback.IsFailed {
		t.Fatalf("confirmed failure callback = %+v, %v", callback, err)
	}

	// A bKash 5xx is an error (retry), never a failed payment (H10).
	httpStatus = http.StatusBadGateway
	if _, err := driver.HandleCallback(ctx, map[string]any{"status": "cancel", "paymentID": "PAY-123"}); err == nil {
		t.Fatal("5xx status answer: error = nil, want a retryable error")
	}

	// A 401 drops the cached token so the next call grants a new one (M18).
	httpStatus = http.StatusUnauthorized
	_, _ = driver.HandleCallback(ctx, map[string]any{"status": "cancel", "paymentID": "PAY-123"})
	httpStatus = http.StatusOK
	grantsBefore := calls["/tokenized/checkout/token/grant"]
	if _, err := driver.HandleCallback(ctx, map[string]any{"status": "cancel", "paymentID": "PAY-123"}); err != nil {
		t.Fatalf("callback after token reset: %v", err)
	}
	if calls["/tokenized/checkout/token/grant"] != grantsBefore+1 {
		t.Fatalf("token grants = %d, want %d (token re-granted after 401)", calls["/tokenized/checkout/token/grant"], grantsBefore+1)
	}
}

func TestManualDriversRejectCallbacksAndUseRandomReferences(t *testing.T) {
	ctx := context.Background()
	payment := &domain.Payment{ID: 1, Amount: 100, Currency: "BDT"}
	seen := map[string]bool{}
	for _, driver := range []domain.PaymentDriver{CashDriver{}, BankTransferDriver{}} {
		if _, err := driver.HandleCallback(ctx, map[string]any{"paymentID": "x"}); err == nil {
			t.Errorf("%T accepted a callback", driver)
		}
		for range 3 {
			result, err := driver.Initiate(ctx, payment, map[string]any{"reference_number": "SAME-REF"})
			if err != nil || result.Reference == nil {
				t.Fatalf("%T Initiate() = %+v, %v", driver, result, err)
			}
			if seen[*result.Reference] || len(*result.Reference) < 20 {
				t.Fatalf("%T reference %q is reused or too short", driver, *result.Reference)
			}
			seen[*result.Reference] = true
		}
	}
}

func TestBkashDriverWithoutConfig(t *testing.T) {
	store := memory.New("bkash-unconfigured-")
	defer store.Close()
	driver := NewBkashDriver(config.Bkash{}, store)

	_, err := driver.Initiate(context.Background(), &domain.Payment{Amount: 100, Currency: "BDT"}, nil)
	if err == nil {
		t.Fatal("Initiate() without config = nil error")
	}
}
