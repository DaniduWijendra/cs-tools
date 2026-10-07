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

// CountOnly on the four searches the dashboards use (cases, incidents, change
// requests, problems): only the COUNT runs, the page query is not sent, the
// total is exactly the one the same search reports without the flag, and the
// list comes back as an empty array rather than null. Run for an internal
// caller, a customer with no projects and a project member, with and without
// a filter, so the row-level-security paths a dashboard takes are all covered.
// Runs the real repositories on a real database and records the statements
// they send. Skipped without CASE_STATS_TEST_DSN.
//
//	CASE_STATS_TEST_DSN=postgres://... go test ./internal/repository/ -run SearchCountOnly

package repository_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

// statements returns every statement sent since the last reset (the
// identity-setting ones Scoped queues are already left out).
func (r *statementRecorder) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sqls...)
}

// statementHeads shortens each statement to its first words so a failure reads
// as "which statements ran", not as pages of SQL.
func statementHeads(stmts []string) []string {
	out := make([]string, len(stmts))
	for i, s := range stmts {
		s = strings.Join(strings.Fields(s), " ")
		if len(s) > 48 {
			s = s[:48] + "..."
		}
		out[i] = s
	}
	return out
}

type countOnlySearch struct {
	name string
	// run performs the search; filtered adds one narrowing filter so the count
	// goes through the same WHERE builder as the page does.
	run func(scope repository.SearchScope, countOnly, filtered bool) (page any, total int, err error)
}

func countOnlySearches(scoped *repository.Scoped) []countOnlySearch {
	page := domain.Pagination{Limit: 1}
	cases := repository.NewCaseRepository(scoped)
	incidents := repository.NewIncidentRepository(scoped)
	changeRequests := repository.NewChangeRequestRepository(scoped)
	problems := repository.NewProblemRepository(scoped)
	as := func(scope repository.SearchScope) context.Context {
		return repository.WithCallerIdentity(context.Background(), scope)
	}

	return []countOnlySearch{
		{"cases", func(scope repository.SearchScope, countOnly, filtered bool) (any, int, error) {
			req := domain.SearchCasesRequest{
				SortBy:     domain.CaseSort{Field: domain.CaseSortFieldCreatedOn, Order: domain.CaseSortOrderDesc},
				Pagination: page, CountOnly: countOnly,
			}
			if filtered {
				req.Parsed.States = []domain.CaseState{domain.CaseStateOpen}
			}
			return cases.SearchCases(as(scope), req, scope)
		}},
		{"incidents", func(scope repository.SearchScope, countOnly, filtered bool) (any, int, error) {
			var states []string
			if filtered {
				states = []string{"NEW"}
			}
			return incidents.SearchIncidents(as(scope), domain.SearchIncidentsRequest{
				SortBy:     domain.IncidentSort{Field: domain.IncidentSortFieldUpdatedOn, Order: domain.IncidentSortOrderDesc},
				Pagination: page, CountOnly: countOnly,
			}, nil, states, nil, nil, nil, nil, nil, nil)
		}},
		{"change requests", func(scope repository.SearchScope, countOnly, filtered bool) (any, int, error) {
			req := domain.SearchChangeRequestsRequest{
				SortBy:     domain.ChangeRequestSort{Field: domain.ChangeRequestSortFieldUpdatedOn, Order: domain.ChangeRequestSortOrderDesc},
				Pagination: page, CountOnly: countOnly,
			}
			if filtered {
				req.Filters.States = []domain.ChangeRequestState{domain.ChangeRequestStateNew}
			}
			return changeRequests.SearchChangeRequests(as(scope), req, nil, nil, nil, nil)
		}},
		{"problems", func(scope repository.SearchScope, countOnly, filtered bool) (any, int, error) {
			var states []string
			if filtered {
				states = []string{"NEW"}
			}
			return problems.SearchProblems(as(scope), domain.SearchProblemsRequest{Pagination: page, CountOnly: countOnly}, states, nil, nil)
		}},
	}
}

func TestSearchCountOnlyIntegration(t *testing.T) {
	pool, rec := tracedPool(t)
	scoped := repository.NewScoped(pool)

	internal := repository.SearchScope{Unrestricted: true, ViewerEmail: "count-only-test@wso2.com"}
	identities := []struct {
		name  string
		scope repository.SearchScope
	}{
		{"internal", internal},
		{"external with no projects", repository.SearchScope{ViewerEmail: "count-only-stranger@test.local"}},
	}
	// A project member: the project with the most work items, if the database
	// has any. Skipped (and reported as such) on an empty database.
	var memberProject string
	if err := scoped.QueryRow(repository.WithCallerIdentity(context.Background(), internal),
		`SELECT project_id::text FROM work_item WHERE project_id IS NOT NULL GROUP BY project_id ORDER BY COUNT(*) DESC LIMIT 1`,
	).Scan(&memberProject); err == nil && memberProject != "" {
		identities = append(identities, struct {
			name  string
			scope repository.SearchScope
		}{"project member", repository.SearchScope{ProjectIDs: []string{memberProject}, ViewerEmail: "count-only-member@test.local"}})
	} else {
		t.Logf("no work items with a project in this database, so the project-member identity is not exercised")
	}

	for _, search := range countOnlySearches(scoped) {
		for _, id := range identities {
			for _, filtered := range []bool{false, true} {
				name := search.name + "/" + id.name
				if filtered {
					name += "/filtered"
				}
				t.Run(name, func(t *testing.T) {
					rec.reset()
					_, want, err := search.run(id.scope, false, filtered)
					if err != nil {
						t.Fatalf("normal search: %v", err)
					}
					if want < 0 {
						t.Fatalf("a normal search reported total %d", want)
					}

					rec.reset()
					page, got, err := search.run(id.scope, true, filtered)
					if err != nil {
						t.Fatalf("count-only search: %v", err)
					}
					stmts := rec.statements()
					if len(stmts) != 1 || rec.counts() != 1 {
						t.Errorf("a count-only search sent %d statements (%d COUNT), want exactly one COUNT: %q", len(stmts), rec.counts(), statementHeads(stmts))
					}
					if got != want {
						t.Errorf("count-only total = %d, the normal search reports %d", got, want)
					}
					if b, _ := json.Marshal(page); string(b) != "[]" {
						t.Errorf("count-only page = %s, want an empty array", b)
					}
				})
			}
		}
	}
}
