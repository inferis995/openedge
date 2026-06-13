package notifications

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/smtp"
	"strconv"
	"strings"
)

// smtpCfg holds just the fields needed to send transactional email.
// Loaded from global_settings the same way alarm email is, but in a
// lighter struct that doesn't require a full Dispatcher.
type smtpCfg struct {
	host     string
	port     int
	username string
	password string
	from     string
	useTLS   bool
}

func loadSMTPCfg(db *sql.DB) (*smtpCfg, bool) {
	rows, err := db.Query(`SELECT key, value FROM global_settings WHERE key LIKE 'notif\_email\_%' ESCAPE '\'`)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	cfg := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			cfg[k] = v
		}
	}
	host := strings.TrimSpace(cfg["notif_email_smtp_host"])
	from := strings.TrimSpace(cfg["notif_email_from"])
	if host == "" || from == "" || !strings.EqualFold(cfg["notif_email_enabled"], "true") {
		return nil, false
	}
	port := 587
	if p, err := strconv.Atoi(cfg["notif_email_smtp_port"]); err == nil && p > 0 {
		port = p
	}
	return &smtpCfg{
		host:     host,
		port:     port,
		username: cfg["notif_email_username"],
		password: cfg["notif_email_password"],
		from:     from,
		useTLS:   strings.EqualFold(cfg["notif_email_use_tls"], "true"),
	}, true
}

// SendInviteEmail sends a user-invite link to the given recipient address.
// It reuses the SMTP settings from global_settings (the same settings used
// for alarm email). If SMTP is not configured the error is logged and the
// invite token is still returned to the caller so they can share it manually.
func SendInviteEmail(db *sql.DB, recipientEmail, inviteURL string) {
	cfg, ok := loadSMTPCfg(db)
	if !ok {
		slog.Warn("invite email skipped — SMTP not configured", "recipient", recipientEmail)
		return
	}

	subject := "You've been invited to OpenEdge"
	body := fmt.Sprintf(`You have been invited to join OpenEdge.

Click the link below to create your account (valid for 7 days):

  %s

If you did not expect this invitation, ignore this email.
`, inviteURL)

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.from, recipientEmail, subject, body,
	)

	addr := fmt.Sprintf("%s:%d", cfg.host, cfg.port)
	auth := smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)

	var sendErr error
	if cfg.useTLS {
		sendErr = sendImplicitTLSTo(cfg.host, addr, auth, cfg.from, recipientEmail, []byte(msg))
	} else {
		sendErr = smtp.SendMail(addr, auth, cfg.from, []string{recipientEmail}, []byte(msg))
	}
	if sendErr != nil {
		slog.Error("invite email send failed", "recipient", recipientEmail, "err", sendErr)
	} else {
		slog.Info("invite email sent", "recipient", recipientEmail)
	}
}

func sendImplicitTLSTo(host, addr string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Quit() //nolint:errcheck
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}
