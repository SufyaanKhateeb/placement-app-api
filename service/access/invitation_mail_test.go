package access

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"
)

func TestGmailInvitationMailer(t *testing.T) {
	const (
		fromAddress = "invitations@example.invalid"
		recipient   = "invitee@example.invalid"
		link        = "https://placement.example.invalid/admin/invitation#fixture-token"
		sendPath    = "/gmail/v1/users/me/messages/send"
	)
	var tokenRequests, sendRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequests.Add(1)
			if err := r.ParseForm(); err != nil || r.Method != http.MethodPost ||
				r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "test-refresh" ||
				r.Form.Get("client_id") != "test-client" || r.Form.Get("client_secret") != "test-secret" {
				t.Error("unexpected OAuth refresh request")
				http.Error(w, "invalid OAuth fixture request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"test-access","token_type":"Bearer","expires_in":3600}`)
		case sendPath:
			attempt := sendRequests.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-access" ||
				r.Header.Get("Content-Type") != "application/json" {
				t.Error("unexpected Gmail request headers")
			}
			var payload struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode Gmail payload: %v", err)
				http.Error(w, "invalid fixture message", http.StatusBadRequest)
				return
			}
			raw, err := base64.RawURLEncoding.Strict().DecodeString(payload.Raw)
			if err != nil {
				t.Errorf("decode Gmail message: %v", err)
				http.Error(w, "invalid fixture encoding", http.StatusBadRequest)
				return
			}
			message, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Errorf("parse invitation email: %v", err)
				http.Error(w, "invalid fixture message", http.StatusBadRequest)
				return
			}
			from, fromErr := message.Header.AddressList("From")
			to, toErr := message.Header.AddressList("To")
			if fromErr != nil || len(from) != 1 || from[0].Address != fromAddress ||
				toErr != nil || len(to) != 1 || to[0].Address != recipient ||
				message.Header.Get("Subject") != "Your Placement App admin invitation" {
				t.Error("incorrect invitation sender, recipient, or subject")
			}
			text, err := io.ReadAll(quotedprintable.NewReader(message.Body))
			if err != nil || !strings.Contains(string(text), link) || !strings.Contains(string(text), "24 hours") {
				t.Error("invitation body omitted the link or expiry")
			}
			switch attempt {
			case 2:
				http.Error(w, "private upstream details", http.StatusForbidden)
			case 3:
				http.Redirect(w, r, "/unexpected", http.StatusTemporaryRedirect)
			default:
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"id":"fixture-message"}`)
			}
		default:
			t.Errorf("unexpected OAuth or Gmail path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := oauth2.Config{
		ClientID: "test-client", ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{TokenURL: server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams},
	}
	mailer := gmailInvitationMailer(config, "test-refresh", &mail.Address{Name: "Placement App", Address: fromAddress}, server.URL+sendPath)
	if err := mailer(context.Background(), recipient, link); err != nil {
		t.Fatalf("send invitation: %v", err)
	}
	if err := mailer(context.Background(), recipient, link); err == nil ||
		!strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "private upstream details") {
		t.Fatalf("expected a sanitized Gmail rejection, got %v", err)
	}
	if err := mailer(context.Background(), recipient, link); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("expected a stopped redirect, got %v", err)
	}
	if tokenRequests.Load() != 1 || sendRequests.Load() != 3 {
		t.Fatalf("expected one token refresh and three send attempts, got %d and %d", tokenRequests.Load(), sendRequests.Load())
	}
}

func TestInvitationMailerConfiguration(t *testing.T) {
	for _, key := range []string{"ADMIN_INVITE_GMAIL_CLIENT_ID", "ADMIN_INVITE_GMAIL_CLIENT_SECRET", "ADMIN_INVITE_GMAIL_REFRESH_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("ADMIN_INVITE_FROM", "Placement App <invitations@example.invalid>")
	mailer, err := ReadInvitationMailer()
	if mailer != nil || err != nil {
		t.Fatalf("expected mail delivery disabled with no Gmail settings, got %v", err)
	}
	t.Setenv("ADMIN_INVITE_GMAIL_CLIENT_ID", "fixture-client")
	if mailer, err := ReadInvitationMailer(); mailer != nil || err == nil {
		t.Fatal("expected an error for incomplete Gmail settings")
	}
	t.Setenv("ADMIN_INVITE_GMAIL_CLIENT_SECRET", "fixture-secret")
	t.Setenv("ADMIN_INVITE_GMAIL_REFRESH_TOKEN", "fixture-refresh")
	if mailer, err := ReadInvitationMailer(); mailer == nil || err != nil {
		t.Fatalf("expected a configured Gmail sender, got %v", err)
	}
}
