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

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

// ProjectMembershipRepository writes a Salesforce Project_Contact__c
// (membership) into the Postgres model: "user", user_role, account_contact,
// project_contact and project_contact_group, in one transaction. It is the
// single write path for customer memberships — the Salesforce ingest, the
// portal's replayed envelopes and any reconcile all end up here.
//
// Rows are resolved by natural keys first (sf_id when already stamped, then
// project key / account sf_id / contact email / (project, account_contact)),
// and every Salesforce id is stamped on the way, so replaying the same
// membership is idempotent even though the tables carry no unique constraints
// on those keys. The DATABASE onboarding step is recorded inside the same
// transaction so it can never disagree with the rows.
type ProjectMembershipRepository interface {
	// Upsert writes one membership. NotFoundError when the project or account
	// is unknown, ConflictError when the contact email matches more than one
	// user, ServiceUnavailableError when a role or project_group row the
	// mapping relies on is missing (the ServiceNow sync seeds them).
	Upsert(ctx context.Context, in domain.SalesforceMembershipUpsert, step domain.UpsertOnboardingStepRequest) (domain.SalesforceMembershipUpsertResult, error)
	// DeactivateBySfID sets project_contact.state = DEACTIVATED for the
	// membership with that Salesforce id and marks its DATABASE onboarding
	// step as applied by a DELETED event, so the ingest's duplicate guard does
	// not treat a later RESTORED/replayed event carrying the same Salesforce
	// LastModifiedDate as already ingested. An unknown id is a no-op (the
	// membership was never ingested), so a DELETED event is idempotent.
	DeactivateBySfID(ctx context.Context, membershipSfID string) (bool, error)
}

type projectMembershipRepo struct {
	db *pgxpool.Pool
}

// NewProjectMembershipRepository constructs a ProjectMembershipRepository backed by the pool.
func NewProjectMembershipRepository(db *pgxpool.Pool) ProjectMembershipRepository {
	return &projectMembershipRepo{db: db}
}

