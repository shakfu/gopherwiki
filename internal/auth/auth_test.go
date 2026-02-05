package auth

import (
	"testing"
)

func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if hash == "" {
		t.Error("HashPassword returned empty hash")
	}

	if hash == password {
		t.Error("Hash should not equal plaintext password")
	}
}

func TestCheckPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Correct password should match
	if !CheckPassword(password, hash) {
		t.Error("CheckPassword should return true for correct password")
	}

	// Wrong password should not match
	if CheckPassword("wrongpassword", hash) {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestGenerateToken(t *testing.T) {
	token1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token1 == "" {
		t.Error("GenerateToken returned empty token")
	}

	// Tokens should be unique
	token2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token1 == token2 {
		t.Error("GenerateToken should generate unique tokens")
	}

	// Token should be reasonably long (base64 of 32 bytes)
	if len(token1) < 40 {
		t.Errorf("Token length = %d, expected at least 40", len(token1))
	}
}

func TestPasswordHashing_DifferentHashesForSamePassword(t *testing.T) {
	password := "samepassword"

	hash1, _ := HashPassword(password)
	hash2, _ := HashPassword(password)

	// bcrypt should generate different hashes for same password (due to salt)
	if hash1 == hash2 {
		t.Error("Same password should produce different hashes due to salting")
	}

	// Both hashes should still validate
	if !CheckPassword(password, hash1) {
		t.Error("First hash should validate")
	}
	if !CheckPassword(password, hash2) {
		t.Error("Second hash should validate")
	}
}

func TestCheckPassword_EmptyInputs(t *testing.T) {
	hash, _ := HashPassword("password")

	// Empty password should not match
	if CheckPassword("", hash) {
		t.Error("Empty password should not match")
	}

	// Empty hash should not match (and should not panic)
	if CheckPassword("password", "") {
		t.Error("Empty hash should not match")
	}
}
