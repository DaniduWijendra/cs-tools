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

package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/salesentity"
)

const (
	firstAccessEmail       = "jane@acme.com"
	firstAccessMembership1 = "a0e000000000001AAA"
	firstAccessMembership2 = "a0e000000000002AAA"
	firstAccessContact1    = "003000000000001AAA"
	firstAccessContact2    = "003000000000002AAA"
)

// ---- fakes ---------------------------------------------------------------

type fakeFirstAccessRepo struct {
	byEmail map[string][]repository.InvitedMembership
	queried []string
	err     error
}

func (f *fakeFirstAccessRepo) InvitedMembershipsByEmail(_ context.Context, email string) ([]repository.InvitedMembership, error) {
	f.queried = append(f.queried, email)
	if f.err != nil {
		return nil, f.err
	}
	return f.byEmail[email], nil
}

// fakeSalesEntityWriter records every call on ONE ordered slice, not one per
// method, so a test can assert the lockout clear really preceded the state
// write for the same membership -- the ordering the Salesforce trigger makes
// load-bearing.
type fakeSalesEntityWriter struct {
	calls        []string
	lockoutErrBy map[string]error
	stateErrBy   map[string]error
	persisted    map[string]salesentity.ProjectContact
}

func (f *fakeSalesEntityWriter) UpdateContactLockout(_ context.Context, contactSfID string, lockedOut bool) error {
	f.calls = append(f.calls, "lockout:"+contactSfID+":"+boolWord(lockedOut))
	return f.lockoutErrBy[contactSfID]
}

