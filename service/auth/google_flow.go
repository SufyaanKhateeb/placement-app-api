package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

func (g *GoogleSignIn) signedFlow(state, verifier, nonce, mode string, adminID int) string {
	expires := strconv.FormatInt(time.Now().Add(googleFlowLifetime).Unix(), 10)
	data := strings.Join([]string{state, verifier, nonce, mode, strconv.Itoa(adminID), expires}, ".")
	return data + "." + base64.RawURLEncoding.EncodeToString(g.flowMAC(data))
}

func (g *GoogleSignIn) verifyFlow(cookie string) []string {
	last := strings.LastIndexByte(cookie, '.')
	if last < 0 {
		return nil
	}
	data, signature := cookie[:last], cookie[last+1:]
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, g.flowMAC(data)) {
		return nil
	}
	fields := strings.Split(data, ".")
	if len(fields) != 6 {
		return nil
	}
	expiration, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil || time.Now().Unix() >= expiration {
		return nil
	}
	return fields
}

func (g *GoogleSignIn) flowMAC(flow string) []byte {
	mac := hmac.New(sha256.New, []byte(g.client.ClientSecret))
	mac.Write([]byte("placement/google-flow/v1:"))
	mac.Write([]byte(flow))
	return mac.Sum(nil)
}
