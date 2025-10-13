// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
	"time"
)

func TestNewTestJWTGenerator(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	if generator.privateKey == nil {
		t.Error("Private key should not be nil")
	}

	if generator.publicKey == nil {
		t.Error("Public key should not be nil")
	}
}

func TestGenerateClusterManagerToken(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	subject := "test-user"
	projectUUID := "test-project-123"
	tokenString, err := generator.GenerateClusterManagerToken(subject, projectUUID, time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if tokenString == "" {
		t.Error("Token string should not be empty")
	}

	// Validate the token
	claims, err := generator.ValidateToken(tokenString)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	// Check claims
	if claims["sub"] != subject {
		t.Errorf("Expected subject %s, got %s", subject, claims["sub"])
	}

	if claims["iss"] != IssuerURL {
		t.Errorf("Expected issuer %s, got %s", IssuerURL, claims["iss"])
	}
}

func TestGenerateTokenWithCustomClaims(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	subject := "test-user"
	audience := []string{"test-service"}
	customClaims := map[string]interface{}{
		"role":        "admin",
		"permissions": []string{"read", "write"},
	}

	tokenString, err := generator.GenerateToken(subject, audience, customClaims)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Validate the token
	claims, err := generator.ValidateToken(tokenString)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	// Check custom claims
	if claims["role"] != "admin" {
		t.Errorf("Expected role 'admin', got %s", claims["role"])
	}

	// Check permissions claim
	perms, ok := claims["permissions"].([]interface{})
	if !ok {
		t.Errorf("Expected permissions to be []interface{}, got %T", claims["permissions"])
	} else if len(perms) != 2 || perms[0] != "read" || perms[1] != "write" {
		t.Errorf("Expected permissions ['read', 'write'], got %v", claims["permissions"])
	}
}

func TestTokenExpiration(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	// Generate a token that expires in 1 millisecond
	tokenString, err := generator.GenerateShortLivedToken("test-user", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	// Try to validate expired token
	_, err = generator.ValidateToken(tokenString)
	if err == nil {
		t.Error("Expected validation to fail for expired token")
	}
}

func TestGetPublicKeyPEM(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	publicKeyPEM, err := generator.GetPublicKeyPEM()
	if err != nil {
		t.Fatalf("Failed to get public key PEM: %v", err)
	}

	if len(publicKeyPEM) == 0 {
		t.Error("Public key PEM should not be empty")
	}

	// Check that it's valid PEM format
	if string(publicKeyPEM[:11]) != "-----BEGIN " {
		t.Error("Public key PEM should start with '-----BEGIN '")
	}
}

func TestGetPrivateKeyPEM(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	privateKeyPEM, err := generator.GetPrivateKeyPEM()
	if err != nil {
		t.Fatalf("Failed to get private key PEM: %v", err)
	}

	if len(privateKeyPEM) == 0 {
		t.Error("Private key PEM should not be empty")
	}

	// Check that it's valid PEM format
	if string(privateKeyPEM[:11]) != "-----BEGIN " {
		t.Error("Private key PEM should start with '-----BEGIN '")
	}
}

func TestInvalidTokenValidation(t *testing.T) {
	generator, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create JWT generator: %v", err)
	}

	// Test cases for invalid tokens
	testCases := []struct {
		name  string
		token string
	}{
		{"invalid token string", "invalid.token.string"},
		{"malformed JWT", "header.payload.signature"},
		{"empty token", ""},
		{"incomplete JWT", "header.payload"},
		{"random string", "not-a-jwt-at-all"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generator.ValidateToken(tc.token)
			if err == nil {
				t.Errorf("Expected validation to fail for %s: %q", tc.name, tc.token)
			}
		})
	}
}

func TestTokenSignedWithDifferentKey(t *testing.T) {
	generator1, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create first JWT generator: %v", err)
	}

	generator2, err := NewTestJWTGenerator()
	if err != nil {
		t.Fatalf("Failed to create second JWT generator: %v", err)
	}

	// Generate token with first generator
	tokenString, err := generator1.GenerateClusterManagerToken("test-user", "test-project", time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Try to validate with second generator (different key)
	_, err = generator2.ValidateToken(tokenString)
	if err == nil {
		t.Error("Expected validation to fail when using different key")
	}
}
