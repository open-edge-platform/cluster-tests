// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Constants for JWT configuration
const (
	KeyID     = "cluster-tests-key"
	IssuerURL = "http://platform-keycloak.orch-platform.svc/realms/master"
)

// runtime-generated keys
var (
	dynamicPrivateKey *rsa.PrivateKey
	dynamicPublicKey  *rsa.PublicKey
	keyGenerationOnce sync.Once
	keyGenerationErr  error
)

// keyFilePath returns the path where keys should be stored
func keyFilePath() string {
	return "/tmp/cluster-tests-dynamic-keys.pem"
}

// loadKeysFromFile attempts to load existing keys from file
func loadKeysFromFile() (*rsa.PrivateKey, error) {
	keyPath := keyFilePath()
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return nil, nil // File doesn't exist, will generate new keys
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return privateKey, nil
}

// saveKeysToFile saves the generated keys to file for reuse
func saveKeysToFile(privateKey *rsa.PrivateKey) error {
	keyPath := keyFilePath()
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	return os.WriteFile(keyPath, privateKeyPEM, 0600)
}

// generateRuntimeKeys creates a new RSA key pair at runtime or loads existing ones
func generateRuntimeKeys() {
	// First try to load existing keys
	if existingKey, err := loadKeysFromFile(); err == nil && existingKey != nil {
		dynamicPrivateKey = existingKey
		dynamicPublicKey = &existingKey.PublicKey
		return
	}

	// Generate new 2048-bit RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		keyGenerationErr = fmt.Errorf("failed to generate RSA key pair: %w", err)
		return
	}

	if saveErr := saveKeysToFile(privateKey); saveErr != nil {
		keyGenerationErr = fmt.Errorf("failed to save keys to file: %w", saveErr)
		return
	}

	dynamicPrivateKey = privateKey
	dynamicPublicKey = &privateKey.PublicKey
}

// getOrGenerateKeys ensures we have a key pair, generating it if needed
func getOrGenerateKeys() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	keyGenerationOnce.Do(generateRuntimeKeys)
	if keyGenerationErr != nil {
		return nil, nil, keyGenerationErr
	}
	return dynamicPrivateKey, dynamicPublicKey, nil
}

// encodeBase64URLBigInt encodes a big integer as a base64url string (for JWKS)
func encodeBase64URLBigInt(i *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(i.Bytes())
}

// GetPublicKeyPEM returns the public key in PEM format for OIDC mock server
func GetPublicKeyPEM() (string, error) {
	_, publicKey, err := getOrGenerateKeys()
	if err != nil {
		return "", err
	}

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	return string(pubKeyPEM), nil
}

// GetJWKS returns the public key in JWKS format for OIDC discovery
func GetJWKS() (string, error) {
	_, publicKey, err := getOrGenerateKeys()
	if err != nil {
		return "", err
	}

	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": KeyID,
				"alg": "PS512",
				"n":   encodeBase64URLBigInt(publicKey.N),
				"e":   encodeBase64URLBigInt(big.NewInt(int64(publicKey.E))),
			},
		},
	}

	jwksBytes, err := json.Marshal(jwks)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JWKS: %w", err)
	}

	return string(jwksBytes), nil
}

// TestJWTGenerator provides backward compatibility for tests
// This struct maintains the interface used by legacy test code while
// leveraging the new dynamic key generation system internally.
type TestJWTGenerator struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// createToken is a helper function to reduce code duplication in token generation
func (g *TestJWTGenerator) createToken(claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodPS512, claims)
	token.Header["kid"] = KeyID // Use constant instead of hardcoded value
	return token.SignedString(g.privateKey)
}

// NewTestJWTGenerator creates a new JWT generator with dynamic keys (backward compatibility)
func NewTestJWTGenerator() (*TestJWTGenerator, error) {
	// Generate unique keys for each generator instance (not shared)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key pair: %w", err)
	}

	return &TestJWTGenerator{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
	}, nil
}

