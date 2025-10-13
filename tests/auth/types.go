// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package auth

// ClusterManagerAuthConfig holds authentication configuration for cluster-manager
type ClusterManagerAuthConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey"`
	Issuer    string `json:"issuer"`
	Audience  string `json:"audience"`
}

// TestAuthContext holds authentication context for tests
type TestAuthContext struct {
	Token    string
	Subject  string
	Issuer   string
	Audience []string
}

// TokenClaims represents the structure of JWT claims used in tests
type TokenClaims struct {
	Subject   string   `json:"sub"`
	Audience  []string `json:"aud"`
	Issuer    string   `json:"iss"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	Scope     string   `json:"scope,omitempty"`
}

// ClusterAPICredentials holds credentials for cluster API access
type ClusterAPICredentials struct {
	Token     string
	Endpoint  string
	Namespace string
	TLSConfig *TLSConfig
}

// TLSConfig holds TLS configuration for API access
type TLSConfig struct {
	Insecure             bool
	CertificateData      []byte
	KeyData              []byte
	CertificateAuthority []byte
}
