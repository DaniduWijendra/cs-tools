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

package scim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/cs-tools/integrations/csm-notification-service/internal/apierror"
)

func newTestClient(t *testing.T, tokenSrv, apiSrv *httptest.Server) *Client {
	t.Helper()
	return NewClient(Config{
		BaseURL:      apiSrv.URL,
		TokenURL:     tokenSrv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scopes:       []string{"scim"},
	})
}

func newTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
}

// TestEnsureExternalUser_Created: a 201 is a freshly created user
// (Existed=false), and the request carries exactly the documented body —
// userName/givenName/familyName/emails, and no password or askPassword.
func TestEnsureExternalUser_Created(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/organizations/external/users" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "u-1", "userName": "jane@acme.com", "existed": false})
	}))
	defer apiSrv.Close()
	tokenSrv := newTokenServer(t)
	defer tokenSrv.Close()

	got, err := newTestClient(t, tokenSrv, apiSrv).EnsureExternalUser(context.Background(), "jane@acme.com", "Jane", "Doe")
	if err != nil {
		t.Fatalf("EnsureExternalUser() error = %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want the OAuth2 bearer token", gotAuth)
	}
	if gotBody["userName"] != "jane@acme.com" || gotBody["givenName"] != "Jane" || gotBody["familyName"] != "Doe" {
		t.Errorf("request body = %v, want userName/givenName/familyName set", gotBody)
	}
	if emails, ok := gotBody["emails"].([]any); !ok || len(emails) != 1 || emails[0] != "jane@acme.com" {
		t.Errorf("emails = %v, want [jane@acme.com]", gotBody["emails"])
	}
	for _, forbidden := range []string{"password", "askPassword"} {
		if _, present := gotBody[forbidden]; present {
			t.Errorf("request body carries %q — the SCIM service generates the password itself", forbidden)
		}
	}
	if got.ID != "u-1" || got.UserName != "jane@acme.com" || got.Existed {
		t.Errorf("got %+v, want id u-1, userName jane@acme.com, Existed=false", got)
	}
}

// TestEnsureExternalUser_AlreadyExisted: a 200 is an already-existing user
// (Existed=true) — the status decides, even if the body omitted "existed".
func TestEnsureExternalUser_AlreadyExisted(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "u-1", "userName": "jane@acme.com"})
	}))
	defer apiSrv.Close()
	tokenSrv := newTokenServer(t)
	defer tokenSrv.Close()

	got, err := newTestClient(t, tokenSrv, apiSrv).EnsureExternalUser(context.Background(), "jane@acme.com", "", "")
	if err != nil {
		t.Fatalf("EnsureExternalUser() error = %v", err)
	}
	if !got.Existed || got.ID != "u-1" {
		t.Errorf("got %+v, want Existed=true, id u-1", got)
	}
}

// TestEnsureExternalUser_UpstreamError: anything but 200/201 is an
// *apierror.Error carrying the status and a body excerpt, so the caller
// can record it as the step's lastError and let the consumer retry.
func TestEnsureExternalUser_UpstreamError(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"asgardeo unavailable"}`))
	}))
	defer apiSrv.Close()
	tokenSrv := newTokenServer(t)
	defer tokenSrv.Close()

	_, err := newTestClient(t, tokenSrv, apiSrv).EnsureExternalUser(context.Background(), "jane@acme.com", "Jane", "Doe")
	var apiErr *apierror.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("EnsureExternalUser() error = %v, want *apierror.Error", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError || apiErr.Body != `{"error":"asgardeo unavailable"}` {
		t.Errorf("got %+v, want status 500 and the body excerpt", apiErr)
	}
}

// TestEnsureExternalUser_RejectsEmptyEmail: nothing is sent upstream for an
// empty email — there is no userName to create.
func TestEnsureExternalUser_RejectsEmptyEmail(t *testing.T) {
	called := false
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer apiSrv.Close()
	tokenSrv := newTokenServer(t)
	defer tokenSrv.Close()

	if _, err := newTestClient(t, tokenSrv, apiSrv).EnsureExternalUser(context.Background(), "", "Jane", "Doe"); err == nil {
		t.Fatal("expected an error for an empty email, got nil")
	}
	if called {
		t.Error("upstream should not have been called for an empty email")
	}
}
