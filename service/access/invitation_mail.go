package access

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

const (
	gmailSMTPHost    = "smtp.gmail.com"
	gmailSMTPAddress = gmailSMTPHost + ":465"
	gmailMailTimeout = 8 * time.Second
)

type GmailSender func(ctx context.Context, recipient, subject, body string) error

func ReadGmailSender() (GmailSender, error) {
	email := strings.TrimSpace(os.Getenv("ADMIN_INVITE_GMAIL_EMAIL"))
	appPassword := strings.ReplaceAll(strings.TrimSpace(os.Getenv("ADMIN_INVITE_GMAIL_APP_PASSWORD")), " ", "")
	if email == "" && appPassword == "" {
		return nil, nil
	}
	if !ValidInvitationEmail(email) || len(appPassword) != 16 || strings.ContainsAny(appPassword, "\r\n\t") {
		return nil, errors.New("set ADMIN_INVITE_GMAIL_EMAIL to the sender address and ADMIN_INVITE_GMAIL_APP_PASSWORD to its 16-character Google App Password")
	}
	config := &tls.Config{ServerName: gmailSMTPHost, MinVersion: tls.VersionTLS12}
	return gmailSender(gmailSMTPAddress, config, email, appPassword), nil
}

func (send GmailSender) InvitationMailer() InvitationMailer {
	if send == nil {
		return nil
	}
	return func(ctx context.Context, email, link string) error {
		return send(ctx, email, "Your Placement App admin invitation",
			"You've been invited to join Placement App as an admin.\r\n\r\n"+
				"Open this link to sign in and accept the invitation. It expires in 24 hours.\r\n\r\n"+link+"\r\n")
	}
}

func (send GmailSender) SendVerificationCode(ctx context.Context, email, code string) error {
	if send == nil {
		return errors.New("Gmail delivery is unavailable")
	}
	return send(ctx, email, "Your Placement App verification code",
		"Your Placement App verification code is "+code+". It expires in 10 minutes.\r\n")
}

func gmailSender(address string, config *tls.Config, sender, appPassword string) GmailSender {
	dialer := &tls.Dialer{Config: config}
	auth := smtp.PlainAuth("", sender, appPassword, config.ServerName)
	from := &mail.Address{Name: "Placement App", Address: sender}
	return func(ctx context.Context, email, subject, body string) error {
		if !ValidInvitationEmail(email) {
			return errors.New("invalid mail recipient")
		}
		ctx, cancel := context.WithTimeout(ctx, gmailMailTimeout)
		defer cancel()
		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		defer connection.Close()
		deadline, _ := ctx.Deadline()
		if err := connection.SetDeadline(deadline); err != nil {
			return err
		}
		stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
		defer stop()
		client, err := smtp.NewClient(connection, config.ServerName)
		if err != nil {
			return err
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
		if err := client.Mail(sender); err != nil {
			return err
		}
		if err := client.Rcpt(email); err != nil {
			return err
		}
		data, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := io.WriteString(data, gmailMessage(from, email, subject, body)); err != nil {
			return err
		}
		// Closing DATA waits for Gmail's final acceptance of the message.
		return data.Close()
	}
}

func gmailMessage(from *mail.Address, email, subject, content string) string {
	var message strings.Builder
	message.WriteString(strings.Join([]string{
		"From: " + from.String(),
		"To: " + (&mail.Address{Address: email}).String(),
		"Subject: " + subject,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"",
	}, "\r\n"))
	body := quotedprintable.NewWriter(&message)
	// The string builder's writes cannot fail.
	_, _ = body.Write([]byte(content))
	_ = body.Close()
	return message.String()
}