func (f *fakeSalesEntityWriter) UpdateProjectContactState(_ context.Context, membershipSfID, state string) (salesentity.ProjectContact, error) {
	f.calls = append(f.calls, "state:"+membershipSfID+":"+state)
	if err := f.stateErrBy[membershipSfID]; err != nil {
		return salesentity.ProjectContact{}, err
	}
	return f.persisted[membershipSfID], nil
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type fakeIngestEvents struct {
	handled []domain.SalesforceEventRequest
	errBy   map[string]error
}

func (f *fakeIngestEvents) HandleEvent(_ context.Context, req domain.SalesforceEventRequest) error {
	f.handled = append(f.handled, req)
	return f.errBy[req.ReferenceID]
}

// ---- harness -------------------------------------------------------------

type firstAccessHarness struct {
	repo   *fakeFirstAccessRepo
	sales  *fakeSalesEntityWriter
	events *fakeIngestEvents
	steps  *fakeStepRepo
	svc    FirstAccessService
}

func newFirstAccessHarness(memberships ...repository.InvitedMembership) *firstAccessHarness {
	h := &firstAccessHarness{
		repo:   &fakeFirstAccessRepo{byEmail: map[string][]repository.InvitedMembership{firstAccessEmail: memberships}},
		sales:  &fakeSalesEntityWriter{lockoutErrBy: map[string]error{}, stateErrBy: map[string]error{}, persisted: map[string]salesentity.ProjectContact{}},
		events: &fakeIngestEvents{errBy: map[string]error{}},
		steps:  &fakeStepRepo{},
	}
	h.svc = NewFirstAccessService(h.repo, h.sales, h.events, h.steps)
	return h
}

func firstAccessCtx(t *testing.T) context.Context {
	t.Helper()
	return contextWithUserIDToken(fakeJWTWithEmail(t, firstAccessEmail))
}

func invited(membershipSfID, contactSfID string) repository.InvitedMembership {
	return repository.InvitedMembership{MembershipSfID: membershipSfID, ContactSfID: contactSfID}
}

// stepFor returns the single recorded step for a membership, or fails.
func stepFor(t *testing.T, steps *fakeStepRepo, membershipSfID string) domain.UpsertOnboardingStepRequest {
	t.Helper()
	var found []domain.UpsertOnboardingStepRequest
	for _, s := range steps.upserts {
		if s.MembershipSfID == membershipSfID {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("recorded %d steps for %s, want exactly 1 (%+v)", len(found), membershipSfID, steps.upserts)
	}
	return found[0]
}

// ---- tests ---------------------------------------------------------------

// The endpoint runs on every profile load, so the no-membership case must do
// nothing at all -- in particular it must never reach Salesforce.
func TestFirstAccess_NoInvitedMembershipsIsANoOp(t *testing.T) {
	h := newFirstAccessHarness()

	if err := h.svc.RecordFirstAccess(firstAccessCtx(t)); err != nil {
		t.Fatalf("RecordFirstAccess: %v", err)
	}
	if got := len(h.sales.calls); got != 0 {
		t.Errorf("sales entity calls = %v, want none", h.sales.calls)
	}
	if got := len(h.events.handled); got != 0 {
		t.Errorf("re-ingest calls = %d, want 0", got)
	}
	if got := len(h.steps.upserts); got != 0 {
		t.Errorf("onboarding steps written = %d, want 0", got)
	}
	if !reflect.DeepEqual(h.repo.queried, []string{firstAccessEmail}) {
		t.Errorf("repo queried = %v, want [%s]", h.repo.queried, firstAccessEmail)
	}
}

// The Salesforce SN_T_Project_Contact trigger recomputes the membership state
// from the contact lockout flag on every save, so clearing the flag MUST come
// first -- asserted here explicitly, on one ordered call list.
func TestFirstAccess_ClearsLockoutBeforeSettingState(t *testing.T) {
	h := newFirstAccessHarness(invited(firstAccessMembership1, firstAccessContact1))
	h.sales.persisted[firstAccessMembership1] = salesentity.ProjectContact{
		ID: firstAccessMembership1, State: sampleStr(domain.MembershipStateRegistered), LastModifiedDate: sampleStr(testLastModified),
	}

	if err := h.svc.RecordFirstAccess(firstAccessCtx(t)); err != nil {
		t.Fatalf("RecordFirstAccess: %v", err)
	}

	want := []string{
		"lockout:" + firstAccessContact1 + ":false",
		"state:" + firstAccessMembership1 + ":" + domain.MembershipStateRegistered,
	}
	if !reflect.DeepEqual(h.sales.calls, want) {
		t.Fatalf("sales entity calls = %v, want %v (lockout must be cleared first)", h.sales.calls, want)
	}

	// Re-ingested through the same envelope the ASB subscriber would post.
	wantEvent := domain.SalesforceEventRequest{
		EventType:   domain.SalesforceEventUpdated,
		Entity:      domain.SalesforceEntityProjectContact,
		ReferenceID: firstAccessMembership1,
	}
	if !reflect.DeepEqual(h.events.handled, []domain.SalesforceEventRequest{wantEvent}) {
		t.Errorf("re-ingest = %+v, want %+v", h.events.handled, wantEvent)
	}

	step := stepFor(t, h.steps, firstAccessMembership1)
	if step.Step != domain.OnboardingStepRegistration || step.Status != domain.OnboardingStepSucceeded {
		t.Errorf("step = %s/%s, want REGISTRATION/SUCCEEDED", step.Step, step.Status)
	}
	if step.LastError != nil {
		t.Errorf("lastError = %q, want nil on success", *step.LastError)
	}
	if step.UpdatedBy != domain.SalesforceSyncActor {
		t.Errorf("updatedBy = %q, want %q", step.UpdatedBy, domain.SalesforceSyncActor)
	}
	if step.Email != firstAccessEmail {
		t.Errorf("email = %q, want %q", step.Email, firstAccessEmail)
	}
	if step.ContactSfID == nil || *step.ContactSfID != firstAccessContact1 {
		t.Errorf("contactSfId = %v, want %q", step.ContactSfID, firstAccessContact1)
	}
	// Stamped with the Salesforce version the flip actually persisted.
	wantModified, _ := parseSalesforceLastModified(sampleStr(testLastModified))
	if !step.EventModifiedOn.Equal(wantModified) {
		t.Errorf("eventModifiedOn = %v, want %v", step.EventModifiedOn, wantModified)
	}
}

// With the lockout flag still set the trigger would revert the state write, so
// a failed clear must skip it rather than issue a call that looks successful.
func TestFirstAccess_LockoutFailureSkipsTheStateUpdate(t *testing.T) {
	h := newFirstAccessHarness(invited(firstAccessMembership1, firstAccessContact1))
	h.sales.lockoutErrBy[firstAccessContact1] = &apierror.ServiceUnavailableError{Msg: "salesentity: down"}

	err := h.svc.RecordFirstAccess(firstAccessCtx(t))
	var sue *apierror.ServiceUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("err = %v (%T), want *apierror.ServiceUnavailableError", err, err)
	}

	want := []string{"lockout:" + firstAccessContact1 + ":false"}
	if !reflect.DeepEqual(h.sales.calls, want) {
		t.Errorf("sales entity calls = %v, want %v (no state update)", h.sales.calls, want)
	}
	if got := len(h.events.handled); got != 0 {
		t.Errorf("re-ingest calls = %d, want 0", got)
	}
	step := stepFor(t, h.steps, firstAccessMembership1)
	if step.Status != domain.OnboardingStepFailed {
		t.Errorf("status = %s, want FAILED", step.Status)
	}
	if step.LastError == nil || *step.LastError == "" {
		t.Error("lastError not recorded on a FAILED step")
	}
}

// Each of the three writes is part of one guarded per-membership attempt.
func TestFirstAccess_PerMembershipFailuresRecordFAILED(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(h *firstAccessHarness)
		wantCalls []string
		wantEvent bool
	}{
		{
			name: "state update rejected",
			setup: func(h *firstAccessHarness) {
				h.sales.stateErrBy[firstAccessMembership1] = &apierror.DownstreamError{Msg: "salesforce rejected the project contact update"}
			},
			wantCalls: []string{
				"lockout:" + firstAccessContact1 + ":false",
				"state:" + firstAccessMembership1 + ":" + domain.MembershipStateRegistered,
			},
		},
		{
			name: "re-ingest failed",
			setup: func(h *firstAccessHarness) {
				h.events.errBy[firstAccessMembership1] = errors.New("upsert membership: resolve project")
			},
			wantCalls: []string{
				"lockout:" + firstAccessContact1 + ":false",
				"state:" + firstAccessMembership1 + ":" + domain.MembershipStateRegistered,
			},
			wantEvent: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newFirstAccessHarness(invited(firstAccessMembership1, firstAccessContact1))
			tc.setup(h)

			if err := h.svc.RecordFirstAccess(firstAccessCtx(t)); err == nil {
				t.Fatal("RecordFirstAccess returned nil, want an error (the only membership failed)")
			}
			if !reflect.DeepEqual(h.sales.calls, tc.wantCalls) {
				t.Errorf("sales entity calls = %v, want %v", h.sales.calls, tc.wantCalls)
			}
			if got := len(h.events.handled) > 0; got != tc.wantEvent {
				t.Errorf("re-ingest attempted = %v, want %v", got, tc.wantEvent)
			}
			step := stepFor(t, h.steps, firstAccessMembership1)
			if step.Status != domain.OnboardingStepFailed {
				t.Errorf("status = %s, want FAILED", step.Status)
			}
		})
	}
}

