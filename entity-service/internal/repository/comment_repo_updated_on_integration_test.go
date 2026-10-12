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

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

// Adding a comment is activity on the work item: CreateComment stamps the
// parent's updated_on / updated_by in the same statement, so the lists' Updated
// column, their updated-date filters and the dashboard reminder widgets see a
// case that was just nudged as touched. This is real SQL against the real
// schema as a restricted login (row-level security in force), so it also proves
// the extra UPDATE is allowed for a project member and cannot touch a work item
// the caller cannot see.
//
//	CASE_STATS_TEST_DSN=postgres://... go test ./internal/repository/ -run CommentUpdatedOn

const (
	cuProjectID = "75000000-0000-0000-0000-000000000001"
	cuAccountID = "75111111-0000-0000-0000-000000000001"
	cuContactID = "75222222-0000-0000-0000-000000000001"
	cuCaseID    = "75333333-0000-0000-0000-000000000001"
	cuOtherCase = "75333333-0000-0000-0000-000000000002"

	cuMember   = "cu-member@test.local"
	cuStranger = "cu-stranger@test.local"
)

// cuStale is the updated_on every seeded work item starts with: far enough in
// the past that any bump is unmistakable.
var cuStale = time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Second)

func seedCommentUpdatedOnFixture(t *testing.T) (context.Context, *repository.Scoped, repository.CommentRepository) {
	t.Helper()
	pool := caseStatsPool(t)
	internal := repository.WithSystemIdentity(context.Background())
	scoped := repository.NewScoped(pool)

	cleanup := func() {
		_, _ = scoped.Exec(internal, `DELETE FROM comment WHERE work_item_id IN ($1, $2)`, cuCaseID, cuOtherCase)
		_, _ = scoped.Exec(internal, `DELETE FROM work_item WHERE project_id = $1`, cuProjectID)
		_, _ = pool.Exec(internal, `DELETE FROM project_contact WHERE project_id = $1`, cuProjectID)
		_, _ = pool.Exec(internal, `DELETE FROM project WHERE id = $1`, cuProjectID)
		_, _ = pool.Exec(internal, `DELETE FROM account_contact WHERE id = $1`, cuContactID)
		_, _ = pool.Exec(internal, `DELETE FROM account WHERE id = $1`, cuAccountID)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustPool := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(internal, sql, args...); err != nil {
			t.Fatalf("seed (%.80s): %v", sql, err)
		}
	}
	mustScoped := func(sql string, args ...any) {
		t.Helper()
		if _, err := scoped.Exec(internal, sql, args...); err != nil {
			t.Fatalf("seed scoped (%.80s): %v", sql, err)
		}
	}

	now := time.Now().UTC()
	mustPool(`INSERT INTO account (id, created_on, updated_on, created_by, updated_by, name, number, sf_id)
		VALUES ($1, $2, $2, 'test', 'test', 'CU Test Account', 'CU-ACC-1', 'sf-cu-acc-1')`, cuAccountID, now)
	mustPool(`INSERT INTO account_contact (id, created_on, updated_on, created_by, updated_by, user_name, account_id)
		VALUES ($1, $2, $2, 'test', 'test', 'CU Test Contact', $3)`, cuContactID, now, cuAccountID)
	mustPool(`INSERT INTO project (id, created_on, updated_on, created_by, updated_by, key, sf_id, account_id)
		VALUES ($1, $2, $2, 'test', 'test', 'CUTEST', 'sf-cutest', $3)`, cuProjectID, now, cuAccountID)
	mustPool(`INSERT INTO project_contact (id, created_on, updated_on, created_by, updated_by, email, account_contact_id, project_id, state)
		VALUES (gen_random_uuid(), $1, $1, 'test', 'test', $2, $3, $4, 'REGISTERED')`, now, cuMember, cuContactID, cuProjectID)

	// cuCaseID is a CASE; cuOtherCase an INCIDENT, which a "case" reference
	// must refuse without touching.
	mustScoped(`INSERT INTO work_item (id, created_on, updated_on, created_by, updated_by, number, wso2_id, subject, type, project_id)
		VALUES ($1, $2, $2, 'seed', 'seed', 'CU-CASE-1', 'CU-WSO2-1', 'cu case', 'CASE', $3)`, cuCaseID, cuStale, cuProjectID)
	mustScoped(`INSERT INTO work_item (id, created_on, updated_on, created_by, updated_by, number, subject, type, project_id)
		VALUES ($1, $2, $2, 'seed', 'seed', 'CU-INC-1', 'cu incident', 'INCIDENT', $3)`, cuOtherCase, cuStale, cuProjectID)

	return internal, scoped, repository.NewCommentRepository(scoped)
}

