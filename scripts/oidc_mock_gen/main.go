// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// oidc_mock_gen is a tiny helper for vEN/bootstrap scripts.
// It prints a Kubernetes manifest that stands up an OIDC discovery + JWKS endpoint
// compatible with the issuer used by cluster-tests (platform-keycloak.orch-platform.svc).
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/open-edge-platform/cluster-tests/tests/auth"
)

func main() {
	mode := flag.String("mode", "manifest", "Output mode: manifest|token")
	subject := flag.String("subject", "cluster-agent", "JWT subject (token mode)")
	aud := flag.String("aud", "cluster-management-client", "JWT audience (token mode). Comma-separated.")
	azp := flag.String("azp", "cluster-management-client", "JWT azp/authorized party (token mode)")
	flag.Parse()

	switch *mode {
	case "manifest":
		m, err := auth.GenerateOIDCMockConfig()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(m)
	case "token":
		var audience []string
		for _, a := range strings.Split(*aud, ",") {
			if v := strings.TrimSpace(a); v != "" {
				audience = append(audience, v)
			}
		}
		if len(audience) == 0 {
			log.Fatal("-aud must not be empty")
		}
		t, err := auth.GenerateTestJWTForClient(*subject, audience, *azp)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(t)
	default:
		log.Fatalf("unknown -mode %q (expected manifest|token)", *mode)
	}
}
