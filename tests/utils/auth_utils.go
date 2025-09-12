// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/open-edge-platform/cluster-tests/tests/auth"
)

// SetupTestAuthentication initializes JWT generation and returns auth context
func SetupTestAuthentication(subject string) (*auth.TestAuthContext, error) {
	generator, err := auth.NewTestJWTGenerator()
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT generator: %w", err)
	}

	token, err := generator.GenerateClusterManagerToken(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &auth.TestAuthContext{
		JWTGenerator: generator,
		Token:        token,
		Subject:      subject,
		Issuer:       "cluster-tests",
		Audience:     []string{"cluster-manager"},
	}, nil
}

// RefreshAuthToken generates a new token with the same generator
func RefreshAuthToken(authContext *auth.TestAuthContext) error {
	token, err := authContext.JWTGenerator.GenerateClusterManagerToken(authContext.Subject)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	authContext.Token = token
	return nil
}

// SetupTestAuthenticationWithExpiry creates auth context with custom token expiry
func SetupTestAuthenticationWithExpiry(subject string, expiry time.Duration) (*auth.TestAuthContext, error) {
	generator, err := auth.NewTestJWTGenerator()
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT generator: %w", err)
	}

	token, err := generator.GenerateShortLivedToken(subject, expiry)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &auth.TestAuthContext{
		JWTGenerator: generator,
		Token:        token,
		Subject:      subject,
		Issuer:       "cluster-tests",
		Audience:     []string{"cluster-manager"},
	}, nil
}

// AuthenticatedHTTPClient creates an HTTP client with JWT authentication
func AuthenticatedHTTPClient(authContext *auth.TestAuthContext) *http.Client {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Add JWT token to requests
	originalTransport := client.Transport
	if originalTransport == nil {
		originalTransport = http.DefaultTransport
	}

	client.Transport = &AuthTransport{
		Transport: originalTransport,
		Token:     authContext.Token,
	}

	return client
}

// AuthTransport adds JWT authentication to HTTP requests
type AuthTransport struct {
	Transport http.RoundTripper
	Token     string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	clonedReq := req.Clone(req.Context())
	clonedReq.Header.Set("Authorization", "Bearer "+t.Token)
	clonedReq.Header.Set("Content-Type", "application/json")
	clonedReq.Header.Set("Accept", "application/json")

	return t.Transport.RoundTrip(clonedReq)
}

// CallClusterManagerAPI makes an authenticated API call to cluster-manager
func CallClusterManagerAPI(authContext *auth.TestAuthContext, method, endpoint string, body interface{}) (*http.Response, error) {
	client := AuthenticatedHTTPClient(authContext)

	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, endpoint, bodyReader)
	} else {
		req, err = http.NewRequest(method, endpoint, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return client.Do(req)
}

// GetClusterManagerEndpoint returns the cluster-manager API endpoint
func GetClusterManagerEndpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%s", PortForwardLocalPort)
}

// GetClusterKubeconfigFromAPI retrieves kubeconfig from cluster-manager API
func GetClusterKubeconfigFromAPI(authContext *auth.TestAuthContext, namespace, clusterName string) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/v2/clusters/%s/kubeconfigs", GetClusterManagerEndpoint(), clusterName)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add namespace header as used by cluster-manager
	req.Header.Set("X-Namespace", namespace)

	client := AuthenticatedHTTPClient(authContext)
	return client.Do(req)
}

// TestClusterManagerAuthentication tests if cluster-manager API accepts JWT authentication
func TestClusterManagerAuthentication(authContext *auth.TestAuthContext) error {
	endpoint := fmt.Sprintf("%s/v2/healthz", GetClusterManagerEndpoint())

	resp, err := CallClusterManagerAPI(authContext, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to call cluster-manager healthz endpoint: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println("✅ JWT authentication successful")
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("JWT authentication failed: token invalid or expired")
	case http.StatusForbidden:
		return fmt.Errorf("JWT valid but insufficient RBAC permissions")
	default:
		return fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}
}

// GetClusterInfoWithAuth retrieves cluster information using authenticated API call
func GetClusterInfoWithAuth(authContext *auth.TestAuthContext, namespace, clusterName string) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/v2/clusters/%s", GetClusterManagerEndpoint(), clusterName)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Namespace", namespace)

	client := AuthenticatedHTTPClient(authContext)
	return client.Do(req)
}