func workItemStamp(t *testing.T, ctx context.Context, scoped *repository.Scoped, id string) (time.Time, string) {
	t.Helper()
	var updatedOn time.Time
	var updatedBy string
	if err := scoped.QueryRow(ctx, `SELECT updated_on, updated_by FROM work_item WHERE id = $1`, id).Scan(&updatedOn, &updatedBy); err != nil {
		t.Fatalf("read work_item stamp: %v", err)
	}
	return updatedOn.UTC(), updatedBy
}

func TestCommentUpdatedOnIntegration_MemberCommentBumpsTheCase(t *testing.T) {
	internal, scoped, repo := seedCommentUpdatedOnFixture(t)
	member := repository.WithCallerIdentity(context.Background(), repository.SearchScope{ProjectIDs: []string{cuProjectID}, ViewerEmail: cuMember})

	before := time.Now().UTC()
	row, err := repo.CreateComment(member, cuCaseID, domain.ReferenceTypeCase, "COMMENT", "<p>Please let us know.</p>", cuMember)
	if err != nil {
		t.Fatalf("CreateComment as a project member: %v", err)
	}

	gotOn, gotBy := workItemStamp(t, internal, scoped, cuCaseID)
	if gotOn.Before(before.Add(-time.Second)) {
		t.Fatalf("work_item.updated_on = %s, want it advanced to the comment's time (>= %s); it was left at the stale %s", gotOn, before, cuStale)
	}
	if !gotOn.Equal(row.CreatedOn.UTC()) {
		t.Errorf("work_item.updated_on = %s, comment.created_on = %s, want the same instant (one statement, one NOW())", gotOn, row.CreatedOn.UTC())
	}
	if gotBy != cuMember {
		t.Errorf("work_item.updated_by = %q, want the commenter %q", gotBy, cuMember)
	}

	var n int
	if err := scoped.QueryRow(internal, `SELECT count(*) FROM comment WHERE work_item_id = $1`, cuCaseID).Scan(&n); err != nil || n != 1 {
		t.Errorf("comment rows for the case = %d (err %v), want exactly the one just written", n, err)
	}
}

func TestCommentUpdatedOnIntegration_InternalWorkNoteBumpsTheCase(t *testing.T) {
	internal, scoped, repo := seedCommentUpdatedOnFixture(t)

	before := time.Now().UTC()
	if _, err := repo.CreateComment(internal, cuCaseID, domain.ReferenceTypeCase, "WORK_NOTE", "internal note", "staff@test.local"); err != nil {
		t.Fatalf("CreateComment as internal: %v", err)
	}
	gotOn, gotBy := workItemStamp(t, internal, scoped, cuCaseID)
	if gotOn.Before(before.Add(-time.Second)) {
		t.Fatalf("work_item.updated_on = %s after an internal work note, want >= %s", gotOn, before)
	}
	if gotBy != "staff@test.local" {
		t.Errorf("work_item.updated_by = %q, want %q", gotBy, "staff@test.local")
	}
}

// POST /cases/{id}/comments is the route "Request update" and every case reply
// takes, and it is a different repository method from the reference-generic
// comment create above (that route refuses referenceType "case"), so it is held
// to the same rule on its own.
func TestCommentUpdatedOnIntegration_CaseCommentBumpsTheCase(t *testing.T) {
	internal, scoped, _ := seedCommentUpdatedOnFixture(t)
	caseRepo := repository.NewCaseRepository(scoped)
	member := repository.WithCallerIdentity(context.Background(), repository.SearchScope{ProjectIDs: []string{cuProjectID}, ViewerEmail: cuMember})

	before := time.Now().UTC()
	c, err := caseRepo.CreateCaseComment(member, domain.CreateCaseCommentRequest{
		CaseID: cuCaseID, CreatedBy: cuMember, Type: domain.CommentTypeComment, Content: "<p>Please let us know.</p>",
	}, nil)
	if err != nil {
		t.Fatalf("CreateCaseComment as a project member: %v", err)
	}

	gotOn, gotBy := workItemStamp(t, internal, scoped, cuCaseID)
	if gotOn.Before(before.Add(-time.Second)) {
		t.Fatalf("work_item.updated_on = %s, want it advanced to the comment's time (>= %s); it was left at the stale %s", gotOn, before, cuStale)
	}
	if !gotOn.Equal(c.CreatedOn.UTC()) {
		t.Errorf("work_item.updated_on = %s, comment.created_on = %s, want the same instant", gotOn, c.CreatedOn.UTC())
	}
	if gotBy != cuMember {
		t.Errorf("work_item.updated_by = %q, want the commenter %q", gotBy, cuMember)
	}
}

