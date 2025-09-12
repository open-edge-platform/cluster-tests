#!/bin/bash
# SPDX-FileCopyrightText: (C) 2025 Intel Corporation
# SPDX-License-Identifier: Apache-2.0

# Generate a minimal 10-minute JWT token for cluster-agent authentication
# This script creates a JWT token that expires in 10 minutes to avoid GitLeaks detection
set -e

header='{"alg":"HS256","typ":"JWT"}'
current_time=$(date +%s)
expiration_time=$((current_time + 600))  # 10 minutes from now
payload=$(cat <<EOF
{
  "exp": ${expiration_time},
  "iss": "https://keycloak.kind.internal/realms/master",
  "realm_access": {
    "roles": [
      "clusters-write-role",
      "clusters-read-role",
      "node-agent-readwrite-role",
      "cluster-agent"
    ]
  },
  "sub": "cluster-agent",
  "typ": "Bearer"
}
EOF
)

# Base64 URL encode function
base64url_encode() {
    base64 -w 0 | tr '+/' '-_' | tr -d '='
}

# Encode header and payload
encoded_header=$(echo -n "$header" | base64url_encode)
encoded_payload=$(echo -n "$payload" | base64url_encode)

# Create signature
signature_input="${encoded_header}.${encoded_payload}"
# Use a non-sensitive test secret (GitLeaks safe)
secret="test-secret-for-development-only"
signature=$(echo -n "$signature_input" | openssl dgst -sha256 -hmac "$secret" -binary | base64url_encode)

# Create final JWT
jwt="${encoded_header}.${encoded_payload}.${signature}"

# Output the token
echo "$jwt"
