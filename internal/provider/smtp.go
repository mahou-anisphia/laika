package provider

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	gosmtp "net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"

	"laika/internal/config"
)

// ErrSMTPNotConfigured is returned by Probe when the flow has no credentials.
// Treated as "skip this flow", not as an auth failure — caller decides whether
// to warn or be silent.
var ErrSMTPNotConfigured = errors.New("smtp: not configured")

// Message holds the content fields shared across all recipients in a single send call.
type Message struct {
	From     string
	Subject  string
	HTMLBody string
}

// Result is the per-recipient outcome returned by Provider.Send.
type Result struct {
	MessageID string
	Err       error
}

// SMTPProvider opens one SMTP connection per Send call and delivers to each
// recipient in turn, reusing the connection (mirrors sendEmailsWithConnectionReuse).
type SMTPProvider struct {
	cfg config.SMTP
}

func NewSMTPProvider(cfg config.SMTP) *SMTPProvider {
	return &SMTPProvider{cfg: cfg}
}

// Probe verifies the flow can dial, negotiate TLS, and authenticate against the
// SMTP server. It does not send mail. Returns ErrSMTPNotConfigured when the
// required fields are blank — no network call is attempted in that case.
func (p *SMTPProvider) Probe() error {
	if p.cfg.Host == "" || p.cfg.Port == "" || p.cfg.Username == "" || p.cfg.Password == "" {
		return ErrSMTPNotConfigured
	}

	addr := net.JoinHostPort(p.cfg.Host, p.cfg.Port)
	tlsCfg := &tls.Config{ServerName: p.cfg.Host}

	client, err := p.dial(addr, tlsCfg)
	if err != nil {
		return err
	}
	defer client.Close()

	auth := gosmtp.PlainAuth("", p.cfg.Username, p.cfg.Password, p.cfg.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	return client.Quit()
}

// Send dials the SMTP server once, authenticates, then sends one message per
// recipient. A connection-level failure (dial, TLS, auth) is returned as the
// outer error. Per-recipient failures are captured in the Result slice.
func (p *SMTPProvider) Send(recipients []string, msg Message) ([]Result, error) {
	addr := net.JoinHostPort(p.cfg.Host, p.cfg.Port)
	tlsCfg := &tls.Config{ServerName: p.cfg.Host}

	client, err := p.dial(addr, tlsCfg)
	if err != nil {
		return nil, err
	}
	// mirrors the Spring finally { transport.close() }
	defer client.Close()

	auth := gosmtp.PlainAuth("", p.cfg.Username, p.cfg.Password, p.cfg.Host)
	if err = client.Auth(auth); err != nil {
		return nil, fmt.Errorf("smtp auth: %w", err)
	}

	results := make([]Result, len(recipients))
	for i, to := range recipients {
		msgID, sendErr := sendOne(client, to, msg)
		results[i] = Result{MessageID: msgID, Err: sendErr}
	}
	return results, nil
}

// dial creates the SMTP client. Port 465 uses implicit TLS (SMTPS); all other
// ports attempt STARTTLS if the server advertises it.
// Note: the Spring impl documents port 465 with "STARTTLS" but 465 is actually
// SMTPS (implicit TLS) — this implementation handles both correctly.
func (p *SMTPProvider) dial(addr string, tlsCfg *tls.Config) (*gosmtp.Client, error) {
	if p.cfg.Port == "465" {
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("smtps tls dial: %w", err)
		}
		client, err := gosmtp.NewClient(conn, p.cfg.Host)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("smtp client (smtps): %w", err)
		}
		return client, nil
	}

	client, err := gosmtp.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(tlsCfg); err != nil {
			client.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return client, nil
}

// sendOne executes a single MAIL→RCPT→DATA cycle on an existing connection.
// On any failure it resets the server state so the connection remains usable
// for subsequent recipients.
func sendOne(client *gosmtp.Client, to string, msg Message) (string, error) {
	if err := client.Mail(msg.From); err != nil {
		_ = client.Reset()
		return "", fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		_ = client.Reset()
		return "", fmt.Errorf("RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		_ = client.Reset()
		return "", fmt.Errorf("DATA: %w", err)
	}

	msgID := fmt.Sprintf("<%s@%s>", uuid.New().String(), strings.Split(msg.From, "@")[len(strings.Split(msg.From, "@"))-1])
	now := time.Now().UTC().Format(time.RFC1123Z)

	raw := strings.Join([]string{
		"From: " + msg.From,
		"To: " + to,
		"Subject: " + msg.Subject,
		"Message-ID: " + msgID,
		"Date: " + now,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		msg.HTMLBody,
	}, "\r\n")

	if _, err = fmt.Fprint(wc, raw); err != nil {
		_ = wc.Close()
		_ = client.Reset()
		return "", fmt.Errorf("write body: %w", err)
	}
	if err = wc.Close(); err != nil {
		_ = client.Reset()
		return "", fmt.Errorf("end data: %w", err)
	}
	return msgID, nil
}
