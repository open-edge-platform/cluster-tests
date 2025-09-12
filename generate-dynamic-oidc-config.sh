#!/bin/bash
# SPDX-FileCopyrightText: (C) 2025 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

# generate-dynamic-oidc-config.sh
# Generates OIDC mock configuration with runtime-generated keys

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_FILE="${1:-configs/oidc-mock-dynamic.yaml}"

echo "Generating dynamic OIDC mock configuration..."

# Generate JWKS using our dynamic key generator
TMP_GO_FILE="/tmp/jwks_gen_$$.go"
cat > "$TMP_GO_FILE" << 'GOEOF'
package main
import (
    "fmt"
    "github.com/open-edge-platform/cluster-tests/tests/auth"
)
func main() {
    jwks, err := auth.GetJWKS()
    if err != nil {
        panic(err)
    }
    fmt.Print(jwks)
}
GOEOF

JWKS=$(cd "$SCRIPT_DIR" && go run "$TMP_GO_FILE")
rm -f "$TMP_GO_FILE"

# Create the dynamic configuration
cat > "$OUTPUT_FILE" << EOF
apiVersion: v1
kind: Pod
metadata:
  name: oidc-mock
  namespace: default
  labels:
    app: oidc-mock
spec:
  containers:
  - name: oidc-mock
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
                "issuer": "http://platform-keycloak.orch-platform.svc/realms/master",
                "authorization_endpoint": "http://platform-keycloak.orch-platform.svc/realms/master/protocol/openid-connect/auth",
                "token_endpoint": "http://platform-keycloak.orch-platform.svc/realms/master/protocol/openid-connect/token",
                "jwks_uri": "http://platform-keycloak.orch-platform.svc/realms/master/keys",
                "userinfo_endpoint": "http://platform-keycloak.orch-platform.svc/realms/master/protocol/openid-connect/userinfo",
                "response_types_supported": ["code", "token", "id_token", "code token", "code id_token", "token id_token", "code token id_token"],
                "subject_types_supported": ["public"],
                "id_token_signing_alg_values_supported": ["PS512", "RS256"]
            }';
            add_header Content-Type application/json;
        }
        
        location /realms/master/keys {
            return 200 '$JWKS';
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
    $JWKS
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
EOF

# Replace the JWKS placeholder with actual generated JWKS
sed -i "s|\$JWKS|$JWKS|g" "$OUTPUT_FILE"

echo "Dynamic OIDC mock configuration generated: $OUTPUT_FILE"
echo "This configuration uses runtime-generated RSA keys and eliminates hardcoded secrets."
