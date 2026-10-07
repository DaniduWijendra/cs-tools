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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

// The four searches a dashboard count tile uses pass CountOnly on to their
// repositories untouched, leave the default behaviour alone, and refuse a
// request that sets both CountOnly and SkipTotal before any query runs. Each
// service copies or rebuilds its request on the way (the case service prepares
// a parsed copy), so a change there that drops the flag would put the page
// query back with no other test failing.
func TestSearchServicesHandleCountOnly(t *testing.T) {
	ctx := contextWithUserIDToken(fakeJWTWithEmail(t, "jane.doe@example.com"))
	page := domain.Pagination{Limit: 1}

	searches := []struct {
		name string
		// run searches with the given flags and reports whether the repository
		// was reached, whether it saw CountOnly, and the service's error.
		run func(skip, countOnly bool) (called, sawCountOnly bool, err error)
	}{
		{"cases", func(skip, countOnly bool) (bool, bool, error) {
			var called, saw bool
			repo := &stubCaseRepo{searchCases: func(_ context.Context, req domain.SearchCasesRequest) ([]domain.SearchCaseView, int, error) {
				called, saw = true, req.CountOnly
				return nil, 7, nil
			}}
			svc := NewCaseService(repo, stubUserRepo{}, nil, alwaysUnrestrictedAccess{}, nil)
			_, err := svc.SearchCases(ctx, domain.SearchCasesRequest{Pagination: page, SkipTotal: skip, CountOnly: countOnly})
			return called, saw, err
		}},
		{"incidents", func(skip, countOnly bool) (bool, bool, error) {
			var seen domain.SearchIncidentsRequest
			svc := NewIncidentService(skipTotalIncidentRepo{&stubIncidentRepo{}, &seen}, nil)
			_, err := svc.SearchIncidents(ctx, domain.SearchIncidentsRequest{Pagination: page, SkipTotal: skip, CountOnly: countOnly})
			return seen.Pagination.Limit != 0, seen.CountOnly, err
		}},
		{"change requests", func(skip, countOnly bool) (bool, bool, error) {
			var seen domain.SearchChangeRequestsRequest
			svc := NewChangeRequestService(skipTotalChangeRequestRepo{&stubChangeRequestRepo{}, &seen}, stubUserRepo{})
			_, err := svc.SearchChangeRequests(ctx, domain.SearchChangeRequestsRequest{Pagination: page, SkipTotal: skip, CountOnly: countOnly})
			return seen.Pagination.Limit != 0, seen.CountOnly, err
		}},
		{"problems", func(skip, countOnly bool) (bool, bool, error) {
			var seen domain.SearchProblemsRequest
			svc := NewProblemService(skipTotalProblemRepo{&stubProblemRepo{}, &seen})
			_, err := svc.SearchProblems(ctx, domain.SearchProblemsRequest{Pagination: page, SkipTotal: skip, CountOnly: countOnly})
			return seen.Pagination.Limit != 0, seen.CountOnly, err
		}},
	}

	for _, s := range searches {
		t.Run(s.name+"/passes CountOnly to the repository", func(t *testing.T) {
			called, saw, err := s.run(false, true)
			if err != nil {
				t.Fatalf("search with CountOnly: %v", err)
			}
			if !called || !saw {
				t.Errorf("repository reached = %v, saw CountOnly = %v; want both true", called, saw)
			}
		})
		t.Run(s.name+"/default request is unchanged", func(t *testing.T) {
			called, saw, err := s.run(false, false)
			if err != nil {
				t.Fatalf("plain search: %v", err)
			}
			if !called || saw {
				t.Errorf("repository reached = %v, saw CountOnly = %v; want true and false", called, saw)
			}
		})
		t.Run(s.name+"/refuses CountOnly with SkipTotal", func(t *testing.T) {
			called, _, err := s.run(true, true)
			var ve *apierror.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %v, want a validation error", err)
			}
			if called {
				t.Errorf("the repository was reached for a request that sets both flags")
			}
		})
	}
}
