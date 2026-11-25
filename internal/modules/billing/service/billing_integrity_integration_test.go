package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
	service "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/service"
	tenantsrepo "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/tenants"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

// bkashStub is a controllable bKash sandbox: it hands out payment ids and
// answers status/execute with whatever status the test sets.
type bkashStub struct {
	mu         sync.Mutex
	next       int
	status     string // transactionStatus reported for every payment
	httpStatus int
}

func (s *bkashStub) set(status string, httpStatus int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.httpStatus = status, httpStatus
}

func (s *bkashStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/tokenized/checkout/token/grant":
		_ = json.NewEncoder(w).Encode(map[string]any{"id_token": "tok"})
	case "/tokenized/checkout/create":
		s.next++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": "0000", "paymentID": fmt.Sprintf("PAY-%d", s.next), "bkashURL": "https://pay.test",
		})
	case "/tokenized/checkout/execute", "/tokenized/checkout/payment/status":
		w.WriteHeader(s.httpStatus)
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": "0000", "transactionStatus": s.status, "trxID": "TRX-1"})
	default:
		http.NotFound(w, r)
	}
}

// TestBillingIntegrity covers gateway-confirmed callbacks,
// manual-method callback rejection, idempotent subscribe, atomic
// completion + activation, terminal cancelled subscriptions and storage.mb.
func TestBillingIntegrity(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool,
		"payment_approval_requests", "payments", "subscriptions",
		"package_features", "packages",
		"personal_access_tokens", "role_has_permissions", "model_has_roles",
		"users", "roles", "permissions", "branches", "tenants",
	)
	ctx := context.Background()
	tenant, err := tenantsrepo.NewRepository(queries).Create(ctx, tenantsrepo.CreateInput{
		Name: "Integrity Tenant", Code: "integrity", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	stub := &bkashStub{status: "Initiated", httpStatus: http.StatusOK}
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	cfg := serviceTestBkashConfig()
	cfg.BaseURL = server.URL
	store := memory.New("integrity-")
	t.Cleanup(store.Close)

	packagesRepo := domain.NewPackageRepository(queries)
	subsRepo := domain.NewSubscriptionRepository(queries)
	paymentsRepo := domain.NewPaymentRepository(queries)
	approvalsRepo := domain.NewApprovalRepository(queries)
	tx := database.NewTxManager(&database.Conn{Name: "primary", Pool: pool})
	orchestrator := billing.NewOrchestrator(paymentsRepo, "BDT").
		Register(domain.MethodCash, billing.CashDriver{}).
		Register(domain.MethodBkash, billing.NewBkashDriver(cfg, store))
	packages := service.NewPackageService(packagesRepo).WithTx(tx)
	subscriptions := service.NewSubscriptionService(subsRepo, packagesRepo, paymentsRepo, approvalsRepo, orchestrator).WithTx(tx)
	approvals := service.NewApprovalService(approvalsRepo, paymentsRepo, subsRepo, tx)
	guard := service.NewGuard(subsRepo, packagesRepo)

	pkg, err := packages.Create(ctx, domain.PackageInput{
		Name: "Integrity", Price: 1000, BillingCycle: domain.CycleMonthly, IsActive: true,
	}, []domain.FeatureInput{
		{FeatureKey: string(domain.FeatureStorageMB), IsEnabled: true, LimitValue: 1},
	})
	if err != nil {
		t.Fatalf("create package: %v", err)
	}

	t.Run("subscribe is idempotent while a checkout is pending", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make([]int64, 4)
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, payment, err := subscriptions.Subscribe(ctx, tenant.ID, pkg.ID, domain.MethodCash, map[string]any{"notes": "cash"}, 1)
				if err != nil {
					t.Errorf("Subscribe() error = %v", err)
					return
				}
				ids[i] = payment.ID
			}()
		}
		wg.Wait()
		for _, id := range ids[1:] {
			if id != ids[0] {
				t.Fatalf("payment ids = %v, want one checkout", ids)
			}
		}
		if _, total, err := approvals.List(ctx, domain.ApprovalPending, 10, 0); err != nil || total != 1 {
			t.Fatalf("pending approvals = %d, %v; want 1", total, err)
		}
	})

	t.Run("manual methods reject callbacks", func(t *testing.T) {
		_, payment, err := subscriptions.Subscribe(ctx, tenant.ID, pkg.ID, domain.MethodCash, nil, 1)
		if err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}
		if _, err := subscriptions.HandleGatewayCallback(ctx, payment.ID, domain.MethodCash, map[string]any{"paymentID": *payment.GatewayRef}); err == nil {
			t.Fatal("cash callback accepted")
		}
		after, _ := paymentsRepo.Get(ctx, payment.ID)
		if after.Status != domain.PaymentPending {
			t.Fatalf("cash payment status = %q, want pending", after.Status)
		}
	})

	t.Run("bKash callbacks follow the gateway, not the callback body", func(t *testing.T) {
		subscription, payment, err := subscriptions.Subscribe(ctx, tenant.ID, pkg.ID, domain.MethodBkash, nil, 1)
		if err != nil {
			t.Fatalf("Subscribe(bkash) error = %v", err)
		}
		forged := map[string]any{"status": "failure", "paymentID": *payment.GatewayRef}

		stub.set("Initiated", http.StatusOK)
		got, err := subscriptions.HandleGatewayCallback(ctx, payment.ID, domain.MethodBkash, forged)
		if err != nil || got.Status != domain.PaymentPending {
			t.Fatalf("in-progress callback = %+v, %v; want pending", got, err)
		}

		stub.set("Completed", http.StatusBadGateway)
		if _, err := subscriptions.HandleGatewayCallback(ctx, payment.ID, domain.MethodBkash, forged); err == nil {
			t.Fatal("gateway 5xx: error = nil")
		}
		if after, _ := paymentsRepo.Get(ctx, payment.ID); after.Status != domain.PaymentPending {
			t.Fatalf("after 5xx status = %q, want pending", after.Status)
		}

		stub.set("Completed", http.StatusOK)
		got, err = subscriptions.HandleGatewayCallback(ctx, payment.ID, domain.MethodBkash, forged)
		if err != nil || got.Status != domain.PaymentCompleted {
			t.Fatalf("completed callback = %+v, %v", got, err)
		}
		active, _ := subsRepo.Get(ctx, subscription.ID)
		if active.Status != domain.StatusActive {
			t.Fatalf("subscription status = %q, want active", active.Status)
		}
	})

	t.Run("a cancelled subscription is never reactivated and the payment rolls back", func(t *testing.T) {
		stub.set("Initiated", http.StatusOK)
		subscription, payment, err := subscriptions.Subscribe(ctx, tenant.ID, pkg.ID, domain.MethodBkash, nil, 1)
		if err != nil {
			t.Fatalf("Subscribe(bkash) error = %v", err)
		}
		if _, err := subscriptions.Cancel(ctx, subscription.ID, nil); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		stub.set("Completed", http.StatusOK)
		_, err = subscriptions.HandleGatewayCallback(ctx, payment.ID, domain.MethodBkash, map[string]any{"status": "success", "paymentID": *payment.GatewayRef})
		if !apperr.Is(err, apperr.InvalidState) {
			t.Fatalf("late success on cancelled subscription error = %v, want INVALID_STATE", err)
		}
		if after, _ := subsRepo.Get(ctx, subscription.ID); after.Status != domain.StatusCancelled {
			t.Fatalf("subscription status = %q, want cancelled", after.Status)
		}
		// Completion and activation share a transaction: the activation failed,
		// so the payment was not marked completed either.
		if after, _ := paymentsRepo.Get(ctx, payment.ID); after.Status != domain.PaymentPending {
			t.Fatalf("payment status = %q, want pending (rolled back)", after.Status)
		}
	})

	t.Run("storage.mb blocks uploads past the plan limit", func(t *testing.T) {
		const mb = 1 << 20
		if err := guard.RequireStorage(ctx, tenant.ID, 0, mb/2); err != nil {
			t.Fatalf("within limit: %v", err)
		}
		err := guard.RequireStorage(ctx, tenant.ID, mb/2, mb)
		if !apperr.Is(err, apperr.UsageLimitExceeded) {
			t.Fatalf("over limit error = %v, want USAGE_LIMIT_EXCEEDED", err)
		}
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Meta["feature_key"] != string(domain.FeatureStorageMB) {
			t.Fatalf("over limit meta = %+v", appErr)
		}
	})
}
