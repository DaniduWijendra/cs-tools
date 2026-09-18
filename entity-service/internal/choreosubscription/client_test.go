// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package choreosubscription

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

// tokenPath is the OAuth2 token endpoint the test servers expose alongside the
// operation's own paths.
const tokenPath = "/oauth2/token"

// withToken serves the client-credentials token endpoint in front of h, so a
// test server can stand in for both the gateway and the operation.
func withToken(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
			return
		}
		h(w, r)
	}
}

// newTestClient builds a Client pointed at server, authenticating against the
// token endpoint withToken serves.
func newTestClient(t *testing.T, server *httptest.Server) Client {
	t.Helper()
	c, err := NewClient(Config{
		BaseURL: server.URL,
		Creds: ClientCredentialsConfig{
			TokenURL:     server.URL + tokenPath,
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// Every call to the operation must carry a bearer token — the gateway rejects
// an unauthenticated caller, and a client that silently omits the header fails
// only at provisioning time, against production.
func TestClient_SendsBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(withToken(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"status":2}}`))
	}))
	defer server.Close()

	if _, err := newTestClient(t, server).GetConsumptionStatus(
		context.Background(), "proj-1", ConsumptionStatusRequest{Email: "user@example.com"},
	); err != nil {
		t.Fatalf("GetConsumptionStatus failed: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
	}
}

// A missing credential must fail at construction rather than produce a client
// that sends unauthenticated requests.
func TestNewClient_RequiresCredentials(t *testing.T) {
	full := Config{
		BaseURL: "https://example.invalid",
		Creds: ClientCredentialsConfig{
			TokenURL:     "https://example.invalid/token",
			ClientID:     "id",
			ClientSecret: "secret",
		},
	}
	for name, mutate := range map[string]func(*Config){
		"no base URL":      func(c *Config) { c.BaseURL = "" },
		"no token URL":     func(c *Config) { c.Creds.TokenURL = "" },
		"no client ID":     func(c *Config) { c.Creds.ClientID = "" },
		"no client secret": func(c *Config) { c.Creds.ClientSecret = "" },
	} {
		cfg := full
		mutate(&cfg)
		if _, err := NewClient(cfg); err == nil {
			t.Errorf("%s: expected an error, got a usable client", name)
		}
	}
	if _, err := NewClient(full); err != nil {
		t.Errorf("a fully configured client must construct: %v", err)
	}
}

func TestClient_GetConsumptionStatus(t *testing.T) {
	projectUUID := "12345678-1234-1234-1234-123456789abc"
	expectedSysID := "12345678123412341234123456789abc"
	deploymentUUID := "87654321-4321-4321-4321-cba987654321"
	expectedDepSysID := "87654321432143214321cba987654321"

	server := httptest.NewServer(withToken(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/projects/" + expectedSysID + "/consumption/status"
		if r.URL.Path != expectedPath {
			t.Errorf("got path %s, want %s", r.URL.Path, expectedPath)
		}
		var req ConsumptionStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.DeploymentID != expectedDepSysID {
			t.Errorf("got deploymentID %s, want %s", req.DeploymentID, expectedDepSysID)
		}

		w.Header().Set("Content-Type", "application/json")
		// Test float status 2.0 to verify jsonInt unmarshaling
		w.Write([]byte(`{"result":{"status":2.0,"applicationId":"app-123","name":"Test App","description":"Test Desc"}}`))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	res, err := c.GetConsumptionStatus(context.Background(), projectUUID, ConsumptionStatusRequest{
		Email:        "user@example.com",
		DeploymentID: deploymentUUID,
	})
	if err != nil {
		t.Fatalf("GetConsumptionStatus failed: %v", err)
	}
	if int(res.Result.Status) != 2 {
		t.Errorf("got status %d, want 2", res.Result.Status)
	}
	if res.Result.ApplicationID == nil || *res.Result.ApplicationID != "app-123" {
		t.Errorf("unexpected applicationId: %v", res.Result.ApplicationID)
	}
}

func TestClient_GetDeploymentLicense(t *testing.T) {
	server := httptest.NewServer(withToken(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/license") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result": {
				"success": true,
				"license": {
					"subscriptionData": {
						"deploymentId": "dep-1",
						"deploymentName": "Prod",
						"subscriptionKey": "key-1",
						"clientId": "cid",
						"clientSecret": "sec",
						"secrets": "shuffled"
					},
					"signature": "sig123"
				}
			}
		}`))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	lic, err := c.GetDeploymentLicense(context.Background(), "proj-1", "dep-1", domain.DeploymentLicenseRequest{Email: "test@example.com"})
	if err != nil {
		t.Fatalf("GetDeploymentLicense failed: %v", err)
	}
	if lic.Signature != "sig123" {
		t.Errorf("got signature %s, want sig123", lic.Signature)
	}
	if lic.SubscriptionData.DeploymentName != "Prod" {
		t.Errorf("got deployment name %s, want Prod", lic.SubscriptionData.DeploymentName)
	}
}
