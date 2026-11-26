package access

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const gmailSendURL = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"
const invitationMailTimeout = 8 * time.Second

func ReadInvitationMailer() (InvitationMailer, error) {
	clientID := strings.TrimSpace(os.Getenv("ADMIN_INVITE_GMAIL_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("ADMIN_INVITE_GMAIL_CLIENT_SECRET"))
	refreshToken := strings.TrimSpace(os.Getenv("ADMIN_INVITE_GMAIL_REFRESH_TOKEN"))
	if clientID == "" && clientSecret == "" && refreshToken == "" {
		return nil, nil
	}
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		return nil, errors.New("set ADMIN_INVITE_GMAIL_CLIENT_ID, ADMIN_INVITE_GMAIL_CLIENT_SECRET, and ADMIN_INVITE_GMAIL_REFRESH_TOKEN together")
	}
	fromValue := strings.TrimSpace(os.Getenv("ADMIN_INVITE_FROM"))
	from, err := mail.ParseAddress(fromValue)
	if err != nil || strings.ContainsAny(fromValue, "\r\n") {
		return nil, errors.New("ADMIN_INVITE_FROM must contain a valid sender address")
	}
	config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  "https://oauth2.googleapis.com/token",
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	return gmailInvitationMailer(config, refreshToken, from, gmailSendURL), nil
}

func gmailInvitationMailer(config oauth2.Config, refreshToken string, from *mail.Address, sendURL string) InvitationMailer {
	baseClient := &http.Client{
		Timeout:       invitationMailTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// A warm API instance can reuse the mailbox access token across requests.
	oauthContext := context.WithValue(context.Background(), oauth2.HTTPClient, baseClient)
	client := config.Client(oauthContext, &oauth2.Token{RefreshToken: refreshToken})
	return func(ctx context.Context, email, link string) error {
		if !ValidInvitationEmail(email) {
			return errors.New("invalid invitation recipient")
		}
		message := invitationMessage(from, email, link)
		body, err := json.Marshal(struct {
			Raw string `json:"raw"`
		}{Raw: base64.RawURLEncoding.EncodeToString([]byte(message))})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, invitationMailTimeout)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("Gmail rejected the invitation email (HTTP %d)", response.StatusCode)
		}
		return nil
	}
}

func invitationMessage(from *mail.Address, email, link string) string {
	var message strings.Builder
	message.WriteString(strings.Join([]string{
		"From: " + from.String(),
		"To: " + (&mail.Address{Address: email}).String(),
		"Subject: Your Placement App admin invitation",
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"",
	}, "\r\n"))
	body := quotedprintable.NewWriter(&message)
	// The string builder's writes cannot fail.
	_, _ = body.Write([]byte("You've been invited to join Placement App as an admin.\r\n\r\n" +
		"Open this link to sign in and accept the invitation. It expires in 24 hours.\r\n\r\n" + link + "\r\n"))
	_ = body.Close()
	return message.String()
}
