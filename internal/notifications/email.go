package notifications

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// smtpDialTimeout bounds the TCP connect + TLS handshake, and smtpSessionTimeout
// the whole SMTP conversation when the caller's context has no deadline.
// Without them a firewalled relay (packets dropped, no RST) parks the send for
// minutes: the dispatcher walks its channels sequentially inside one goroutine,
// so a hung email delays — or with its 30 s ctx cancels — Telegram, Slack,
// Teams and PagerDuty for the same alarm.
const (
	smtpDialTimeout    = 10 * time.Second
	smtpSessionTimeout = 30 * time.Second
)

// emailChannel sends alarm events as plain-text email via SMTP+STARTTLS
// or implicit TLS (port 465). Auth is plain LOGIN — covers Gmail
// (with app password), Office 365 SMTP, and most on-prem relays.
type emailChannel struct {
	enabled  bool
	host     string
	port     int
	username string
	password string
	from     string
	to       []string // comma-separated in settings, split here
	useTLS   bool     // implicit TLS (port 465); STARTTLS is auto-detected otherwise
}

func newEmailChannel(cfg map[string]string) Channel {
	c := &emailChannel{
		enabled:  strings.EqualFold(cfg["notif_email_enabled"], "true"),
		host:     strings.TrimSpace(cfg["notif_email_smtp_host"]),
		username: cfg["notif_email_username"],
		password: cfg["notif_email_password"],
		from:     strings.TrimSpace(cfg["notif_email_from"]),
		useTLS:   strings.EqualFold(cfg["notif_email_use_tls"], "true"),
	}
	if p, err := strconv.Atoi(cfg["notif_email_smtp_port"]); err == nil && p > 0 {
		c.port = p
	} else {
		c.port = 587 // sensible default for STARTTLS submission
	}
	for _, addr := range strings.Split(cfg["notif_email_to"], ",") {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			c.to = append(c.to, addr)
		}
	}
	// A configured-but-incomplete channel is treated as disabled: better
	// to silently skip than to spam the log every alarm with the same
	// "missing field" error.
	if c.host == "" || c.from == "" || len(c.to) == 0 {
		c.enabled = false
	}
	return c
}

func (c *emailChannel) Name() string  { return "email" }
func (c *emailChannel) Enabled() bool { return c.enabled }

func (c *emailChannel) Send(ctx context.Context, e *Event) error {
	subject := fmt.Sprintf("[OpenEdge][%s] %s — %s",
		strings.ToUpper(e.Severity), strings.ToUpper(e.Status), e.TagAlias)
	body := renderText(*e)

	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		c.from, strings.Join(c.to, ", "), subject, body,
	)

	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	auth := smtp.PlainAuth("", c.username, c.password, c.host)

	// Both paths go through sendSMTP: net/smtp.SendMail has no timeout and no
	// context, so we dial ourselves and put a deadline on the socket.
	return c.sendSMTP(ctx, addr, auth, []byte(msg))
}

// dial opens the transport to the relay, honouring ctx and a hard dial
// timeout. useTLS servers wrap the whole connection in TLS from byte zero
// (typically port 465); the others are upgraded later via STARTTLS.
func (c *emailChannel) dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: smtpDialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}

	// net/smtp offers no context-aware I/O, so the ctx deadline (or a default)
	// is enforced with a socket deadline covering the whole conversation:
	// a relay that accepts the TCP connection and then stalls can't hang us.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(smtpSessionTimeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("smtp deadline: %w", err)
	}

	if !c.useTLS {
		return conn, nil
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: c.host,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tls dial: %w", err)
	}
	return tlsConn, nil
}

// sendSMTP performs the SMTP conversation on a bounded connection. It is the
// timeout-aware equivalent of smtp.SendMail: STARTTLS is negotiated when the
// server advertises it (587 submission, 25 legacy relays) and skipped when the
// connection is already implicit TLS (465).
func (c *emailChannel) sendSMTP(ctx context.Context, addr string, auth smtp.Auth, msg []byte) error {
	conn, err := c.dial(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if !c.useTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{
				ServerName: c.host,
				MinVersion: tls.VersionTLS12,
			}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	if c.username != "" {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(c.from); err != nil {
		return err
	}
	for _, to := range c.to {
		if err := client.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func renderText(e Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Severity: %s\n", strings.ToUpper(e.Severity))
	fmt.Fprintf(&b, "Status:   %s\n", e.Status)
	fmt.Fprintf(&b, "Tag:      %s\n", e.TagAlias)
	fmt.Fprintf(&b, "Value:    %g\n", e.Value)
	fmt.Fprintf(&b, "Threshold:%g\n", e.Threshold)
	fmt.Fprintf(&b, "When:     %s\n", e.OccurredAt.Format(time.RFC3339))
	if e.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", e.Description)
	}
	if e.AlarmID > 0 {
		fmt.Fprintf(&b, "\nAlarm event id: %d\n", e.AlarmID)
	}
	return b.String()
}
