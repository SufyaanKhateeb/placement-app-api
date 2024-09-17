package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func ParseJson(r *http.Request, payload any) error {
	if r.Body == nil {
		return fmt.Errorf("missing request body")
	}

	return json.NewDecoder(r.Body).Decode(payload)
}

func WriteJson(w http.ResponseWriter, status int, payload any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(payload)
}

func WriteJsonError(w http.ResponseWriter, status int, err error) error {
	return WriteJson(w, status, map[string]string{"error": err.Error()})
}

func WriteJwtToCookie(w http.ResponseWriter, key string, token string, expirationTime time.Duration) {
	cookie := &http.Cookie{
		Name:     key,
		Value:    token,
		HttpOnly: true,
		Expires:  time.Now().Add(expirationTime),
		Path:     "/",
		MaxAge:   int(expirationTime.Seconds()),
	}

	http.SetCookie(w, cookie)
}
