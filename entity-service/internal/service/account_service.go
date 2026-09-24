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

// Package service is declared in interfaces.go.
package service

import (
	"context"
	"time"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

type accountService struct {
	repo repository.AccountRepository
}

// NewAccountService constructs an AccountService backed by the given repository.
func NewAccountService(repo repository.AccountRepository) AccountService {
	return &accountService{repo: repo}
}

// SearchAccounts implements AccountService.
func (s *accountService) SearchAccounts(ctx context.Context, req domain.SearchAccountsRequest) (domain.SearchAccountsResponse, error) {
	if err := normalizePagination(&req.Pagination); err != nil {
		return domain.SearchAccountsResponse{}, err
	}
	if err := validateSearchQuery(req.Filters.SearchQuery); err != nil {
		return domain.SearchAccountsResponse{}, err
	}
	rows, total, err := s.repo.SearchAccounts(ctx, req)
	if err != nil {
		return domain.SearchAccountsResponse{}, err
	}

	accounts := make([]domain.AccountView, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, accountRowToView(row))
	}

	return domain.SearchAccountsResponse{
		Accounts: accounts,
		Total:    total,
		Limit:    req.Pagination.Limit,
		Offset:   req.Pagination.Offset,
		HasMore:  req.Pagination.Offset+len(accounts) < total,
	}, nil
}

// GetAccountByID implements AccountService.
func (s *accountService) GetAccountByID(ctx context.Context, id string) (domain.AccountDetail, error) {
	if err := validateUUIDs("id", []string{id}); err != nil {
		return domain.AccountDetail{}, err
	}
	row, err := s.repo.GetAccountByID(ctx, id)
	if err != nil {
		return domain.AccountDetail{}, err
	}
	return accountRowToDetail(row), nil
}

// accountPersonRef builds a PersonRef from a joined person id/name/email triple,
// returning nil when the id is absent (no owner/manager linked).
func accountPersonRef(id, name, email *string) *domain.PersonRef {
	if id == nil || *id == "" {
		return nil
	}
	var personName string
	if name != nil {
		personName = *name
	}
	return &domain.PersonRef{ID: *id, Name: personName, Email: email}
}

func dateOnlyOrNil(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

func boolOrFalse(b *bool) bool {
	return b != nil && *b
}

// accountRowCommonFields maps the fields shared between the search view and
// the account detail response. SupportTier, ArrToday, CreTeam, and SreTeam
// are not available in the Postgres schema and are always nil.
func accountRowCommonFields(row repository.AccountRow) (classification string, technicalOwner, accountManager, renewalAccountManager *domain.PersonRef) {
	if row.Classification != nil {
		classification = *row.Classification
	}
	technicalOwner = accountPersonRef(row.TechnicalOwnerID, row.TechnicalOwnerName, row.TechnicalOwnerEmail)
	accountManager = accountPersonRef(row.AccountManagerID, row.AccountManagerName, row.AccountManagerEmail)
	renewalAccountManager = accountPersonRef(row.RenewalAccountManagerID, row.RenewalAccountManagerName, row.RenewalAccountManagerEmail)
	return
}

func accountRowToView(row repository.AccountRow) domain.AccountView {
	classification, technicalOwner, accountManager, renewalAccountManager := accountRowCommonFields(row)
	createdBy := row.CreatedBy

	return domain.AccountView{
		ID:                    row.ID,
		Name:                  row.Name,
		Classification:        classification,
		Pod:                   row.Pod,
		SfID:                  row.SfID,
		Region:                row.Region,
		SupportTier:           nil,
		ArrToday:              nil,
		TechnicalOwner:        technicalOwner,
		AccountManager:        accountManager,
		RenewalAccountManager: renewalAccountManager,
		CreTeam:               nil,
		SreTeam:               nil,
		ActivationDate:        dateOnlyOrNil(row.ActivationDate),
		DeactivationDate:      dateOnlyOrNil(row.DeactivationDate),
		HasAgent:              boolOrFalse(row.HasAgent),
		HasKbReferences:       boolOrFalse(row.HasKbReferences),
		CreatedOn:             row.CreatedOn.Format(time.RFC3339),
		CreatedBy:             &createdBy,
		UpdatedOn:             row.UpdatedOn.Format(time.RFC3339),
	}
}

func accountRowToDetail(row repository.AccountRow) domain.AccountDetail {
	classification, technicalOwner, accountManager, renewalAccountManager := accountRowCommonFields(row)
	createdBy := row.CreatedBy

	return domain.AccountDetail{
		ID:                    row.ID,
		Name:                  row.Name,
		Classification:        classification,
		Pod:                   row.Pod,
		SfID:                  row.SfID,
		Region:                row.Region,
		SupportTier:           nil,
		ArrToday:              nil,
		TechnicalOwner:        technicalOwner,
		AccountManager:        accountManager,
		RenewalAccountManager: renewalAccountManager,
		CreTeam:               nil,
		SreTeam:               nil,
		ActivationDate:        dateOnlyOrNil(row.ActivationDate),
		DeactivationDate:      dateOnlyOrNil(row.DeactivationDate),
		HasAgent:              boolOrFalse(row.HasAgent),
		HasKbReferences:       boolOrFalse(row.HasKbReferences),
		CreatedOn:             row.CreatedOn.Format(time.RFC3339),
		CreatedBy:             &createdBy,
		UpdatedOn:             row.UpdatedOn.Format(time.RFC3339),
	}
}
