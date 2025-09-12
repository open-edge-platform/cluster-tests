// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestJWTGenerator provides JWT token generation for testing purposes
type TestJWTGenerator struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// NewTestJWTGenerator creates a new JWT generator with a generated RSA key pair
func NewTestJWTGenerator() (*TestJWTGenerator, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	return &TestJWTGenerator{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
	}, nil
}

// GenerateClusterManagerToken generates a JWT token for cluster-manager API access
func (g *TestJWTGenerator) GenerateClusterManagerToken(subject string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":         subject,
		"aud":         []string{"cluster-manager", "cluster-manager-api"},
		"iss":         "cluster-tests",
		"iat":         now.Unix(),
		"exp":         now.Add(time.Hour).Unix(),
		"nbf":         now.Add(-time.Minute).Unix(), // Allow for clock skew
		"jti":         fmt.Sprintf("test-%d", now.Unix()),
		"scope":       "cluster:read cluster:write cluster:admin",
		"permissions": []string{"cluster:read", "cluster:write", "cluster:delete", "kubeconfig:read"},
		"groups":      []string{"system:authenticated", "cluster-admins"},
		"username":    subject,
		"user_id":     fmt.Sprintf("user-%s", subject),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Add key ID to header for better JWT validation
	token.Header["kid"] = "test-key-1"

	return token.SignedString(g.privateKey)
}

// GenerateToken generates a JWT token with custom claims
func (g *TestJWTGenerator) GenerateToken(subject string, audience []string, customClaims map[string]interface{}) (string, error) {
	claims := jwt.MapClaims{
		"sub": subject,
		"aud": audience,
		"iss": "cluster-tests",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	// Add custom claims
	for key, value := range customClaims {
		claims[key] = value
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(g.privateKey)
}

// ValidateToken validates a JWT token and returns the claims
func (g *TestJWTGenerator) ValidateToken(tokenString string) (*jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return g.publicKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to parse claims")
	}

	return &claims, nil
}

// GetPublicKeyPEM returns the public key in PEM format for cluster-manager configuration
func (g *TestJWTGenerator) GetPublicKeyPEM() ([]byte, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(g.publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return publicKeyPEM, nil
}

// GetPrivateKeyPEM returns the private key in PEM format (for testing purposes)
func (g *TestJWTGenerator) GetPrivateKeyPEM() ([]byte, error) {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(g.privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	return privateKeyPEM, nil
}

// GenerateShortLivedToken generates a token with a custom expiration time
func (g *TestJWTGenerator) GenerateShortLivedToken(subject string, duration time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub":   subject,
		"aud":   []string{"cluster-manager"},
		"iss":   "cluster-tests",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(duration).Unix(),
		"scope": "cluster:read cluster:write",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(g.privateKey)
}