func TestCommentUpdatedOnIntegration_MirroredHistoryDoesNotBumpTheCase(t *testing.T) {
	internal, scoped, _ := seedCommentUpdatedOnFixture(t)
	caseRepo := repository.NewCaseRepository(scoped)

	// ServiceNow's initial comments are copied in with their own past
	// timestamp when a case is created under dual write: that is history, not
	// something happening to the case now.
	past := cuStale.Add(-48 * time.Hour)
	c, err := caseRepo.CreateCaseCommentAsSystem(internal, domain.CreateCaseCommentRequest{
		CaseID: cuCaseID, CreatedBy: "customer@test.local", Type: domain.CommentTypeComment, Content: "opening message",
	}, &past)
	if err != nil {
		t.Fatalf("CreateCaseCommentAsSystem with a past timestamp: %v", err)
	}
	if !c.CreatedOn.UTC().Equal(past) {
		t.Fatalf("comment.created_on = %s, want the mirrored %s", c.CreatedOn.UTC(), past)
	}
	if gotOn, gotBy := workItemStamp(t, internal, scoped, cuCaseID); !gotOn.Equal(cuStale) || gotBy != "seed" {
		t.Errorf("work_item stamp = %s / %q after a mirrored comment, want it untouched (%s / seed)", gotOn, gotBy, cuStale)
	}
}

func TestCommentUpdatedOnIntegration_RefusedCaseCommentLeavesTheWorkItemAlone(t *testing.T) {
	internal, scoped, _ := seedCommentUpdatedOnFixture(t)
	caseRepo := repository.NewCaseRepository(scoped)
	stranger := repository.WithCallerIdentity(context.Background(), repository.SearchScope{ViewerEmail: cuStranger})

	_, err := caseRepo.CreateCaseComment(stranger, domain.CreateCaseCommentRequest{
		CaseID: cuCaseID, CreatedBy: cuStranger, Type: domain.CommentTypeComment, Content: "should not land",
	}, nil)
	var nf *apierror.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("CreateCaseComment as a stranger error = %v, want a NotFoundError", err)
	}
	if gotOn, gotBy := workItemStamp(t, internal, scoped, cuCaseID); !gotOn.Equal(cuStale) || gotBy != "seed" {
		t.Errorf("work_item stamp = %s / %q after a refused comment, want it untouched (%s / seed)", gotOn, gotBy, cuStale)
	}
}

func TestCommentUpdatedOnIntegration_RefusedCommentLeavesTheWorkItemAlone(t *testing.T) {
	internal, scoped, repo := seedCommentUpdatedOnFixture(t)
	member := repository.WithCallerIdentity(context.Background(), repository.SearchScope{ProjectIDs: []string{cuProjectID}, ViewerEmail: cuMember})
	stranger := repository.WithCallerIdentity(context.Background(), repository.SearchScope{ViewerEmail: cuStranger})

	cases := []struct {
		name string
		ctx  context.Context
		id   string
	}{
		{"a case reference naming an incident", member, cuOtherCase},
		{"a caller who is not on the project", stranger, cuCaseID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.CreateComment(tc.ctx, tc.id, domain.ReferenceTypeCase, "COMMENT", "should not land", cuStranger)
			var nf *apierror.NotFoundError
			if !errors.As(err, &nf) {
				t.Fatalf("CreateComment error = %v, want a NotFoundError", err)
			}
			if gotOn, gotBy := workItemStamp(t, internal, scoped, tc.id); !gotOn.Equal(cuStale) || gotBy != "seed" {
				t.Errorf("work_item stamp = %s / %q after a refused comment, want it untouched (%s / seed)", gotOn, gotBy, cuStale)
			}
		})
	}

	var n int
	if err := scoped.QueryRow(internal, `SELECT count(*) FROM comment WHERE work_item_id IN ($1, $2)`, cuCaseID, cuOtherCase).Scan(&n); err != nil || n != 0 {
		t.Errorf("comment rows after refused writes = %d (err %v), want 0", n, err)
	}
}
