package authn

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	cachememory "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/memory"
)

type recordingDeliverer struct {
	mu       sync.Mutex
	messages []AccountMessage
}

func (d *recordingDeliverer) DeliverAccount(_ context.Context, message AccountMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.messages = append(d.messages, message)
	return nil
}

func (d *recordingDeliverer) snapshot() []AccountMessage {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]AccountMessage(nil), d.messages...)
}

func TestOTPSendDeliversTheCode(t *testing.T) {
	store := cachememory.New("otp-test-")
	t.Cleanup(func() { _ = store.Clear(context.Background()) })

	service := NewOTPService(store, config.Auth{OTPLength: 6, OTPExpiry: 5 * time.Minute}, slog.Default(), false)
	deliverer := &recordingDeliverer{}
	service.WithDeliverer(deliverer)

	user := &UserRecord{ID: 7, Name: "Jane", Email: "user@example.com"}
	code, err := service.Send(context.Background(), user)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}

	// The stored code verifies.
	if err := service.Verify(context.Background(), 7, code); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	sent := deliverer.snapshot()
	if len(sent) != 1 {
		t.Fatalf("delivered %d messages, want 1", len(sent))
	}
	if sent[0].User != user || sent[0].Template != TemplateOTPCode {
		t.Errorf("message = %+v", sent[0])
	}
	if sent[0].Data["code"] != code || sent[0].Data["minutes"] != 5 {
		t.Errorf("data = %v, want the code and 5 minutes", sent[0].Data)
	}
}

func TestOTPSendWithoutDelivererStillStores(t *testing.T) {
	store := cachememory.New("otp-test-")
	t.Cleanup(func() { _ = store.Clear(context.Background()) })

	service := NewOTPService(store, config.Auth{OTPLength: 6, OTPExpiry: 5 * time.Minute}, slog.Default(), false)
	code, err := service.Send(context.Background(), &UserRecord{ID: 8})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := service.Verify(context.Background(), 8, code); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}
