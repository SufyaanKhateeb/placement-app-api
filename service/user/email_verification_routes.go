package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

var emailCodePattern = regexp.MustCompile("^[0-9]{6}$")
var errEmailDelivery = errors.New("could not send a verification code. Please try again later")

func (h *Handler) registerEmailVerificationRoutes(mux *http.ServeMux, prefix, origin string) {
	for path, handler := range map[string]http.HandlerFunc{
		"/auth/email-verification/confirm":                h.confirmEmail(false),
		"/auth/email-verification/resend":                 h.resendEmail(false),
		"/auth/email-verifications/current/confirmations": h.confirmEmail(true),
		"/auth/email-verifications/current/deliveries":    h.resendEmail(true),
	} {
		mux.HandleFunc("POST "+prefix+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if r.Header.Get("Origin") != origin {
				utils.WriteJsonError(w, http.StatusForbidden, errors.New("invalid request origin"))
				return
			}
			handler(w, r)
		})
	}
}

func (h *Handler) emailVerificationCookie(value string, maxAge int) *http.Cookie {
	name := "placement_email_verification"
	secure := strings.HasPrefix(h.frontendOrigin, "https://")
	if secure {
		name = "__Host-" + name
	}
	return &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

func (h *Handler) emailVerificationToken(r *http.Request) ([]byte, error) {
	cookie, err := r.Cookie(h.emailVerificationCookie("", -1).Name)
	if err != nil {
		return nil, errEmailChallengeExpired
	}
	token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(token) != 32 {
		return nil, errEmailChallengeExpired
	}
	return token, nil
}

func (h *Handler) startEmailVerification(w http.ResponseWriter, r *http.Request, challenge emailChallenge, canonical bool) {
	w.Header().Set("Cache-Control", "no-store")
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		writeEmailVerificationError(w, err)
		return
	}
	code, err := newEmailCode()
	if err != nil {
		writeEmailVerificationError(w, err)
		return
	}
	hash := sha256.Sum256(token)
	challenge.tokenHash, challenge.codeDigest = hash[:], codeDigest(token, code)
	recipient, err := h.Store.startEmailChallenge(r.Context(), challenge)
	if err != nil {
		writeEmailVerificationError(w, err)
		return
	}
	// Keep the pending browser grant so a delivery failure can be retried.
	http.SetCookie(w, h.emailVerificationCookie(base64.RawURLEncoding.EncodeToString(token), int(emailChallengeLifetime.Seconds())))
	h.deliverEmailCode(w, r, recipient, code, challenge.tokenHash, challenge.codeDigest, canonical)
}

func (h *Handler) resendEmail(canonical bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := h.emailVerificationToken(r)
		if err != nil {
			writeEmailVerificationError(w, err)
			return
		}
		code, err := newEmailCode()
		if err != nil {
			writeEmailVerificationError(w, err)
			return
		}
		hash := sha256.Sum256(token)
		digest := codeDigest(token, code)
		recipient, err := h.Store.resendEmailChallenge(r.Context(), hash[:], digest)
		if err != nil {
			writeEmailVerificationError(w, err)
			return
		}
		http.SetCookie(w, h.emailVerificationCookie(base64.RawURLEncoding.EncodeToString(token), int(emailChallengeLifetime.Seconds())))
		h.deliverEmailCode(w, r, recipient, code, hash[:], digest, canonical)
	}
}

func (h *Handler) deliverEmailCode(w http.ResponseWriter, r *http.Request, recipient, code string, hash, digest []byte, canonical bool) {
	if h.EmailCodeMailer != nil && h.EmailCodeMailer(r.Context(), recipient, code) == nil {
		utils.WriteJson(w, http.StatusAccepted, map[string]bool{"verificationRequired": true})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()
	if err := h.Store.invalidateEmailCode(ctx, hash, digest); err != nil {
		slog.Warn("could not invalidate undelivered email verification code")
	}
	slog.Warn("email verification delivery failed")
	field := "error"
	if canonical {
		field = "deliveryWarning"
	}
	utils.WriteJson(w, http.StatusAccepted, map[string]any{"verificationRequired": true, field: errEmailDelivery.Error()})
}

func (h *Handler) confirmEmail(canonical bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Code string `json:"code"`
		}
		if !readPayload(w, r, &payload) {
			return
		}
		if !emailCodePattern.MatchString(payload.Code) {
			writeEmailVerificationError(w, errEmailCodeInvalid)
			return
		}
		token, err := h.emailVerificationToken(r)
		if err != nil {
			writeEmailVerificationError(w, err)
			return
		}
		hash := sha256.Sum256(token)
		id, created, err := h.Store.confirmEmailChallenge(r.Context(), hash[:], codeDigest(token, payload.Code))
		if err != nil {
			writeEmailVerificationError(w, err)
			return
		}
		http.SetCookie(w, h.emailVerificationCookie("", -1))
		status := http.StatusOK
		if canonical {
			status = http.StatusNoContent
			if created {
				status = http.StatusCreated
			}
		}
		h.startSession(w, status, types.TokenInput{Id: id, Type: types.RoleUser})
	}
}

func writeEmailVerificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errEmailVerificationLimit):
		utils.WriteJsonError(w, http.StatusTooManyRequests, err)
	case errors.Is(err, errEmailChallengeExpired):
		utils.WriteAPIError(w, http.StatusGone, "verification_expired", err, nil)
	case errors.Is(err, errEmailCodeInvalid):
		utils.WriteAPIError(w, http.StatusBadRequest, "invalid_verification_code", err, nil)
	case errors.Is(err, errEmailUnavailable):
		utils.WriteAPIError(w, http.StatusConflict, "email_unavailable", err, nil)
	case errors.Is(err, access.ErrInvitationRequired):
		utils.WriteAPIError(w, http.StatusConflict, "invitation_required", err, nil)
	default:
		slog.Warn("email verification request failed")
		utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not complete email verification"))
	}
}