// GenerateClusterManagerToken generates a token for cluster-manager (backward compatibility)
func (g *TestJWTGenerator) GenerateClusterManagerToken(subject, projectUUID string, expiry time.Duration) (string, error) {
	// Set issuer and audience to match unit test expectations
	now := time.Now()
	clusterNamespace := "53cd37b9-66b2-4cc8-b080-3722ed7af64a" // Default namespace from cluster_utils.go
	claims := jwt.MapClaims{
		"sub":   subject,
		"iss":   IssuerURL,
		"aud":   []string{"cluster-manager"},
		"scope": "openid email roles profile", // Match working JWT scope
		"exp":   now.Add(expiry).Unix(),
		"iat":   now.Unix(),
		"typ":   "Bearer",
		"azp":   "system-client",
		"realm_access": map[string]interface{}{ // Complete Keycloak-style roles structure
			"roles": []string{
				"account/view-profile",
				clusterNamespace + "_cl-tpl-r",
				clusterNamespace + "_cl-tpl-rw",
				"default-roles-master",
				clusterNamespace + "_im-r",
				clusterNamespace + "_reg-r",
				clusterNamespace + "_cat-r",
				clusterNamespace + "_alrt-r",
				clusterNamespace + "_tc-r",
				clusterNamespace + "_ao-rw",
				"offline_access",
				"uma_authorization",
				clusterNamespace + "_cl-r",
				clusterNamespace + "_cl-rw",
				"account/manage-account",
				"63764aaf-1527-46a0-b921-c5f32dba1ddb_" + clusterNamespace + "_m",
			},
		},
		"resource_access": map[string]interface{}{ // Resource-specific roles
			"cluster-manager": map[string]interface{}{
				"roles": []string{"admin", "manager"},
			},
		},
		"preferred_username": subject,
	}

	return g.createToken(claims)
}

// GenerateToken generates a general JWT token (backward compatibility)
func (g *TestJWTGenerator) GenerateToken(subject string, audience []string, customClaims map[string]interface{}) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": subject,
		"iss": IssuerURL,
		"aud": audience,
		"exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(),
		"typ": "Bearer",
	}

	// Add custom claims
	for k, v := range customClaims {
		claims[k] = v
	}

	return g.createToken(claims)
}

// GenerateShortLivedToken generates a token with short expiry (backward compatibility)
func (g *TestJWTGenerator) GenerateShortLivedToken(subject string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": subject,
		"iss": IssuerURL,
		"aud": []string{"cluster-manager"},
		"exp": now.Add(expiry).Unix(),
		"iat": now.Unix(),
		"typ": "Bearer",
	}

	return g.createToken(claims)
}

// ValidateToken validates a JWT token (backward compatibility)
func (g *TestJWTGenerator) ValidateToken(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSAPSS); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return g.publicKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// GetPublicKeyJWKS returns the public key in JWKS format (backward compatibility)
func (g *TestJWTGenerator) GetPublicKeyJWKS() (string, error) {
	return GetJWKS()
}

// GetPublicKeyPEM returns the public key in PEM format (backward compatibility)
func (g *TestJWTGenerator) GetPublicKeyPEM() (string, error) {
	return GetPublicKeyPEM()
}

// GetPrivateKeyPEM returns the private key in PEM format (backward compatibility)
func (g *TestJWTGenerator) GetPrivateKeyPEM() (string, error) {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(g.privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})
	return string(privateKeyPEM), nil
}

// SetupTestAuthentication creates authentication context for the given username
func SetupTestAuthentication(username string) (*TestAuthContext, error) {
	token, err := GenerateTestJWT(username)
	if err != nil {
		return nil, fmt.Errorf("failed to generate test JWT: %w", err)
	}

	return &TestAuthContext{
		Token:    token,
		Subject:  username,
		Issuer:   "cluster-tests",
		Audience: []string{"cluster-manager"},
	}, nil
}

// GenerateTestJWT creates a JWT token for testing with the given username using PS512
func GenerateTestJWT(username string) (string, error) {
	// Get the dynamically generated private key
	privateKey, _, err := getOrGenerateKeys()
	if err != nil {
		return "", fmt.Errorf("failed to get private key: %w", err)
	}

	// Set issuer and audience to match unit test expectations
	now := time.Now()
	clusterNamespace := "53cd37b9-66b2-4cc8-b080-3722ed7af64a" // Default namespace from cluster_utils.go
	claims := jwt.MapClaims{
		"sub":   username,
		"iss":   IssuerURL,                    // Use constant instead of hardcoded value
		"aud":   []string{"cluster-manager"},  // Unit tests expect this audience
		"scope": "openid email roles profile", // Match working JWT scope
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
		"typ":   "Bearer",        // Token type
		"azp":   "system-client", // Authorized party
		"realm_access": map[string]interface{}{
			"roles": []string{
				"account/view-profile",
				clusterNamespace + "_cl-tpl-r",
				clusterNamespace + "_cl-tpl-rw",
				"default-roles-master",
				clusterNamespace + "_im-r",
				clusterNamespace + "_reg-r",
				clusterNamespace + "_cat-r",
				clusterNamespace + "_alrt-r",
				clusterNamespace + "_tc-r",
				clusterNamespace + "_ao-rw",
				"offline_access",
				"uma_authorization",
				clusterNamespace + "_cl-r",
				clusterNamespace + "_cl-rw",
				"account/manage-account",
				"63764aaf-1527-46a0-b921-c5f32dba1ddb_" + clusterNamespace + "_m",
			},
		},
		"resource_access": map[string]interface{}{ // Resource-specific roles
			"cluster-manager": map[string]interface{}{
				"roles": []string{"admin", "manager"},
			},
		},
		"preferred_username": username,
	}

	// Create token using PS512 as required by cluster-manager v2.1.15
	token := jwt.NewWithClaims(jwt.SigningMethodPS512, claims)
	token.Header["kid"] = KeyID // Use constant instead of hardcoded value

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT token: %w", err)
	}

	return tokenString, nil
}

