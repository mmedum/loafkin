// Package mail sends email over SMTP. The sender's TLS is required
// unless the configuration turns it off for a local mail catcher.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// TLS is how the connection to the SMTP server is protected.
type TLS int

const (
	// ImplicitTLS opens TLS before SMTP starts, on port 465.
	ImplicitTLS TLS = iota + 1
	// StartTLS upgrades a plain connection, usually on port 587, and fails
	// when the server does not offer the upgrade.
	StartTLS
	// NoTLS sends in the clear, which only a local mail catcher accepts.
	NoTLS
)

// ParseTLS reads the LOAFKIN_SMTP_TLS setting, where empty means implicit.
func ParseTLS(s string) (TLS, error) {
	switch s {
	case "", "implicit":
		return ImplicitTLS, nil
	case "starttls":
		return StartTLS, nil
	case "none":
		return NoTLS, nil
	}
	return 0, errors.New("mail: LOAFKIN_SMTP_TLS is implicit, starttls or none")
}

// sendTimeout bounds one whole delivery, so a stalled server cannot hold
// an outbox row locked.
const sendTimeout = 30 * time.Second

// SMTP sends each message in its own connection.
type SMTP struct {
	Addr     string // host:port
	Username string // empty for no authentication
	Password string
	From     string
	TLS      TLS
}

// Error carries a code that is safe to log. SMTP replies and dial errors
// can echo addresses, so their text never reaches a log (D-33).
type Error struct {
	code string
	err  error
}

func (e *Error) Error() string     { return "mail: " + e.code }
func (e *Error) Unwrap() error     { return e.err }
func (e *Error) ErrorCode() string { return e.code }

func fail(code string, err error) error {
	if reply, ok := errors.AsType[*textproto.Error](err); ok {
		code = fmt.Sprintf("smtp_%d", reply.Code)
	}
	return &Error{code: code, err: err}
}

// Send delivers one plain-text message to one recipient.
func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	msg, err := message(s.From, to, subject, body, time.Now())
	if err != nil {
		return fail("smtp_message", err)
	}
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return fail("smtp_config", err)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if s.TLS == ImplicitTLS {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", s.Addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", s.Addr)
	}
	if err != nil {
		return fail("smtp_dial", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fail("smtp_greeting", err)
	}
	defer c.Close()
	if s.TLS == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fail("smtp_starttls_missing", nil)
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return fail("smtp_starttls", err)
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return fail("smtp_auth", err)
		}
	}
	if err := c.Mail(s.From); err != nil {
		return fail("smtp_mail", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fail("smtp_rcpt", err)
	}
	w, err := c.Data()
	if err != nil {
		return fail("smtp_data", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fail("smtp_data", err)
	}
	if err := w.Close(); err != nil {
		return fail("smtp_data", err)
	}
	if err := c.Quit(); err != nil {
		return fail("smtp_quit", err)
	}
	return nil
}

// message builds an RFC 5322 message. It refuses addresses that do not
// parse, so a line break cannot add a header.
func message(from, to, subject, body string, now time.Time) ([]byte, error) {
	fromAddr, err := netmail.ParseAddress(from)
	if err != nil {
		return nil, err
	}
	toAddr, err := netmail.ParseAddress(to)
	if err != nil {
		return nil, err
	}
	if toAddr.Address != to || fromAddr.Address != from {
		return nil, errors.New("mail: an address carries a display name or extra text")
	}
	_, domain, _ := strings.Cut(from, "@")

	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", from)
	header("To", to)
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+rand.Text()+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