// One bad membership must not cost the caller the others; the request still
// succeeds, and the failed one is still INVITED for the next profile load.
func TestFirstAccess_OneFailureDoesNotStopTheOthers(t *testing.T) {
	h := newFirstAccessHarness(
		invited(firstAccessMembership1, firstAccessContact1),
		invited(firstAccessMembership2, firstAccessContact2),
	)
	h.sales.lockoutErrBy[firstAccessContact1] = &apierror.ServiceUnavailableError{Msg: "salesentity: down"}

	if err := h.svc.RecordFirstAccess(firstAccessCtx(t)); err != nil {
		t.Fatalf("RecordFirstAccess = %v, want nil on a partial success", err)
	}

	want := []string{
		"lockout:" + firstAccessContact1 + ":false",
		"lockout:" + firstAccessContact2 + ":false",
		"state:" + firstAccessMembership2 + ":" + domain.MembershipStateRegistered,
	}
	if !reflect.DeepEqual(h.sales.calls, want) {
		t.Errorf("sales entity calls = %v, want %v", h.sales.calls, want)
	}
	if got := stepFor(t, h.steps, firstAccessMembership1).Status; got != domain.OnboardingStepFailed {
		t.Errorf("membership 1 status = %s, want FAILED", got)
	}
	if got := stepFor(t, h.steps, firstAccessMembership2).Status; got != domain.OnboardingStepSucceeded {
		t.Errorf("membership 2 status = %s, want SUCCEEDED", got)
	}
}

