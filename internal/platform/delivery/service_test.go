package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/crypto"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/queue"
)

type memoryRepo struct {
	mu   sync.Mutex
	rows []Record
}

func (m *memoryRepo) Create(_ context.Context, entry Entry) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := int64(len(m.rows) + 1)
	record := Record{ID: id, Channel: string(entry.Channel), Address: entry.Address, Template: entry.Template, Status: entry.Status}
	if entry.Error != "" {
		record.Error = &entry.Error
	}
	m.rows = append(m.rows, record)
	return id, nil
}

func (m *memoryRepo) MarkSent(_ context.Context, id int64, providerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := &m.rows[id-1]
	row.Status, row.Attempts, row.Error = StatusSent, row.Attempts+1, nil
	if providerID != "" {
		row.ProviderMessageID = &providerID
	}
	return nil
}

func (m *memoryRepo) MarkFailed(_ context.Context, id int64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := &m.rows[id-1]
	row.Status, row.Attempts, row.Error = StatusFailed, row.Attempts+1, &reason
	return nil
}

func (m *memoryRepo) Get(_ context.Context, id int64) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.rows[id-1]
	return &row, nil
}

func (m *memoryRepo) List(context.Context, ListFilter) ([]Record, error) { return m.rows, nil }

func (m *memoryRepo) CountByDay(_ context.Context, since time.Time) ([]DayCount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var counts []DayCount
	for _, row := range m.rows {
		if row.CreatedAt.Before(since) {
			continue
		}
		counts = append(counts, DayCount{Day: row.CreatedAt.UTC().Truncate(24 * time.Hour), Channel: string(row.Channel), Status: row.Status, Count: 1})
	}
	return counts, nil
}

func (m *memoryRepo) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

type recordingSender struct {
	mu       sync.Mutex
	contents []Content
	fail     error
}

func (r *recordingSender) Name() string { return "recording" }

func (r *recordingSender) Send(_ context.Context, _ Message, content Content) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return "", r.fail
	}
	r.contents = append(r.contents, content)
	return "provider-1", nil
}

type fakeOpener map[string][]byte

