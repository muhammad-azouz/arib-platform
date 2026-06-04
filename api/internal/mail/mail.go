// Package mail delivers transactional email (currently just OTP codes).
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

// Sender delivers login codes over SMTP, or logs them when SMTP is unconfigured.
type Sender struct {
	host string
	addr string
	auth smtp.Auth
	from string
	log  *slog.Logger
}

// Config configures the SMTP Sender.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// New builds a Sender. With an empty Host it runs in log-only (dev) mode.
func New(c Config, log *slog.Logger) *Sender {
	s := &Sender{from: c.From, log: log}
	if c.Host != "" {
		s.host = c.Host
		s.addr = fmt.Sprintf("%s:%d", c.Host, c.Port)
		s.auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}
	return s
}

// SendOTP emails (or logs) a one-time login code.
func (s *Sender) SendOTP(ctx context.Context, to, code string) error {
	if s.host == "" {
		s.log.Warn("SMTP not configured; logging OTP for dev only", "email", to, "code", code)
		return nil
	}
	subject := "Arib POS login code"
	body := fmt.Sprintf("Your Arib POS login code is: %s\r\n\r\nIt expires shortly. If you did not request it, ignore this email.", code)
	msg := strings.Join([]string{
		"From: " + s.from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")
	return smtp.SendMail(s.addr, s.auth, extractAddr(s.from), []string{to}, []byte(msg))
}

func extractAddr(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}
