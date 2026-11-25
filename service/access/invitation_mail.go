package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"
)

func ReadInvitationMailer() (InvitationMailer, error) {
	key := strings.TrimSpace(os.Getenv("ADMIN_INVITE_RESEND_API_KEY"))
	from := strings.TrimSpace(os.Getenv("ADMIN_INVITE_FROM"))
	if key == "" && from == "" {
		return nil, nil
	}
	if _, err := mail.ParseAddress(from); key == "" || err != nil {
		return nil, errors.New("set ADMIN_INVITE_RESEND_API_KEY and a valid ADMIN_INVITE_FROM together")
	}
	client := &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return func(ctx context.Context, email, link string) error {
		body, err := json.Marshal(map[string]string{
			"from": from, "to": email, "subject": "Your Placement App admin invitation",
			"text": "You've been invited to join Placement App as an admin.\n\n" +
				"Open this link to sign in and accept the invitation. It expires in 24 hours.\n\n" + link,
		})
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+key)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", "placement-app")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return errors.New("invitation email provider rejected the request")
		}
		return nil
	}, nil
}
