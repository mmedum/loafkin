package mail

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// fakeServer speaks just enough SMTP for one delivery, never offers
// STARTTLS, and answers the verb named by refuse with reply.
type fakeServer struct {
	addr   string
	data   chan string
	hello  chan string
	refuse string
	reply  string
}

func newFakeServer(t *testing.T, refuse, reply string) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeServer{addr: ln.Addr().String(), data: make(chan string, 1), hello: make(chan string, 1), refuse: refuse, reply: reply}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		f.serve(textproto.NewConn(conn))
	}()
	return f
}

func (f *fakeServer) serve(c *textproto.Conn) {
	_ = c.PrintfLine("220 fake ready")
	for {
		line, err := c.ReadLine()
		if err != nil {
			return
		}
		verb, _, _ := strings.Cut(strings.ToUpper(line), " ")
		switch {
		case verb == f.refuse:
			_ = c.PrintfLine("%s", f.reply)
		case verb == "EHLO" || verb == "HELO":
			select {
			case f.hello <- line:
			default:
			}
			_ = c.PrintfLine("250 ok")
		case verb == "DATA":
			_ = c.PrintfLine("354 go on")
			lines, _ := c.ReadDotLines()
			f.data <- strings.Join(lines, "\n")
			_ = c.PrintfLine("250 queued")
		case verb == "QUIT":
			_ = c.PrintfLine("221 bye")
			return
		default:
			_ = c.PrintfLine("250 ok")
		}
	}
}

func TestSendDeliversAQuotedPrintableMessage(t *testing.T) {
	f := newFakeServer(t, "", "")
	s := &SMTP{Addr: f.addr, From: "loafkin@example.com", TLS: NoTLS}

	if err := s.Send(t.Context(), 1, "member@example.com", "Your code: 123456", "Æblegrød\r\nYour code is\n123456."); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := <-f.hello; got != "EHLO example.com" {
		t.Errorf("greeting = %q, want %q", got, "EHLO example.com")
	}
	got := <-f.data
	header, body, _ := strings.Cut(got, "\n\n")
	for _, want := range []string{
		"From: loafkin@example.com",
		"To: member@example.com",
		"Subject: Your code: 123456",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
	} {
		if !strings.Contains(header+"\n", want+"\n") {
			t.Errorf("header lacks %q:\n%s", want, header)
		}
	}
	if want := "=C3=86blegr=C3=B8d\nYour code is\n123456."; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestSendReportsFailuresAsCodes(t *testing.T) {
	tests := []struct {
		name          string
		refuse        string
		reply         string
		tls           TLS
		to            string
		wantCode      string
		wantPermanent bool
	}{
		{"refused recipient", "RCPT", "550 no such user", NoTLS, "member@example.com", "smtp_550", true},
		{"refused message", "DATA", "554 rejected", NoTLS, "member@example.com", "smtp_554", true},
		{"recipient busy", "RCPT", "450 try later", NoTLS, "member@example.com", "smtp_450", false},
		{"refused sender", "MAIL", "550 not allowed", NoTLS, "member@example.com", "smtp_550", false},
		{"STARTTLS not offered", "", "", StartTLS, "member@example.com", "smtp_starttls_missing", false},
		{"implicit TLS to a plain server", "", "", ImplicitTLS, "member@example.com", "smtp_dial", false},
		{"header injection", "", "", NoTLS, "member@example.com\r\nBcc: other@example.com", "smtp_message", true},
		{"display name", "", "", NoTLS, "Member <member@example.com>", "smtp_message", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeServer(t, tt.refuse, tt.reply)
			s := &SMTP{Addr: f.addr, From: "loafkin@example.com", TLS: tt.tls}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err := s.Send(ctx, 1, tt.to, "Subject", "Body")

			mailErr, ok := errors.AsType[*Error](err)
			if !ok || mailErr.ErrorCode() != tt.wantCode || mailErr.Permanent() != tt.wantPermanent {
				t.Fatalf("Send to %q: err %v, want code %s, permanent %v", tt.to, err, tt.wantCode, tt.wantPermanent)
			}
			select {
			case got := <-f.data:
				t.Errorf("the server received a message: %q", got)
			default:
			}
		})
	}
}

func TestParseTLS(t *testing.T) {
	tests := []struct {
		in      string
		want    TLS
		wantErr bool
	}{
		{"", ImplicitTLS, false},
		{"implicit", ImplicitTLS, false},
		{"starttls", StartTLS, false},
		{"none", NoTLS, false},
		{"off", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseTLS(tt.in)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("ParseTLS(%q) = %v, %v; want %v, error %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}
