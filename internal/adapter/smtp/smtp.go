// Package smtp sends plain-text email through an SMTP relay (implicit
// TLS on 465, STARTTLS otherwise).
package smtp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Config is the relay.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "Kurgu <kurgu@example.com>"
	// TLS is "starttls" (default), "tls" (implicit, port 465) or "none"
	// (local relays only).
	TLS string
}

// Enabled reports whether a relay is configured.
func (c Config) Enabled() bool { return c.Host != "" && c.From != "" }

// Mailer sends messages.
type Mailer struct{ cfg Config }

// New returns a Mailer.
func New(cfg Config) *Mailer { return &Mailer{cfg: cfg} }

// Enabled reports whether mail can be sent.
func (m *Mailer) Enabled() bool { return m.cfg.Enabled() }

// Send sends a plain-text message to one recipient.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.cfg.Enabled() {
		return errors.New("smtp: not configured")
	}
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		return fmt.Errorf("smtp: from address: %w", err)
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: recipient: %w", err)
	}
	msg := strings.Join([]string{
		"From: " + from.String(),
		"To: " + rcpt.String(),
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.ReplaceAll(body, "\n", "\r\n"),
	}, "\r\n")

	port := m.cfg.Port
	if port == 0 {
		port = 587
		if m.cfg.TLS == "tls" {
			port = 465
		}
	}
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(port))
	var conn net.Conn
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	if m.cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp: dial: %w", err)
	}
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: hello: %w", err)
	}
	defer func() { _ = c.Close() }()
	if m.cfg.TLS != "tls" && m.cfg.TLS != "none" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: the server does not offer STARTTLS (set KURGU_SMTP_TLS=none only for a local relay)")
		}
		if err := c.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp: mail from: %w", err)
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("smtp: rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: send: %w", err)
	}
	return c.Quit()
}
