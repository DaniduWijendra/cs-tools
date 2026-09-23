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
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/config"
)

const firstAccessPath = "/users/me/first-access"

// newFirstAccessRouter builds a Postgres-mode router with the first-access
// flag set as given. The pool points at an address nothing listens on: pgx
// connects lazily, so this is a usable non-nil *pgxpool.Pool that never needs
// a database -- and the two cases asserted below (an unregistered route, and
// an anonymous caller refused before any query) never reach one.
func newFirstAccessRouter(t *testing.T, firstAccessEnabled bool) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://entity:entity@127.0.0.1:1/entity?sslmode=disable")
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := &config.Config{
		DataSource:                        config.DataSourcePostgres,
		SalesEntityBaseURL:                "https://example.invalid",
		SalesEntityTokenURL:               "https://example.invalid/oauth2/token",
		SalesEntityClientID:               "test-client",
		SalesEntityClientSecret:           "test-secret",
		SalesforceMembershipIngestEnabled: true,
		CSMMigrationFirstAccessEnabled:    firstAccessEnabled,
	}
	withTestAuth(t, cfg)
	router, _ := NewRouter(pool, cfg)
	return router
}

// CSM_MIGRATION_FIRST_ACCESS_ENABLED off must leave the route unregistered
// entirely -- not a handler that refuses, a 404 -- so nothing on this path can
// reach Salesforce at all while the flag is off.
func TestFirstAccessRouteIsAbsentWhenTheFlagIsOff(t *testing.T) {
	router := newFirstAccessRouter(t, false)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, firstAccessPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 with the flag off", rec.Code)
	}
}

// With the flag on the route exists, and a caller carrying no end-user token
// is refused (401) before the service looks anything up.
func TestFirstAccessRejectsAnAnonymousCaller(t *testing.T) {
	router := newFirstAccessRouter(t, true)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, firstAccessPath, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a caller with no x-user-id-token", rec.Code)
	}
}
