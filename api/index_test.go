package handler

import (
	"net/http/httptest"
	"testing"
)

func TestAPIRequest(t *testing.T) {
	tests := []struct{ input, want string }{
		{"/api/v1/auth/google/callback?state=sample&code=demo", "/api/v1/auth/google/callback?code=demo&state=sample"},
		{"/api/index?__placement_route=v1%2Fauth%2Fgoogle%2Fcallback&state=sample", "/api/v1/auth/google/callback?state=sample"},
		{"/api/index.go?__placement_route=v1%2Fadmin%2Fforms%2F42", "/api/v1/admin/forms/42"},
		{"/api/v1/forms?__placement_route=v1%2Fadmin%2Fforms", "/api/v1/forms"},
		{"/api/index?__placement_route=v1/forms&__placement_route=v1/admin/forms", ""},
		{"/api/index?__placement_route=v2/forms", ""},
		{"/api/index", ""},
		{"/forms", ""},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			original := httptest.NewRequest("GET", test.input, nil)
			r, ok := apiRequest(original)
			if test.want == "" {
				if ok {
					t.Fatal("unexpected API route")
				}
				return
			}
			if !ok || r.RequestURI != test.want {
				t.Fatalf("expected %q, got %v", test.want, r)
			}
			if original.RequestURI != test.input {
				t.Fatal("the incoming request was mutated")
			}
		})
	}
}