func (f fakeOpener) Open(_ context.Context, disk, path string) (io.ReadCloser, error) {
	data, ok := f[disk+":"+path]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// newTestService runs jobs inline (sync engine), so Send delivers at once.
func newTestService(t *testing.T) (*Service, *memoryRepo, *recordingSender, *recordingSender) {
	t.Helper()
	registry := queue.NewRegistry()
	repo := &memoryRepo{}
	mailSender, smsSender := &recordingSender{}, &recordingSender{}
	service := NewService(NewRegistry(), repo, queue.NewSyncEngine(registry), slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithSender(Mail, mailSender).
		WithSender(SMS, smsSender).
		WithBrand(StaticBrand(testBrand))
	if err := service.RegisterJob(registry); err != nil {
		t.Fatalf("RegisterJob() error = %v", err)
	}
	return service, repo, mailSender, smsSender
}

func TestSendDeliversEachChannel(t *testing.T) {
	service, repo, mailSender, smsSender := newTestService(t)
	err := service.Send(context.Background(), Message{
		To:       Recipient{UserID: 3, Name: "Jane", Email: "jane@acme.test", Phone: "+8801712345678", Locale: "bn"},
		Template: "otp-code",
		Data:     map[string]any{"code": "123456", "minutes": 5},
		Channels: []Channel{Mail, SMS},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(mailSender.contents) != 1 || len(smsSender.contents) != 1 {
		t.Fatalf("sent mail=%d sms=%d, want one each", len(mailSender.contents), len(smsSender.contents))
	}
	if got := mailSender.contents[0]; got.To != "jane@acme.test" || !strings.Contains(got.Text, "123456") || !strings.Contains(got.Text, "৫ মিনিট") {
		t.Errorf("mail = %+v, want the Bangla code mail to jane", got)
	}
	if got := smsSender.contents[0]; got.To != "+8801712345678" {
		t.Errorf("sms to = %q", got.To)
	}
	if len(repo.rows) != 2 {
		t.Fatalf("log rows = %d, want 2", len(repo.rows))
	}
	for _, row := range repo.rows {
		if row.Status != StatusSent || row.Template != "otp-code" || strings.Contains(row.Address, "jane@") {
			t.Errorf("row = %+v, want sent with a masked address", row)
		}
	}
}

func TestSendSkipsChannelsWithoutAnAddress(t *testing.T) {
	service, repo, mailSender, smsSender := newTestService(t)
	err := service.Send(context.Background(), Message{
		To:       Recipient{Email: "jane@acme.test"},
		Template: "otp-code",
		Data:     map[string]any{"code": "1", "minutes": 1},
		Channels: []Channel{Mail, SMS},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(mailSender.contents) != 1 || len(smsSender.contents) != 0 {
		t.Fatalf("sent mail=%d sms=%d, want mail only", len(mailSender.contents), len(smsSender.contents))
	}
	if repo.rows[1].Status != StatusSkipped {
		t.Fatalf("sms row = %+v, want skipped", repo.rows[1])
	}
}

func TestSendRefusesBadMessagesBeforeQueueing(t *testing.T) {
	service, repo, _, _ := newTestService(t)
	cases := map[string]Message{
		"unknown template": {To: Recipient{Email: "a@b.test"}, Template: "nope"},
		"missing data":     {To: Recipient{Email: "a@b.test"}, Template: "otp-code", Data: map[string]any{"minutes": 5}},
		"no sender":        {To: Recipient{UserID: 1}, Template: "otp-code", Data: map[string]any{"code": "1", "minutes": 1}, Channels: []Channel{Push}},
		"raw without text": {To: Recipient{Email: "a@b.test"}, Subject: "Hi"},
	}
	for name, message := range cases {
		if err := service.Send(context.Background(), message); err == nil {
			t.Errorf("%s: Send() error = nil", name)
		}
	}
	if len(repo.rows) != 0 {
		t.Fatalf("log rows = %d, want none for refused messages", len(repo.rows))
	}
}

func TestRawMessages(t *testing.T) {
	service, _, mailSender, _ := newTestService(t)
	if err := service.Send(context.Background(), Message{
		To: Recipient{Email: "jane@acme.test"}, Subject: "Weekly report", Text: "See attached.",
	}); err != nil {
		t.Fatalf("Send(text) error = %v", err)
	}
	if err := service.Send(context.Background(), Message{
		To: Recipient{Email: "jane@acme.test"}, Subject: "Weekly report", Text: "See attached.", HTML: "<p>See <b>attached</b>.</p>",
	}); err != nil {
		t.Fatalf("Send(html) error = %v", err)
	}
	text, html := mailSender.contents[0], mailSender.contents[1]
	if text.Subject != "Weekly report" || text.Text != "See attached." || text.HTML != "" {
		t.Errorf("text-only raw mail = %+v", text)
	}
	if !strings.Contains(html.HTML, "<b>attached</b>") || !strings.Contains(html.HTML, testBrand.Footer) {
		t.Errorf("raw HTML is not in the layout:\n%s", html.HTML)
	}
}

func TestFailedDeliveryIsRecordedAndReturned(t *testing.T) {
	service, repo, mailSender, _ := newTestService(t)
	mailSender.fail = errors.New("smtp: 421 try later")
	err := service.Send(context.Background(), Message{
		To: Recipient{Email: "jane@acme.test"}, Template: "otp-code", Data: map[string]any{"code": "1", "minutes": 1},
	})
	if !apperr.Is(err, apperr.ServiceUnavailable) {
		t.Fatalf("Send() error = %v, want SERVICE_UNAVAILABLE", err)
	}
	row := repo.rows[0]
	if row.Status != StatusFailed || row.Attempts != 1 || row.Error == nil || !strings.Contains(*row.Error, "421") {
		t.Fatalf("row = %+v, want one failed attempt with the reason", row)
	}
}

func TestAttachments(t *testing.T) {
	service, repo, mailSender, _ := newTestService(t)
	service.WithAttachments(fakeOpener{"private:reports/q3.pdf": []byte("%PDF-1.7 report")}, 1<<20)
	message := Message{
		To: Recipient{Email: "jane@acme.test"}, Subject: "Q3", Text: "Attached.",
		Attachments: []Attachment{{Disk: "private", Path: "reports/q3.pdf", Name: "q3.pdf", ContentType: "application/pdf"}},
	}
	if err := service.Send(context.Background(), message); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	files := mailSender.contents[0].Files
	if len(files) != 1 || files[0].Name != "q3.pdf" || string(files[0].Data) != "%PDF-1.7 report" {
		t.Fatalf("files = %+v", files)
	}

	service.WithAttachments(fakeOpener{"private:reports/q3.pdf": bytes.Repeat([]byte("x"), 2048)}, 1024)
	if err := service.Send(context.Background(), message); err == nil {
		t.Fatal("an attachment over the cap was sent")
	}
	if last := repo.rows[len(repo.rows)-1]; last.Status != StatusFailed || !strings.Contains(*last.Error, "exceed") {
		t.Fatalf("row = %+v, want failed for size", last)
	}
}

func TestLocaleComesFromTheContextThenTheFallback(t *testing.T) {
	service, _, mailSender, _ := newTestService(t)
	service.WithFallbackLocale("bn")
	if err := service.Send(context.Background(), Message{
		To: Recipient{Email: "jane@acme.test"}, Template: "otp-code", Data: map[string]any{"code": "1", "minutes": 1},
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.Contains(mailSender.contents[0].Text, "কোড") {
		t.Fatalf("text = %q, want the Bangla fallback", mailSender.contents[0].Text)
	}
}

func TestPreviewUsesSampleData(t *testing.T) {
	service, repo, mailSender, _ := newTestService(t)
	content, err := service.Preview(context.Background(), "password-reset", Mail, "en", nil)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if !strings.Contains(content.Text, "reset-password?token=sample") || content.HTML == "" {
		t.Fatalf("preview = %+v", content)
	}
	if len(repo.rows) != 0 || len(mailSender.contents) != 0 {
		t.Fatal("preview sent or logged a message")
	}
	if _, err := service.Preview(context.Background(), "nope", Mail, "en", nil); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("Preview(unknown) error = %v, want RESOURCE_NOT_FOUND", err)
	}
}

// capturingEngine keeps the encoded payloads instead of running them.
type capturingEngine struct{ payloads [][]byte }

func (c *capturingEngine) Enqueue(_ context.Context, _ string, payload any, _ queue.EnqueueOptions) error {
	raw, err := json.Marshal(payload)
	c.payloads = append(c.payloads, raw)
	return err
}

func (c *capturingEngine) EnqueueTx(ctx context.Context, _ pgx.Tx, kind string, payload any, opts queue.EnqueueOptions) error {
	return c.Enqueue(ctx, kind, payload, opts)
}

func (c *capturingEngine) Kind() string { return "capture" }

func TestSensitiveDataIsSealedInTheQueue(t *testing.T) {
	encrypter, err := crypto.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("crypto.New() error = %v", err)
	}
	registry := queue.NewRegistry()
	engine := &capturingEngine{}
	mailSender := &recordingSender{}
	service := NewService(NewRegistry(), &memoryRepo{}, engine, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithSender(Mail, mailSender).
		WithEncrypter(encrypter)
	if err := service.RegisterJob(registry); err != nil {
		t.Fatalf("RegisterJob() error = %v", err)
	}
	err = service.Send(context.Background(), Message{
		To: Recipient{Email: "jane@acme.test"}, Template: "otp-code",
		Data: map[string]any{"code": "908172", "minutes": 5}, Sensitive: true,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(engine.payloads) != 1 || bytes.Contains(engine.payloads[0], []byte("908172")) {
		t.Fatalf("queued payload shows the code: %s", engine.payloads)
	}

	definition, _ := registry.Definition(KindSend)
	if err := definition.Handle(context.Background(), engine.payloads[0]); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(mailSender.contents) != 1 || !strings.Contains(mailSender.contents[0].Text, "908172") {
		t.Fatalf("the mail does not carry the code: %+v", mailSender.contents)
	}
}
