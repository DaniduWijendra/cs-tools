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
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

// panicOnSearchProjectRepo is a ProjectRepository stub whose SearchProjects
// panics if called — used to prove a rejection happens before the Postgres
// path ever reaches the repository, not just that the repository happens to
// ignore the field.
type panicOnSearchProjectRepo struct{}

func (panicOnSearchProjectRepo) SearchProjects(context.Context, domain.SearchProjectsRequest) ([]domain.Project, int, error) {
	panic("SearchProjects should not be called")
}

func (panicOnSearchProjectRepo) GetProjectByID(context.Context, string) (domain.ProjectDetailsView, error) {
	panic("GetProjectByID should not be called")
}

// TestProjectService_SearchProjects_RejectsExcludeFilters proves the
// Postgres-backed SearchProjects rejects ExcludeClosureStates/
// ExcludeSubscriptionTypes before ever reaching the repository — the
// ServiceNow data source is the only one that implements this filtering (see
// snProjectService.fetchAllProjectsFiltered), and the Postgres path has no
// equivalent, so it must fail loudly rather than silently return an
// unfiltered result.
func TestProjectService_SearchProjects_RejectsExcludeFilters(t *testing.T) {
	svc := NewProjectService(panicOnSearchProjectRepo{})

	t.Run("excludeClosureStates", func(t *testing.T) {
		_, err := svc.SearchProjects(context.Background(), domain.SearchProjectsRequest{
			Pagination:           domain.Pagination{Limit: 10},
			ExcludeClosureStates: []string{"Restricted"},
		})
		var ve *apierror.ValidationError
		if !asValidationError(err, &ve) {
			t.Fatalf("expected *apierror.ValidationError, got %T: %v", err, err)
		}
	})

	t.Run("excludeSubscriptionTypes", func(t *testing.T) {
		_, err := svc.SearchProjects(context.Background(), domain.SearchProjectsRequest{
			Pagination:               domain.Pagination{Limit: 10},
			ExcludeSubscriptionTypes: []domain.SubscriptionType{domain.SubscriptionTypeCloudSupport},
		})
		var ve *apierror.ValidationError
		if !asValidationError(err, &ve) {
			t.Fatalf("expected *apierror.ValidationError, got %T: %v", err, err)
		}
	})
}
