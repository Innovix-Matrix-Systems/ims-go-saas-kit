package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestLogDriverSends(t *testing.T) {
	driver, err := New(config.Mail{Driver: "log", FromAddress: "from@example.com"}, discardLogger(), false)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if driver.Name() != "log" {
		t.Fatalf("driver name = %q", driver.Name())
	}
	if err := driver.Send(context.Background(), Message{To: "to@example.com", Subject: "Hi", Text: "Body"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestSMTPDriverSendsAgainstListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	messages := make(chan string, 1)
	go serveFakeSMTP(t, listener, messages)

	port := listener.Addr().(*net.TCPAddr).Port
	driver := NewSMTPDriver(config.Mail{
		Host:        "127.0.0.1",
		Port:        port,
		FromAddress: "from@example.com",
		FromName:    "IMS Test",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.Send(ctx, Message{To: "to@example.com", Subject: "OTP code", Text: "123456 is your OTP."}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case captured := <-messages:
		if !strings.Contains(captured, "Subject: OTP code") {
			t.Errorf("captured mail missing subject:\n%s", captured)
		}
		if !strings.Contains(captured, "123456 is your OTP.") {
			t.Errorf("captured mail missing body:\n%s", captured)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake SMTP server captured no message")
	}
}

func serveFakeSMTP(t *testing.T, listener net.Listener, messages chan<- string) {
	t.Helper()
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 fake ESMTP\r\n")
	var body strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			fmt.Fprint(conn, "250-fake\r\n250 OK\r\n")
		case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
			fmt.Fprint(conn, "250 OK\r\n")
		case command == "DATA":
			fmt.Fprint(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
				body.WriteString(dataLine)
			}
			fmt.Fprint(conn, "250 OK\r\n")
			select {
			case messages <- body.String():
			default:
			}
		case command == "QUIT":
			fmt.Fprint(conn, "221 Bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 OK\r\n")
		}
	}
}

func TestSMTPRenderBuildsMultipartMail(t *testing.T) {
	driver := NewSMTPDriver(config.Mail{FromAddress: "from@example.com", FromName: "আইএমএস"})
	raw, err := driver.render(Message{
		To:      "to@example.com",
		CC:      []string{"cc@example.com"},
		BCC:     []string{"hidden@example.com"},
		ReplyTo: "support@example.com",
		Subject: "আপনার কোড",
		Text:    "Plain body",
		HTML:    "<p>HTML body</p>",
		Attachments: []Attachment{
			{Name: "report.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.7")},
		},
	})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "আপনার কোড" {
		t.Errorf("subject = %q (%v), want the Bangla subject", subject, err)
	}
	if msg.Header.Get("Cc") != "cc@example.com" || msg.Header.Get("Reply-To") != "support@example.com" {
		t.Errorf("headers = %v", msg.Header)
	}
	if strings.Contains(string(raw), "hidden@example.com") {
		t.Error("the BCC address appears in the message")
	}

	mediaType, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mediaType != "multipart/mixed" {
		t.Fatalf("content type = %q, want multipart/mixed", mediaType)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	body, err := parts.NextPart()
	if err != nil {
		t.Fatalf("body part: %v", err)
	}
	bodyType, bodyParams, _ := mime.ParseMediaType(body.Header.Get("Content-Type"))
	if bodyType != "multipart/alternative" {
		t.Fatalf("body type = %q, want multipart/alternative", bodyType)
	}
	alternatives := multipart.NewReader(body, bodyParams["boundary"])
	var got []string
	for {
		part, err := alternatives.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("alternative: %v", err)
		}
		content, _ := io.ReadAll(part) // NextPart decodes quoted-printable
		got = append(got, part.Header.Get("Content-Type")+"="+string(content))
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "text/plain") || !strings.HasSuffix(got[0], "Plain body") ||
		!strings.HasPrefix(got[1], "text/html") || !strings.HasSuffix(got[1], "<p>HTML body</p>") {
		t.Fatalf("alternatives = %q", got)
	}
	attachment, err := parts.NextPart()
	if err != nil {
		t.Fatalf("attachment part: %v", err)
	}
	data, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, attachment))
	if attachment.FileName() != "report.pdf" || string(data) != "%PDF-1.7" {
		t.Fatalf("attachment = %q %q", attachment.FileName(), data)
	}
}

func TestSMTPRenderPlainTextStaysSimple(t *testing.T) {
	raw, err := NewSMTPDriver(config.Mail{FromAddress: "from@example.com"}).render(Message{To: "to@example.com", Subject: "Hi", Text: "Body"})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	if !strings.HasPrefix(msg.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("content type = %q, want text/plain", msg.Header.Get("Content-Type"))
	}
}

func TestSMTPRenderWritesExtraHeadersSafely(t *testing.T) {
	raw, err := NewSMTPDriver(config.Mail{FromAddress: "from@example.com"}).render(Message{
		To: "to@example.com", Subject: "Hi", Text: "Body",
		Headers: map[string]string{"list-unsubscribe": "<https://api.test/u?token=x>\r\nBcc: evil@example.com"},
	})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	if got := msg.Header.Get("List-Unsubscribe"); !strings.HasPrefix(got, "<https://api.test/u?token=x>") {
		t.Fatalf("List-Unsubscribe = %q", got)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("a header value injected another header")
	}
}
