package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
)

func ParseJson(r *http.Request, payload any) error {
	if r.Body == nil {
		return fmt.Errorf("missing request body")
	}

	return json.NewDecoder(r.Body).Decode(payload)
}

func WriteJson(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(payload)
}

func WriteJsonError(w http.ResponseWriter, status int, err error) error {
	code := errorCodes[status]
	if code == "" {
		code = "request_failed"
	}
	return WriteAPIError(w, status, code, err, nil)
}

var errorCodes = map[int]string{
	http.StatusBadRequest:           "invalid_request",
	http.StatusUnauthorized:         "authentication_required",
	http.StatusForbidden:            "forbidden",
	http.StatusNotFound:             "not_found",
	http.StatusMethodNotAllowed:     "method_not_allowed",
	http.StatusConflict:             "conflict",
	http.StatusGone:                 "gone",
	http.StatusUnsupportedMediaType: "unsupported_media_type",
	http.StatusUnprocessableEntity:  "validation_failed",
	http.StatusTooManyRequests:      "rate_limited",
	http.StatusInternalServerError:  "internal_error",
	http.StatusBadGateway:           "bad_gateway",
	http.StatusServiceUnavailable:   "service_unavailable",
}

func WriteAPIError(w http.ResponseWriter, status int, code string, err error, fields map[string]string) error {
	return WriteJson(w, status, struct {
		Code   string            `json:"code"`
		Error  string            `json:"error"`
		Errors map[string]string `json:"errors,omitempty"`
	}{Code: code, Error: err.Error(), Errors: fields})
}

func WriteJwtToCookie(w http.ResponseWriter, key string, token string, expirationTime time.Duration) {
	cookie := &http.Cookie{
		Name:     key,
		Value:    token,
		HttpOnly: true,
		Secure:   strings.HasPrefix(config.Env.FrontendOrigin, "https://"),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(expirationTime),
		Path:     "/",
		MaxAge:   int(expirationTime.Seconds()),
	}

	if expirationTime <= 0 {
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
}

func AdminInvitationCookie(origin, token string, lifetime time.Duration) *http.Cookie {
	secure := strings.HasPrefix(origin, "https://")
	name := "placement_admin_invitation"
	if secure {
		name = "__Host-" + name
	}
	maxAge := int(lifetime.Seconds())
	if maxAge <= 0 {
		maxAge = -1
	}
	return &http.Cookie{
		Name: name, Value: token, Path: "/", MaxAge: maxAge,
		Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
}