func TestFirstAccess_EveryMembershipFailingIsAnError(t *testing.T) {
	h := newFirstAccessHarness(
		invited(firstAccessMembership1, firstAccessContact1),
		invited(firstAccessMembership2, firstAccessContact2),
	)
	h.sales.lockoutErrBy[firstAccessContact1] = &apierror.ServiceUnavailableError{Msg: "salesentity: down"}
	h.sales.lockoutErrBy[firstAccessContact2] = &apierror.ServiceUnavailableError{Msg: "salesentity: down"}

	err := h.svc.RecordFirstAccess(firstAccessCtx(t))
	var sue *apierror.ServiceUnavailableError
	if !errors.As(err, &sue) {
		t.Fatalf("err = %v (%T), want *apierror.ServiceUnavailableError", err, err)
	}
	// Both were still attempted before the error was reported.
	if got := len(h.sales.calls); got != 2 {
		t.Errorf("sales entity calls = %v, want one lockout attempt per membership", h.sales.calls)
	}
}

func TestFirstAccess_RejectsACallerWithNoIdentity(t *testing.T) {
	tests := []struct {
		name  string
		token string
		check func(error) bool
	}{
		{
			name:  "no x-user-id-token at all",
			token: "",
			check: func(err error) bool {
				var ue *apierror.UnauthorizedError
				return errors.As(err, &ue)
			},
		},
		{
			name:  "undecodable x-user-id-token",
			token: "not-a-jwt",
			check: func(err error) bool {
				var ve *apierror.ValidationError
				return errors.As(err, &ve)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newFirstAccessHarness(invited(firstAccessMembership1, firstAccessContact1))

			err := h.svc.RecordFirstAccess(contextWithUserIDToken(tc.token))
			if err == nil || !tc.check(err) {
				t.Fatalf("err = %v (%T), want the rejection for %s", err, err, tc.name)
			}
			if len(h.repo.queried) != 0 {
				t.Errorf("repo queried = %v, want no lookup for an unidentified caller", h.repo.queried)
			}
			if len(h.sales.calls) != 0 {
				t.Errorf("sales entity calls = %v, want none", h.sales.calls)
			}
		})
	}
}

// A membership whose persisted record came back empty (sales-entity-service
// could not re-read it after a successful write) is still a success.
func TestFirstAccess_EmptyPersistedRecordIsStillASuccess(t *testing.T) {
	h := newFirstAccessHarness(invited(firstAccessMembership1, firstAccessContact1))
	// No h.sales.persisted entry: a zero ProjectContact, no LastModifiedDate.

	if err := h.svc.RecordFirstAccess(firstAccessCtx(t)); err != nil {
		t.Fatalf("RecordFirstAccess: %v", err)
	}
	step := stepFor(t, h.steps, firstAccessMembership1)
	if step.Status != domain.OnboardingStepSucceeded {
		t.Errorf("status = %s, want SUCCEEDED", step.Status)
	}
	if step.EventModifiedOn.IsZero() {
		t.Error("eventModifiedOn is zero; want a now() fallback when Salesforce returned no LastModifiedDate")
	}
}