func (r *projectMembershipRepo) DeactivateBySfID(ctx context.Context, membershipSfID string) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("deactivate project contact: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE project_contact
		SET state = $2::project_contact_state_enum, updated_on = NOW(), updated_by = $3
		WHERE sf_id = $1`,
		membershipSfID, domain.MembershipStateDeactivated, domain.SalesforceSyncActor)
	if err != nil {
		return false, fmt.Errorf("deactivate project contact by sf_id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	// Salesforce has no LastModifiedDate to offer for a deleted record, so
	// the step keeps its recorded version and only its event_type changes;
	// the guard in the ingest ignores DELETED-typed steps.
	if _, err := tx.Exec(ctx, `
		UPDATE onboarding_step
		SET event_type = $2, updated_on = NOW(), updated_by = $3
		WHERE membership_sf_id = $1 AND step = 'DATABASE'::onboarding_step_enum`,
		membershipSfID, string(domain.SalesforceEventDeleted), domain.SalesforceSyncActor); err != nil {
		return false, fmt.Errorf("mark DATABASE step deleted: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("deactivate project contact: commit: %w", err)
	}
	return true, nil
}

func (r *projectMembershipRepo) Upsert(ctx context.Context, in domain.SalesforceMembershipUpsert, step domain.UpsertOnboardingStepRequest) (domain.SalesforceMembershipUpsertResult, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.SalesforceMembershipUpsertResult{}, fmt.Errorf("upsert membership: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := upsertMembershipTx(ctx, tx, in)
	if err != nil {
		return domain.SalesforceMembershipUpsertResult{}, err
	}

	step.ProjectID = &res.ProjectID
	step.ProjectContactID = &res.ProjectContactID
	if _, err := upsertOnboardingStep(ctx, tx, step); err != nil {
		return domain.SalesforceMembershipUpsertResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.SalesforceMembershipUpsertResult{}, fmt.Errorf("upsert membership: commit: %w", err)
	}
	return res, nil
}

// upsertMembershipTx runs the seven resolution/write steps inside tx.
func upsertMembershipTx(ctx context.Context, tx pgx.Tx, in domain.SalesforceMembershipUpsert) (domain.SalesforceMembershipUpsertResult, error) {
	var res domain.SalesforceMembershipUpsertResult
	actor := domain.SalesforceSyncActor

	// 1. project — by key, then by sf_id.
	var projectAccountID *string
	err := tx.QueryRow(ctx, `
		SELECT id, account_id FROM project
		WHERE (key = $1 AND $1 <> '') OR (sf_id = $2 AND $2 <> '')
		ORDER BY CASE WHEN key = $1 THEN 0 ELSE 1 END
		LIMIT 1`, in.ProjectKey, in.ProjectSfID).Scan(&res.ProjectID, &projectAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, &apierror.NotFoundError{Msg: fmt.Sprintf("project not found for key %q / sfId %q", in.ProjectKey, in.ProjectSfID)}
	}
	if err != nil {
		return res, fmt.Errorf("upsert membership: resolve project: %w", err)
	}

	// 2. account — the contact's own account by sf_id; an own contact falls
	// back to the project's account.
	if in.ContactAccountSfID != "" {
		err = tx.QueryRow(ctx, `SELECT id FROM account WHERE sf_id = $1`, in.ContactAccountSfID).Scan(&res.AccountID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return res, fmt.Errorf("upsert membership: resolve account: %w", err)
		}
	}
	if res.AccountID == "" {
		if !strings.EqualFold(in.Type, domain.MembershipTypePartnerContact) && projectAccountID != nil {
			res.AccountID = *projectAccountID
		} else {
			return res, &apierror.NotFoundError{Msg: fmt.Sprintf("account not found for sfId %q", in.ContactAccountSfID)}
		}
	}

	// 3. user — by sf_id, then by email (must be unique), else insert.
	var userName string
	res.UserID, userName, res.CreatedUser, err = upsertMembershipUser(ctx, tx, in, actor)
	if err != nil {
		return res, err
	}

	// 4. global roles.
	if err := syncGlobalRoles(ctx, tx, res.UserID, in.GlobalRoles, in.ManagedAdminRoles, actor); err != nil {
		return res, err
	}

	// 5. account_contact — by sf_id, then (account, user_name), else insert.
	res.AccountContactID, res.CreatedAccountContact, err = upsertAccountContact(ctx, tx, in.ContactSfID, res.AccountID, userName, actor)
	if err != nil {
		return res, err
	}

	// 6. project_contact — by sf_id, then (project, account_contact), else insert.
	res.ProjectContactID, res.CreatedProjectContact, err = upsertProjectContact(ctx, tx, in, res.ProjectID, res.AccountContactID, actor)
	if err != nil {
		return res, err
	}

	// 7. project groups.
	if err := syncProjectGroups(ctx, tx, res.ProjectContactID, in.ProjectGroups, actor); err != nil {
		return res, err
	}
	return res, nil
}

func upsertMembershipUser(ctx context.Context, tx pgx.Tx, in domain.SalesforceMembershipUpsert, actor string) (id, userName string, created bool, err error) {
	email := strings.ToLower(strings.TrimSpace(in.ContactEmail))
	if email == "" {
		return "", "", false, &apierror.ValidationError{Msg: "contact email is required to resolve the user"}
	}

	err = tx.QueryRow(ctx, `SELECT id, user_name FROM "user" WHERE sf_id = $1`, in.ContactSfID).Scan(&id, &userName)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, fmt.Errorf("upsert membership: resolve user by sf_id: %w", err)
	}
	if id == "" {
		rows, qerr := tx.Query(ctx, `SELECT id, user_name FROM "user" WHERE LOWER(email) = $1 LIMIT 2`, email)
		if qerr != nil {
			return "", "", false, fmt.Errorf("upsert membership: resolve user by email: %w", qerr)
		}
		var matches [][2]string
		for rows.Next() {
			var mid, mname string
			if serr := rows.Scan(&mid, &mname); serr != nil {
				rows.Close()
				return "", "", false, fmt.Errorf("upsert membership: scan user: %w", serr)
			}
			matches = append(matches, [2]string{mid, mname})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", "", false, fmt.Errorf("upsert membership: iterate users: %w", err)
		}
		switch len(matches) {
		case 0:
		case 1:
			id, userName = matches[0][0], matches[0][1]
		default:
			return "", "", false, &apierror.ConflictError{Msg: "more than one user matches the contact email; cannot resolve the membership"}
		}
	}

	if id == "" {
		err = tx.QueryRow(ctx, `
			INSERT INTO "user" (id, created_on, updated_on, created_by, updated_by,
				user_name, name, first_name, last_name, email, is_active, is_system_user, sf_id)
			VALUES (gen_random_uuid(), NOW(), NOW(), $1, $1, $2, $3, $4, $5, $2, TRUE, $6, $7)
			RETURNING id, user_name`,
			actor, email, nullIfBlank(in.ContactName), nullIfBlank(in.ContactFirstName), nullIfBlank(in.ContactLastName),
			in.IsCsIntegrationUser, in.ContactSfID).Scan(&id, &userName)
		if err != nil {
			return "", "", false, fmt.Errorf("upsert membership: insert user: %w", err)
		}
		return id, userName, true, nil
	}

	// Existing row: refresh profile fields and stamp the Salesforce id, but
	// never touch user_name — it is the join key to account_contact.
	if _, err = tx.Exec(ctx, `
		UPDATE "user"
		SET name = COALESCE($2, name), first_name = COALESCE($3, first_name), last_name = COALESCE($4, last_name),
		    email = $5, is_system_user = $6, sf_id = $7, updated_on = NOW(), updated_by = $8
		WHERE id = $1`,
		id, nullIfBlank(in.ContactName), nullIfBlank(in.ContactFirstName), nullIfBlank(in.ContactLastName),
		email, in.IsCsIntegrationUser, in.ContactSfID, actor); err != nil {
		return "", "", false, fmt.Errorf("upsert membership: update user: %w", err)
	}
	return id, userName, false, nil
}

func syncGlobalRoles(ctx context.Context, tx pgx.Tx, userID string, wanted, managed []string, actor string) error {
	if len(wanted) > 0 {
		roleIDs := map[string]string{}
		rows, err := tx.Query(ctx, `SELECT id, name FROM role WHERE name = ANY($1::text[])`, wanted)
		if err != nil {
			return fmt.Errorf("upsert membership: resolve roles: %w", err)
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return fmt.Errorf("upsert membership: scan role: %w", err)
			}
			roleIDs[name] = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("upsert membership: iterate roles: %w", err)
		}
		for _, name := range wanted {
			if _, ok := roleIDs[name]; !ok {
				return &apierror.ServiceUnavailableError{Msg: fmt.Sprintf("role %q is not seeded in the role table", name)}
			}
		}

		held := map[string]bool{}
		hrows, err := tx.Query(ctx, `SELECT r.name FROM user_role ur JOIN role r ON r.id = ur.role_id WHERE ur.user_id = $1`, userID)
		if err != nil {
			return fmt.Errorf("upsert membership: query user roles: %w", err)
		}
		for hrows.Next() {
			var name string
			if err := hrows.Scan(&name); err != nil {
				hrows.Close()
				return fmt.Errorf("upsert membership: scan user role: %w", err)
			}
			held[name] = true
		}
		hrows.Close()
		if err := hrows.Err(); err != nil {
			return fmt.Errorf("upsert membership: iterate user roles: %w", err)
		}

		for _, name := range wanted {
			if held[name] {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_role (id, created_on, updated_on, created_by, updated_by, user_id, role_id)
				VALUES (gen_random_uuid(), NOW(), NOW(), $1, $1, $2, $3)`, actor, userID, roleIDs[name]); err != nil {
				return fmt.Errorf("upsert membership: grant role %s: %w", name, err)
			}
		}
	}

	// Revoke managed admin roles the mapping no longer grants.
	revoke := make([]string, 0, len(managed))
	wantedSet := map[string]bool{}
	for _, w := range wanted {
		wantedSet[w] = true
	}
	for _, m := range managed {
		if !wantedSet[m] {
			revoke = append(revoke, m)
		}
	}
	if len(revoke) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM user_role ur USING role r
			WHERE ur.role_id = r.id AND ur.user_id = $1 AND r.name = ANY($2::text[])`, userID, revoke); err != nil {
			return fmt.Errorf("upsert membership: revoke roles: %w", err)
		}
	}
	return nil
}

func upsertAccountContact(ctx context.Context, tx pgx.Tx, contactSfID, accountID, userName, actor string) (id string, created bool, err error) {
	err = tx.QueryRow(ctx, `SELECT id FROM account_contact WHERE sf_id = $1 AND account_id = $2`, contactSfID, accountID).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("upsert membership: resolve account_contact by sf_id: %w", err)
	}
	if id == "" {
		err = tx.QueryRow(ctx, `
			SELECT id FROM account_contact WHERE account_id = $1 AND LOWER(user_name) = LOWER($2)
			ORDER BY created_on LIMIT 1`, accountID, userName).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("upsert membership: resolve account_contact: %w", err)
		}
	}
	if id == "" {
		err = tx.QueryRow(ctx, `
			INSERT INTO account_contact (id, created_on, updated_on, created_by, updated_by,
				is_active, user_name, is_primary_contact, account_id, sf_id)
			VALUES (gen_random_uuid(), NOW(), NOW(), $1, $1, TRUE, $2, FALSE, $3, $4)
			RETURNING id`, actor, userName, accountID, contactSfID).Scan(&id)
		if err != nil {
			return "", false, fmt.Errorf("upsert membership: insert account_contact: %w", err)
		}
		return id, true, nil
	}
	if _, err = tx.Exec(ctx, `
		UPDATE account_contact SET is_active = TRUE, sf_id = $2, updated_on = NOW(), updated_by = $3 WHERE id = $1`,
		id, contactSfID, actor); err != nil {
		return "", false, fmt.Errorf("upsert membership: update account_contact: %w", err)
	}
	return id, false, nil
}

func upsertProjectContact(ctx context.Context, tx pgx.Tx, in domain.SalesforceMembershipUpsert, projectID, accountContactID, actor string) (id string, created bool, err error) {
	err = tx.QueryRow(ctx, `SELECT id FROM project_contact WHERE sf_id = $1`, in.MembershipSfID).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("upsert membership: resolve project_contact by sf_id: %w", err)
	}
	if id == "" {
		err = tx.QueryRow(ctx, `
			SELECT id FROM project_contact WHERE project_id = $1 AND account_contact_id = $2
			ORDER BY created_on LIMIT 1`, projectID, accountContactID).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("upsert membership: resolve project_contact: %w", err)
		}
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if id == "" {
		err = tx.QueryRow(ctx, `
			INSERT INTO project_contact (id, created_on, updated_on, created_by, updated_by,
				email, state, account_contact_id, project_id, sf_id)
			VALUES (gen_random_uuid(), NOW(), NOW(), $1, $1, $2, $3::project_contact_state_enum, $4, $5, $6)
			RETURNING id`, actor, email, in.State, accountContactID, projectID, in.MembershipSfID).Scan(&id)
		if err != nil {
			return "", false, fmt.Errorf("upsert membership: insert project_contact: %w", err)
		}
		return id, true, nil
	}
	if _, err = tx.Exec(ctx, `
		UPDATE project_contact
		SET email = $2, state = $3::project_contact_state_enum, account_contact_id = $4, sf_id = $5,
		    updated_on = NOW(), updated_by = $6
		WHERE id = $1`,
		id, email, in.State, accountContactID, in.MembershipSfID, actor); err != nil {
		return "", false, fmt.Errorf("upsert membership: update project_contact: %w", err)
	}
	return id, false, nil
}

func syncProjectGroups(ctx context.Context, tx pgx.Tx, projectContactID string, groups []string, actor string) error {
	groupIDs := map[string]string{}
	if len(groups) > 0 {
		rows, err := tx.Query(ctx, `SELECT id, "group" FROM project_group WHERE "group" = ANY($1::text[])`, groups)
		if err != nil {
			return fmt.Errorf("upsert membership: resolve project groups: %w", err)
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return fmt.Errorf("upsert membership: scan project group: %w", err)
			}
			groupIDs[name] = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("upsert membership: iterate project groups: %w", err)
		}
		for _, g := range groups {
			if _, ok := groupIDs[g]; !ok {
				return &apierror.ServiceUnavailableError{Msg: fmt.Sprintf("project group %q is not present in project_group", g)}
			}
		}
	}

	wanted := make([]string, 0, len(groupIDs))
	for _, id := range groupIDs {
		wanted = append(wanted, id)
	}
	// Remove memberships outside the target set (an empty set removes all).
	if _, err := tx.Exec(ctx, `
		DELETE FROM project_contact_group
		WHERE project_contact_id = $1 AND NOT (project_group_id::text = ANY($2::text[]))`,
		projectContactID, wanted); err != nil {
		return fmt.Errorf("upsert membership: remove project groups: %w", err)
	}
	// Add the missing ones.
	for _, gid := range wanted {
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_contact_group (id, created_on, updated_on, created_by, updated_by, project_group_id, project_contact_id)
			SELECT gen_random_uuid(), NOW(), NOW(), $1, $1, $2, $3
			WHERE NOT EXISTS (
				SELECT 1 FROM project_contact_group WHERE project_group_id = $2 AND project_contact_id = $3)`,
			actor, gid, projectContactID); err != nil {
			return fmt.Errorf("upsert membership: add project group: %w", err)
		}
	}
	return nil
}

func nullIfBlank(s string) *string {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	return &t
}