// GenerateOIDCMockConfig generates a Kubernetes YAML configuration for OIDC mock server
// with runtime-generated JWKS, replacing the bash script implementation
func GenerateOIDCMockConfig() (string, error) {
	// Generate dynamic JWKS using the same auth package as JWT tests
	jwks, err := GetJWKS()
	if err != nil {
		return "", fmt.Errorf("failed to generate JWKS: %w", err)
	}

	const template = `# SPDX-FileCopyrightText: (C) 2025 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

# Generated OIDC Mock Server Configuration (Dynamic Keys)
# This configuration provides a mock OIDC server with runtime-generated RSA keys

apiVersion: apps/v1
kind: Deployment
metadata:
  name: oidc-mock
  namespace: default
  labels:
    app: oidc-mock
spec:
  replicas: 1
  selector:
    matchLabels:
      app: oidc-mock
  template:
    metadata:
      labels:
        app: oidc-mock
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        ports:
        - containerPort: 80
        volumeMounts:
        - name: config
          mountPath: /etc/nginx/conf.d
        - name: content
          mountPath: /usr/share/nginx/html
      volumes:
      - name: config
        configMap:
          name: oidc-mock-nginx-config
      - name: content
        configMap:
          name: oidc-mock-content
---
apiVersion: v1
kind: Service
metadata:
  name: platform-keycloak
  namespace: orch-platform
spec:
  selector:
    app: oidc-mock
  ports:
  - port: 80
    targetPort: 80
    name: http
  type: ExternalName
  externalName: oidc-mock.default.svc.cluster.local
---
apiVersion: v1
kind: Service
metadata:
  name: oidc-mock
  namespace: default
spec:
  selector:
    app: oidc-mock
  ports:
  - port: 80
    targetPort: 80
    name: http
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: oidc-mock-nginx-config
  namespace: default
data:
  default.conf: |
    server {
        listen 80;
        server_name localhost;
        
        location /realms/master/.well-known/openid-configuration {
            return 200 '{
                "issuer": "` + IssuerURL + `",
                "authorization_endpoint": "` + IssuerURL + `/protocol/openid-connect/auth",
                "token_endpoint": "` + IssuerURL + `/protocol/openid-connect/token",
                "jwks_uri": "` + IssuerURL + `/keys",
                "userinfo_endpoint": "` + IssuerURL + `/protocol/openid-connect/userinfo",
                "response_types_supported": ["code", "token", "id_token", "code token", "code id_token", "token id_token", "code token id_token"],
                "subject_types_supported": ["public"],
                "id_token_signing_alg_values_supported": ["PS512", "RS256"]
            }';
            add_header Content-Type application/json;
        }
        
        location /realms/master/keys {
            return 200 '%s';
            add_header Content-Type application/json;
        }
        
        location / {
            return 200 'OIDC Mock Server (Dynamic Keys)\nAvailable endpoints:\n  /realms/master/.well-known/openid-configuration\n  /realms/master/keys\n';
        }
    }
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: oidc-mock-content
  namespace: default
data:
  jwks.json: |
    %s
  index.html: |
    <!DOCTYPE html>
    <html>
    <head><title>OIDC Mock Server (Dynamic Keys)</title></head>
    <body>
    <h1>OIDC Mock Server</h1>
    <p><strong>Using Runtime-Generated Keys</strong></p>
    <p>Available endpoints:</p>
    <ul>
    <li><a href="/realms/master/.well-known/openid-configuration">/.well-known/openid-configuration</a></li>
    <li><a href="/realms/master/keys">/keys</a></li>
    </ul>
    </body>
    </html>
`

	// Replace placeholders with actual JWKS
	config := fmt.Sprintf(template, jwks, jwks)

	return config, nil
}
