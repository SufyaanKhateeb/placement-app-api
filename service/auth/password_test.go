package auth

import "testing"

func TestHashPassword(t *testing.T) {
	originalPass := "password"
	hash, err := HashPassword(originalPass)
	if err != nil {
		t.Errorf("error hashing password: %v", err)
	}

	if hash == "" {
		t.Error("expected hash to be not empty")
	}

	if hash == originalPass {
		t.Error("expected hash to be different from password")
	}
}

func TestCompareHashAndPassword(t *testing.T) {
	originalPass := "password"
	hash, err := HashPassword(originalPass)
	if err != nil {
		t.Errorf("error hashing password: %v", err)
	}

	if err := CompareHashAndPassword(originalPass, hash); err != nil {
		t.Error("expected password to match hash")
	}

	if err := CompareHashAndPassword("wrongPass", hash); err == nil {
		t.Error("expected hash to be different from password")
	}
}
