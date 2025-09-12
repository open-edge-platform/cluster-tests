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
	tokenString, err := generator.GenerateClusterManagerToken(subject)
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
	if (*claims)["sub"] != subject {
		t.Errorf("Expected subject %s, got %s", subject, (*claims)["sub"])
	}

	if (*claims)["iss"] != "cluster-tests" {
		t.Errorf("Expected issuer 'cluster-tests', got %s", (*claims)["iss"])
	}

	if (*claims)["scope"] != "cluster:read cluster:write cluster:admin" {
		t.Errorf("Expected scope 'cluster:read cluster:write cluster:admin', got %s", (*claims)["scope"])
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
	if (*claims)["role"] != "admin" {
		t.Errorf("Expected role 'admin', got %s", (*claims)["role"])
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

	// Test with invalid token string
	_, err = generator.ValidateToken("invalid.token.string")
	if err == nil {
		t.Error("Expected validation to fail for invalid token")
	}

	// Test with malformed JWT
	_, err = generator.ValidateToken("header.payload.signature")
	if err == nil {
		t.Error("Expected validation to fail for malformed JWT")
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
	tokenString, err := generator1.GenerateClusterManagerToken("test-user")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Try to validate with second generator (different key)
	_, err = generator2.ValidateToken(tokenString)
	if err == nil {
		t.Error("Expected validation to fail when using different key")
	}
}
