package access

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestGmailInvitationMailer(t *testing.T) {
	const (
		sender      = "invitations@example.invalid"
		recipient   = "invitee@example.invalid"
		appPassword = "fixturemailpassw"
		link        = "https://placement.example.invalid/admin/invitation#fixture-token"
	)
	serverTLS, clientTLS := smtpFixtureTLS(t)
	for _, test := range []struct {
		name  string
		reply string
	}{
		{"accepted", "250 message accepted"},
		{"rejected", "554 message rejected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			result := make(chan error, 1)
			go func() {
				conn, err := server.Accept()
				if err != nil {
					result <- err
					return
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					result <- err
					return
				}
				smtp := textproto.NewConn(conn)
				auth := base64.StdEncoding.EncodeToString([]byte("\x00" + sender + "\x00" + appPassword))
				if err := smtp.PrintfLine("220 fixture ready"); err != nil {
					result <- err
					return
				}
				for _, step := range []struct{ command, reply string }{
					{"EHLO localhost", "250-fixture\r\n250 AUTH PLAIN"},
					{"AUTH PLAIN " + auth, "235 authenticated"},
					{"MAIL FROM:<" + sender + ">", "250 sender accepted"},
					{"RCPT TO:<" + recipient + ">", "250 recipient accepted"},
					{"DATA", "354 send message"},
				} {
					line, err := smtp.ReadLine()
					if err != nil || line != step.command {
						result <- fmt.Errorf("expected SMTP command %q (read error: %v)", strings.Fields(step.command)[0], err)
						return
					}
					if err := smtp.PrintfLine("%s", step.reply); err != nil {
						result <- err
						return
					}
				}
				raw, err := smtp.ReadDotBytes()
				if err != nil {
					result <- err
					return
				}
				message, err := mail.ReadMessage(bytes.NewReader(raw))
				if err != nil {
					result <- err
					return
				}
				from, fromErr := message.Header.AddressList("From")
				to, toErr := message.Header.AddressList("To")
				if fromErr != nil || len(from) != 1 || from[0].Address != sender ||
					toErr != nil || len(to) != 1 || to[0].Address != recipient ||
					message.Header.Get("Subject") != "Your Placement App admin invitation" {
					result <- errors.New("unexpected invitation headers")
					return
				}
				body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
				if err != nil || !strings.Contains(string(body), link) || !strings.Contains(string(body), "24 hours") {
					result <- errors.New("invitation body omitted the link or expiry")
					return
				}
				result <- smtp.PrintfLine("%s", test.reply)
			}()

			mailer := gmailInvitationMailer(server.Addr().String(), clientTLS, sender, appPassword)
			err = mailer(context.Background(), recipient, link)
			_ = server.Close()
			if fixtureErr := <-result; fixtureErr != nil {
				t.Fatalf("SMTP fixture: %v", fixtureErr)
			}
			if test.name == "accepted" && err != nil {
				t.Fatalf("send invitation: %v", err)
			}
			if test.name == "rejected" {
				var reply *textproto.Error
				if !errors.As(err, &reply) || reply.Code != 554 {
					t.Fatalf("expected SMTP rejection, got %v", err)
				}
			}
		})
	}
}

func smtpFixtureTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	const host = "smtp.example.invalid"
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	cert, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(cert)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	server := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{cert}, PrivateKey: privateKey}}}
	client := &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12}
	return server, client
}

func TestInvitationMailerConfiguration(t *testing.T) {
	t.Setenv("ADMIN_INVITE_GMAIL_EMAIL", "")
	t.Setenv("ADMIN_INVITE_GMAIL_APP_PASSWORD", "")
	if mailer, err := ReadInvitationMailer(); mailer != nil || err != nil {
		t.Fatalf("expected mail delivery disabled with no Gmail settings, got %v", err)
	}
	t.Setenv("ADMIN_INVITE_GMAIL_EMAIL", "invitations@example.invalid")
	if mailer, err := ReadInvitationMailer(); mailer != nil || err == nil {
		t.Fatal("expected an error for incomplete Gmail settings")
	}
	t.Setenv("ADMIN_INVITE_GMAIL_APP_PASSWORD", "abcd efgh ijkl mnop")
	if mailer, err := ReadInvitationMailer(); mailer == nil || err != nil {
		t.Fatalf("expected a configured Gmail sender, got %v", err)
	}
}
