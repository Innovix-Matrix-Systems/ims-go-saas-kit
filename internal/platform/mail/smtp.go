package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/tracing"
)

// SMTPDriver delivers mail through an SMTP server (plain, STARTTLS or TLS).
type SMTPDriver struct {
	cfg config.Mail
}

// NewSMTPDriver creates the SMTP driver.
func NewSMTPDriver(cfg config.Mail) *SMTPDriver {
	return &SMTPDriver{cfg: cfg}
}

// Name implements Driver.
func (d *SMTPDriver) Name() string { return "smtp" }

// Send implements Driver. The span names the server, never the addresses.
func (d *SMTPDriver) Send(ctx context.Context, message Message) (err error) {
	ctx, end := tracing.Begin(ctx, "mail.smtp_send", attribute.String("server.address", d.cfg.Host))
	defer end(&err)
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := d.render(message)
	if err != nil {
		return err
	}
	host := d.cfg.Host
	addr := net.JoinHostPort(host, strconv.Itoa(d.cfg.Port))
	serverName := strings.Split(host, ":")[0]

	var client *smtp.Client
	if strings.EqualFold(d.cfg.Encryption, "tls") {
		conn, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{ServerName: serverName})
		if dialErr != nil {
			return fmt.Errorf("mail: smtp dial: %w", dialErr)
		}
		// A stalled SMTP server must not hang the mail worker forever (M52).
		_ = conn.SetDeadline(smtpDeadline(ctx))
		client, err = smtp.NewClient(conn, serverName)
	} else {
		conn, dialErr := net.DialTimeout("tcp", addr, 10*time.Second)
		if dialErr != nil {
			return fmt.Errorf("mail: smtp dial: %w", dialErr)
		}
		_ = conn.SetDeadline(smtpDeadline(ctx))
		client, err = smtp.NewClient(conn, serverName)
	}
	if err != nil {
		return fmt.Errorf("mail: smtp connect: %w", err)
	}
	defer client.Close()

	if strings.EqualFold(d.cfg.Encryption, "starttls") {
		if err := client.StartTLS(&tls.Config{ServerName: serverName}); err != nil {
			return fmt.Errorf("mail: smtp starttls: %w", err)
		}
	}
	if d.cfg.Username != "" {
		auth := smtp.PlainAuth("", d.cfg.Username, d.cfg.Password, serverName)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mail: smtp auth: %w", err)
		}
	}
	if err := client.Mail(d.cfg.FromAddress); err != nil {
		return fmt.Errorf("mail: smtp from: %w", err)
	}
	for _, recipient := range message.Recipients() {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("mail: smtp rcpt: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: smtp data: %w", err)
	}
	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("mail: smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mail: smtp close: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("mail: smtp quit: %w", err)
	}
	return nil
}

// render builds the RFC 5322 message: text/plain alone, text + HTML as
// multipart/alternative, and with attachments the whole wrapped in
// multipart/mixed. Header values are sanitized and RFC 2047 encoded, so
// non-ASCII subjects and names (Bangla) arrive intact.
func (d *SMTPDriver) render(message Message) ([]byte, error) {
	var buf bytes.Buffer
	writeHeader := func(key, value string) { fmt.Fprintf(&buf, "%s: %s\r\n", key, value) }

	from := sanitizeHeader(d.cfg.FromAddress)
	if name := sanitizeHeader(d.cfg.FromName); name != "" {
		from = (&netmail.Address{Name: name, Address: from}).String()
	}
	writeHeader("From", from)
	writeHeader("To", sanitizeHeader(message.To))
	if len(message.CC) > 0 {
		writeHeader("Cc", sanitizeHeader(strings.Join(message.CC, ", ")))
	}
	if message.ReplyTo != "" {
		writeHeader("Reply-To", sanitizeHeader(message.ReplyTo))
	}
	writeHeader("Subject", mime.QEncoding.Encode("UTF-8", sanitizeHeader(message.Subject)))
	writeHeader("Date", time.Now().UTC().Format(time.RFC1123Z))
	for _, key := range sortedKeys(message.Headers) {
		writeHeader(textproto.CanonicalMIMEHeaderKey(sanitizeHeader(key)), sanitizeHeader(message.Headers[key]))
	}
	writeHeader("MIME-Version", "1.0")

	if len(message.Attachments) == 0 {
		if err := writeBody(&buf, message); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	mixed := multipart.NewWriter(&buf)
	writeHeader("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%q", mixed.Boundary()))
	buf.WriteString("\r\n")
	if err := writeBodyPart(mixed, message); err != nil {
		return nil, err
	}
	for _, attachment := range message.Attachments {
		if err := writeAttachment(mixed, attachment); err != nil {
			return nil, err
		}
	}
	if err := mixed.Close(); err != nil {
		return nil, fmt.Errorf("mail: close parts: %w", err)
	}
	return buf.Bytes(), nil
}

