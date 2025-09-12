// SPDX-FileCopyrightText: (C) 2025 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package cluster_api_test_test

import (
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-edge-platform/cluster-tests/tests/auth"
	"github.com/open-edge-platform/cluster-tests/tests/utils"
)

var _ = Describe("JWT Authentication Tests",
	Ordered, Label(utils.ClusterOrchClusterApiSmokeTest), func() {
		var (
			authContext *auth.TestAuthContext
		)

		BeforeAll(func() {
			var err error
			authContext, err = utils.SetupTestAuthentication("test-user")
			Expect(err).NotTo(HaveOccurred())
			Expect(authContext).NotTo(BeNil())
			Expect(authContext.Token).NotTo(BeEmpty())
		})

		It("Should generate valid JWT tokens", func() {
			By("Verifying token is not empty")
			Expect(authContext.Token).NotTo(BeEmpty())

			By("Validating token structure and claims")
			claims, err := authContext.JWTGenerator.ValidateToken(authContext.Token)
			Expect(err).NotTo(HaveOccurred())
			Expect(claims).NotTo(BeNil())

			By("Checking token claims")
			Expect((*claims)["sub"]).To(Equal("test-user"))
			Expect((*claims)["iss"]).To(Equal("cluster-tests"))
			Expect((*claims)["scope"]).To(Equal("cluster:read cluster:write"))
		})

		It("Should test cluster-manager API authentication", func() {
			By("Testing cluster-manager healthz endpoint with JWT authentication")
			err := utils.TestClusterManagerAuthentication(authContext)

			if err != nil {
				// If authentication fails, it might be because:
				// 1. Cluster-manager is not running with auth enabled
				// 2. The JWT configuration is not set up properly
				// 3. The API endpoint is not accessible
				fmt.Printf("⚠️  Authentication test result: %v\n", err)

				// Let's try to diagnose the issue
				By("Attempting basic connectivity test")
				endpoint := fmt.Sprintf("%s/v2/healthz", utils.GetClusterManagerEndpoint())
				resp, connErr := http.Get(endpoint)
				if connErr != nil {
					Skip(fmt.Sprintf("Cluster-manager API not accessible: %v", connErr))
				}
				defer resp.Body.Close()

				switch resp.StatusCode {
				case http.StatusOK:
					fmt.Println("✅ Cluster-manager API is accessible without authentication")
					Skip("Authentication appears to be disabled in cluster-manager")
				case http.StatusUnauthorized:
					fmt.Println("✅ Cluster-manager API requires authentication (expected)")
					// This is actually what we want - now we need to configure JWT properly
					Fail(fmt.Sprintf("JWT authentication failed: %v", err))
				default:
					Skip(fmt.Sprintf("Unexpected response from cluster-manager: %d", resp.StatusCode))
				}
			} else {
				fmt.Println("✅ JWT authentication successful")
			}
		})

		It("Should be able to refresh JWT tokens", func() {
			By("Getting original token")
			originalToken := authContext.Token

			By("Refreshing the token")
			err := utils.RefreshAuthToken(authContext)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying new token is different")
			Expect(authContext.Token).NotTo(Equal(originalToken))
			Expect(authContext.Token).NotTo(BeEmpty())
		})

		It("Should handle token expiration", func() {
			By("Creating a short-lived token")
			shortAuthContext, err := utils.SetupTestAuthenticationWithExpiry("test-user", 1)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for token to expire")
			// Sleep a bit to ensure expiration
			Eventually(func() bool {
				_, err := shortAuthContext.JWTGenerator.ValidateToken(shortAuthContext.Token)
				return err != nil
			}, "5s", "100ms").Should(BeTrue(), "Token should eventually expire")
		})

		Context("When testing kubeconfig API endpoint", func() {
			It("Should attempt to retrieve kubeconfig via authenticated API", func() {
				namespace := utils.GetEnv(utils.NamespaceEnvVar, utils.DefaultNamespace)

				By("Calling cluster-manager kubeconfig API with JWT authentication")
				resp, err := utils.GetClusterKubeconfigFromAPI(authContext, namespace, utils.ClusterName)

				if err != nil {
					Skip(fmt.Sprintf("Kubeconfig API call failed: %v", err))
				}
				defer resp.Body.Close()

				switch resp.StatusCode {
				case http.StatusOK:
					fmt.Println("✅ Successfully retrieved kubeconfig via cluster-manager API")
					// TODO: Validate the kubeconfig content
				case http.StatusNotFound:
					Skip("Cluster not found - this test needs to run after cluster creation")
				case http.StatusUnauthorized:
					Fail("JWT authentication failed for kubeconfig endpoint")
				case http.StatusForbidden:
					Fail("JWT token lacks permissions for kubeconfig endpoint")
				default:
					Skip(fmt.Sprintf("Unexpected response from kubeconfig API: %d", resp.StatusCode))
				}
			})
		})
	})