// writeBody writes the body headers, a blank line and the body: the text
// alone, or text + HTML as multipart/alternative.
func writeBody(buf *bytes.Buffer, message Message) error {
	if message.HTML == "" {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		return writeQuotedPrintable(buf, message.Text)
	}
	alternative := multipart.NewWriter(buf)
	fmt.Fprintf(buf, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", alternative.Boundary())
	if err := writeAlternatives(alternative, message); err != nil {
		return err
	}
	return nil
}

// writeBodyPart writes the body as the first part of multipart/mixed.
func writeBodyPart(mixed *multipart.Writer, message Message) error {
	if message.HTML == "" {
		part, err := mixed.CreatePart(textPartHeader("text/plain"))
		if err != nil {
			return fmt.Errorf("mail: body part: %w", err)
		}
		return writeQuotedPrintable(part, message.Text)
	}
	var parts bytes.Buffer
	alternative := multipart.NewWriter(&parts)
	if err := writeAlternatives(alternative, message); err != nil {
		return err
	}
	part, err := mixed.CreatePart(textproto.MIMEHeader{
		"Content-Type": {fmt.Sprintf("multipart/alternative; boundary=%q", alternative.Boundary())},
	})
	if err != nil {
		return fmt.Errorf("mail: body part: %w", err)
	}
	if _, err := part.Write(parts.Bytes()); err != nil {
		return fmt.Errorf("mail: body part: %w", err)
	}
	return nil
}

func writeAlternatives(alternative *multipart.Writer, message Message) error {
	for _, body := range []struct{ contentType, content string }{
		{"text/plain", message.Text},
		{"text/html", message.HTML},
	} {
		part, err := alternative.CreatePart(textPartHeader(body.contentType))
		if err != nil {
			return fmt.Errorf("mail: alternative part: %w", err)
		}
		if err := writeQuotedPrintable(part, body.content); err != nil {
			return err
		}
	}
	if err := alternative.Close(); err != nil {
		return fmt.Errorf("mail: close alternative: %w", err)
	}
	return nil
}

func textPartHeader(contentType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{
		"Content-Type":              {contentType + "; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	}
}

func writeQuotedPrintable(w io.Writer, text string) error {
	encoder := quotedprintable.NewWriter(w)
	if _, err := encoder.Write([]byte(text)); err != nil {
		return fmt.Errorf("mail: encode body: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("mail: encode body: %w", err)
	}
	return nil
}

func writeAttachment(mixed *multipart.Writer, attachment Attachment) error {
	contentType := sanitizeHeader(attachment.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	name := mime.QEncoding.Encode("UTF-8", sanitizeHeader(attachment.Name))
	part, err := mixed.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {fmt.Sprintf("%s; name=%q", contentType, name)},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", name)},
	})
	if err != nil {
		return fmt.Errorf("mail: attachment part: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(attachment.Data)
	for len(encoded) > 0 { // RFC 2045: lines of at most 76 characters
		n := min(76, len(encoded))
		if _, err := io.WriteString(part, encoded[:n]+"\r\n"); err != nil {
			return fmt.Errorf("mail: attachment: %w", err)
		}
		encoded = encoded[n:]
	}
	return nil
}

func sortedKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sanitizeHeader strips CR/LF (and NUL) so caller-supplied values cannot inject
// extra SMTP headers or recipients.
func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(value)
}

// smtpDeadline bounds the whole SMTP conversation: the context deadline when
// there is one, otherwise two minutes.
func smtpDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(2 * time.Minute)
}
