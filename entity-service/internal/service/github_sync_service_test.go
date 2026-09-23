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
	"fmt"
	"strings"
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/github"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

const ghIssueURL = "https://github.com/wso2/choreo/issues/42"

type fakeGhRepo struct {
	claimed    map[string]bool
	linked     map[string]string
	mappingErr error
	accountID  string
	linkErr    error
	mapping    *repository.RepoMapping
	cr         *repository.GithubChangeRequest
}

func (f *fakeGhRepo) RepoMapping(context.Context, string, string) (*repository.RepoMapping, error) {
	return f.mapping, f.mappingErr
}
func (f *fakeGhRepo) ChangeRequestByGitReference(context.Context, string) (*repository.GithubChangeRequest, error) {
	return f.cr, nil
}

// Behaves like the table it stands in for: a second claim on the same id is
// refused. A fake that always returned nil is why the missing claim call went
// unnoticed.
func (f *fakeGhRepo) RepoForAccount(context.Context, string) (*repository.RepoMapping, error) {
	return f.mapping, f.mappingErr
}
func (f *fakeGhRepo) AccountForCase(context.Context, string) (string, error) {
	return f.accountID, nil
}
func (f *fakeGhRepo) SetCaseGithubIssueNumber(_ context.Context, caseID string, n int) (bool, error) {
	if f.linkErr != nil {
		return false, f.linkErr
	}
	if f.linked == nil {
		f.linked = map[string]string{}
	}
	f.linked[caseID] = fmt.Sprint(n)
	return true, nil
}

func (f *fakeGhRepo) ClaimDelivery(_ context.Context, id, _, _ string) error {
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	if f.claimed[id] {
		return repository.ErrDeliverySeen
	}
	f.claimed[id] = true
	return nil
}

func (f *fakeGhRepo) ReleaseDelivery(_ context.Context, id string) error {
	delete(f.claimed, id)
	return nil
}

func (f *fakeGhRepo) LinkDelivery(_ context.Context, id, crID string) error {
	if f.linked == nil {
		f.linked = map[string]string{}
	}
	f.linked[id] = crID
	return nil
}

type fakeGhClient struct {
	comments []string
	states   []github.State
	labels   [][]string
	removed  []string
	added    []string
}

func (f *fakeGhClient) CreateComment(_ context.Context, _ github.Issue, body string) (*github.Comment, error) {
	f.comments = append(f.comments, body)
	return &github.Comment{}, nil
}
func (f *fakeGhClient) SetLabels(_ context.Context, _ github.Issue, l []string) error {
	f.labels = append(f.labels, l)
	return nil
}
func (f *fakeGhClient) AddLabel(_ context.Context, _ github.Issue, l string) error {
	f.added = append(f.added, l)
	return nil
}
func (f *fakeGhClient) RemoveLabel(_ context.Context, _ github.Issue, l string) error {
	f.removed = append(f.removed, l)
	return nil
}
func (f *fakeGhClient) SetState(_ context.Context, _ github.Issue, s github.State) error {
	f.states = append(f.states, s)
	return nil
}

func ghDelivery(event, action string, labels []string) Delivery {
	var p IssuePayload
	p.Action = action
	p.Issue.HTMLURL = ghIssueURL
	p.Issue.Number = 42
	p.Repository.Name = "choreo"
	p.Repository.Owner.Login = "wso2"
	p.Sender.Login = "a-human"
	for _, l := range labels {
		p.Issue.Labels = append(p.Issue.Labels, struct {
			Name string `json:"name"`
		}{Name: l})
	}
	return Delivery{ID: "d1", Event: event, Payload: p}
}

func crLabels() []string {
	return []string{labelChangeRequest, "CRType/Normal", labelScopeApp}
}

func newGhSvc(r *fakeGhRepo, c *fakeGhClient) GithubSyncService {
	return NewGithubSyncService(r, c, "wso2-integration-bot")
}

func mapped() *repository.RepoMapping {
	return &repository.RepoMapping{AccountID: "a1", AccountName: "Choreo Customer", CredentialRef: "gh-choreo"}
}

// Our own writes come back as webhooks; dropping them by sender identity is
// what stops an endless loop.
func TestGithubSync_DropsOwnEvents(t *testing.T) {
	d := ghDelivery("issues", "labeled", crLabels())
	d.Payload.Sender.Login = "WSO2-Integration-Bot" // case-insensitive
	got, err := newGhSvc(&fakeGhRepo{mapping: mapped()}, &fakeGhClient{}).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if !strings.Contains(got.Skipped, "integration account") {
		t.Fatalf("skipped = %q, want the integration-account reason", got.Skipped)
	}
}

// The mapping table is the allow-list. An unmapped repository is not an error.
func TestGithubSync_UnmappedRepositoryIsSkipped(t *testing.T) {
	got, err := newGhSvc(&fakeGhRepo{mapping: nil}, &fakeGhClient{}).
		HandleWebhook(context.Background(), ghDelivery("issues", "labeled", crLabels()))
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if !strings.Contains(got.Skipped, "not mapped") {
		t.Fatalf("skipped = %q", got.Skipped)
	}
}

// A change request is created by labelling, never by opening an issue.
func TestGithubSync_CreationRequiresLabelling(t *testing.T) {
	for _, action := range []string{"opened", "edited"} {
		t.Run(action, func(t *testing.T) {
			got, _ := newGhSvc(&fakeGhRepo{mapping: mapped(), cr: nil}, &fakeGhClient{}).
				HandleWebhook(context.Background(), ghDelivery("issues", action, crLabels()))
			if got.Action != "" {
				t.Fatalf("action = %q, want no action", got.Action)
			}
		})
	}
	got, _ := newGhSvc(&fakeGhRepo{mapping: mapped(), cr: nil}, &fakeGhClient{}).
		HandleWebhook(context.Background(), ghDelivery("issues", "labeled", crLabels()))
	if got.Action != "creation_prepared" {
		t.Fatalf("action = %q, want creation_prepared", got.Action)
	}
}

// The full label set gates everything.
func TestGithubSync_LabelGate(t *testing.T) {
	cases := map[string]struct {
		labels []string
		want   bool
	}{
		"complete":        {crLabels(), true},
		"no type label":   {[]string{"CRType/Normal", labelScopeApp}, false},
		"no CRType":       {[]string{labelChangeRequest, labelScopeApp}, false},
		"no scope":        {[]string{labelChangeRequest, "CRType/Normal"}, false},
		"two scopes":      {[]string{labelChangeRequest, "CRType/Normal", labelScopeApp, labelScopeInfra}, false},
		"unrelated extra": {append(crLabels(), "bug"), true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := DefaultGithubLabels().Valid(tc.labels); got != tc.want {
				t.Fatalf("Valid(%v) = %v, want %v", tc.labels, got, tc.want)
			}
		})
	}
}

// THE GUARD WORTH KEEPING: closing is only allowed from Review. Anything else
// reopens the issue and explains why.
func TestGithubSync_CloseRefusedOutsideReview(t *testing.T) {
	client := &fakeGhClient{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", Number: "CHG1", State: "ASSESS"}}

	got, err := newGhSvc(repo, client).HandleWebhook(context.Background(), ghDelivery("issues", "closed", crLabels()))
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "close_refused" {
		t.Fatalf("action = %q, want close_refused", got.Action)
	}
	if len(client.states) != 1 || client.states[0] != github.StateOpen {
		t.Fatalf("issue was not reopened: %v", client.states)
	}
	if len(client.comments) != 1 || !strings.Contains(client.comments[0], "ASSESS") {
		t.Fatalf("comment did not name the state: %v", client.comments)
	}
}

func TestGithubSync_CloseAllowedFromReview(t *testing.T) {
	client := &fakeGhClient{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: stateReview}}

	got, err := newGhSvc(repo, client).HandleWebhook(context.Background(), ghDelivery("issues", "closed", crLabels()))
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "close_allowed_pending_write" {
		t.Fatalf("action = %q, want close_allowed_pending_write", got.Action)
	}
	if len(client.states) != 0 {
		t.Fatalf("issue should not have been reopened: %v", client.states)
	}
}

// Comments mirror only on creation, and only for a linked issue.
func TestGithubSync_Comments(t *testing.T) {
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1"}}
	d := ghDelivery("issue_comment", "created", crLabels())
	d.Payload.Comment = &struct {
		Body    string      `json:"body"`
		HTMLURL string      `json:"html_url"`
		User    github.User `json:"user"`
	}{Body: "hello"}

	got, _ := newGhSvc(repo, &fakeGhClient{}).HandleWebhook(context.Background(), d)
	if got.Action != "comment_pending_write" {
		t.Fatalf("action = %q, want comment_pending_write", got.Action)
	}

	edited := ghDelivery("issue_comment", "edited", crLabels())
	got, _ = newGhSvc(repo, &fakeGhClient{}).HandleWebhook(context.Background(), edited)
	if got.Action != "" {
		t.Fatalf("edited comment should not mirror, got %q", got.Action)
	}

	unlinked := &fakeGhRepo{mapping: mapped(), cr: nil}
	got, _ = newGhSvc(unlinked, &fakeGhClient{}).HandleWebhook(context.Background(), d)
	if got.Action != "" {
		t.Fatalf("unlinked issue should not mirror, got %q", got.Action)
	}
}

// State comes from the label's name, not from ServiceNow's numeric codes --
// whose own table disagreed with its own constants.
func TestGithubSync_StateByLabelName(t *testing.T) {
	want := map[string]string{
		"Assessed": "ASSESS", "Authorized": "AUTHORIZE", "Scheduled": "SCHEDULED",
		"Implemented": "IMPLEMENT", "Reviewed": "REVIEW",
	}
	for label, state := range want {
		got, ok := DefaultGithubLabels().StateFor(label)
		if !ok || got != state {
			t.Errorf("StateFor(%q) = %q,%v want %q", label, got, ok, state)
		}
	}
	// Closed and Canceled must not be label-driven: closing has a guard that a
	// label would bypass.
	for _, label := range []string{"Closed", "Canceled"} {
		if _, ok := DefaultGithubLabels().StateFor(label); ok {
			t.Errorf("%q should not be label-driven", label)
		}
	}
}

func TestGithubAttributes(t *testing.T) {
	impact, likelihood, crType := DefaultGithubLabels().Attributes([]string{
		labelChangeRequest, "CRType/Normal", labelScopeInfra, labelImpactHigh, labelLikelihoodMed,
	})
	if impact != "HIGH" {
		t.Errorf("impact = %q, want HIGH", impact)
	}
	if likelihood != "MEDIUM" {
		t.Errorf("likelihood = %q, want MEDIUM", likelihood)
	}
	if crType != "INFRA" {
		t.Errorf("crType = %q, want INFRA", crType)
	}

	// Absent labels leave fields empty rather than defaulting to the lowest
	// value, which would be indistinguishable from a deliberate choice.
	impact, likelihood, crType = DefaultGithubLabels().Attributes([]string{labelChangeRequest, "CRType/Normal", labelScopeApp})
	if impact != "" || likelihood != "" {
		t.Errorf("absent labels defaulted: impact=%q likelihood=%q", impact, likelihood)
	}
	if crType != "GENERAL" {
		t.Errorf("crType = %q, want GENERAL", crType)
	}
}

func TestGithubSync_UnhandledEvents(t *testing.T) {
	for _, event := range []string{"push", "pull_request", "star"} {
		got, err := newGhSvc(&fakeGhRepo{mapping: mapped()}, &fakeGhClient{}).
			HandleWebhook(context.Background(), ghDelivery(event, "created", crLabels()))
		if err != nil {
			t.Fatalf("HandleWebhook(%s): %v", event, err)
		}
		if got.Skipped == "" {
			t.Fatalf("event %s should have been skipped", event)
		}
	}
}

// The label set an issue carries once its change request exists. Mirrors what
// ServiceNow wrote back on creation.
func TestResolveCreationLabels(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want []string
	}{
		"keeps the type, strips state labels": {
			in:   []string{labelChangeRequest, "CRType/Normal", labelScopeApp, "Assessed", "Implemented"},
			want: []string{"CRType/Normal", labelChangeRequest, labelScopeApp},
		},
		"keeps the author's own labels": {
			in:   []string{labelChangeRequest, "CRType/Emergency", labelScopeInfra, "bug", "priority/high"},
			want: []string{"CRType/Emergency", labelChangeRequest, labelScopeInfra, "bug", "priority/high"},
		},
		"only one CRType survives": {
			in:   []string{labelChangeRequest, "CRType/Normal", "CRType/Standard", labelScopeApp},
			want: []string{"CRType/Normal", labelChangeRequest, labelScopeApp},
		},
		"Canceled is not stripped — matching ServiceNow's own list": {
			in:   []string{labelChangeRequest, "CRType/Normal", labelScopeApp, "Canceled"},
			want: []string{"CRType/Normal", labelChangeRequest, labelScopeApp, "Canceled"},
		},
		"no duplicates": {
			in:   []string{labelChangeRequest, labelChangeRequest, "CRType/Normal", labelScopeApp},
			want: []string{"CRType/Normal", labelChangeRequest, labelScopeApp},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := DefaultGithubLabels().ResolveOnCreate(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// The CRType label leads, so a reader of the issue sees the type first and the
// set is stable for the same input.
func TestResolveCreationLabels_TypeLeads(t *testing.T) {
	got := DefaultGithubLabels().ResolveOnCreate([]string{"bug", labelChangeRequest, "CRType/Standard", labelScopeApp})
	if len(got) == 0 || got[0] != "CRType/Standard" {
		t.Fatalf("got %v, want CRType/Standard first", got)
	}
}

// On creation the issue's labels are normalised and written back, and the
// author is told the issue was picked up.
func TestGithubSync_CreationNormalisesLabels(t *testing.T) {
	client := &fakeGhClient{}
	repo := &fakeGhRepo{mapping: mapped(), cr: nil}

	d := ghDelivery("issues", "labeled", []string{
		labelChangeRequest, "CRType/Normal", labelScopeInfra,
		"Assessed", // a state label — must be stripped
		"bug",      // the author's own — must survive
		labelImpactHigh,
	})
	got, err := newGhSvc(repo, client).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "creation_prepared" {
		t.Fatalf("action = %q", got.Action)
	}

	if len(client.labels) != 1 {
		t.Fatalf("labels written %d times, want 1: %v", len(client.labels), client.labels)
	}
	written := client.labels[0]
	if written[0] != "CRType/Normal" {
		t.Errorf("the type label should lead: %v", written)
	}
	for _, gone := range []string{"Assessed"} {
		for _, l := range written {
			if l == gone {
				t.Errorf("%q should have been stripped: %v", gone, written)
			}
		}
	}
	var keptBug bool
	for _, l := range written {
		if l == "bug" {
			keptBug = true
		}
	}
	if !keptBug {
		t.Errorf("the author's own label was dropped: %v", written)
	}

	// And the acknowledgement names what was derived.
	if len(client.comments) != 1 {
		t.Fatalf("comments = %v", client.comments)
	}
	for _, want := range []string{"INFRA", "HIGH"} {
		if !strings.Contains(client.comments[0], want) {
			t.Errorf("acknowledgement missing %q: %q", want, client.comments[0])
		}
	}
}

// An already-correct label set is not rewritten: a pointless write produces a
// webhook we then have to drop.
func TestGithubSync_CreationSkipsRedundantLabelWrite(t *testing.T) {
	client := &fakeGhClient{}
	d := ghDelivery("issues", "labeled", []string{"CRType/Normal", labelChangeRequest, labelScopeApp})
	if _, err := newGhSvc(&fakeGhRepo{mapping: mapped()}, client).HandleWebhook(context.Background(), d); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if len(client.labels) != 0 {
		t.Fatalf("labels were rewritten unnecessarily: %v", client.labels)
	}
}

// ── the mutation layer ───────────────────────────────────────────────────────

type fakeMutate struct {
	created   []repository.NewChangeRequestFromIssue
	updated   []repository.NewChangeRequestFromIssue
	states    []string
	comments  []string
	assignees []string
	createErr error
}

func (f *fakeMutate) CreateFromIssue(_ context.Context, in repository.NewChangeRequestFromIssue) (string, string, error) {
	if f.createErr != nil {
		return "", "", f.createErr
	}
	f.created = append(f.created, in)
	return "cr-new", "CHG-GH-000001", nil
}
func (f *fakeMutate) UpdateFromIssue(_ context.Context, _ string, in repository.NewChangeRequestFromIssue) error {
	f.updated = append(f.updated, in)
	return nil
}
func (f *fakeMutate) SetState(_ context.Context, _, state string) (bool, error) {
	f.states = append(f.states, state)
	return true, nil
}
func (f *fakeMutate) AddComment(_ context.Context, _, content, _ string) error {
	f.comments = append(f.comments, content)
	return nil
}
func (f *fakeMutate) SetAssignee(_ context.Context, _, userID string) (bool, error) {
	f.assignees = append(f.assignees, userID)
	return true, nil
}
func (f *fakeMutate) UserIDForGithubLogin(context.Context, string) (string, error) { return "", nil }

func writingSvc(r *fakeGhRepo, c *fakeGhClient, m *fakeMutate) GithubSyncService {
	return NewGithubSyncServiceWriting(r, m, c, "wso2-integration-bot", DefaultGithubLabels())
}

func TestGithubSync_CreatesTheChangeRequest(t *testing.T) {
	m := &fakeMutate{}
	client := &fakeGhClient{}
	d := ghDelivery("issues", "labeled", []string{labelChangeRequest, "CRType/Normal", labelScopeInfra, labelImpactHigh})
	d.Payload.Issue.Title = "Rotate the gateway certificates"
	d.Payload.Issue.Body = "Across all three nodes."

	got, err := writingSvc(&fakeGhRepo{mapping: mapped()}, client, m).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "created" {
		t.Fatalf("action = %q, want created", got.Action)
	}
	if len(m.created) != 1 {
		t.Fatalf("created %d records, want 1", len(m.created))
	}
	in := m.created[0]
	if in.Subject != "Rotate the gateway certificates" || in.Type != "INFRA" || in.Impact != "HIGH" {
		t.Errorf("wrong field mapping: %+v", in)
	}
	if in.GitReference != ghIssueURL {
		t.Errorf("git reference = %q", in.GitReference)
	}
	// The author is told the number, not just that something happened.
	if len(client.comments) != 1 || !strings.Contains(client.comments[0], "CHG-GH-000001") {
		t.Errorf("acknowledgement did not name the record: %v", client.comments)
	}
}

// Two deliveries for the same issue race; the loser finds the record already
// there, which is the correct end state rather than an error.
func TestGithubSync_DuplicateCreationIsNotAnError(t *testing.T) {
	m := &fakeMutate{createErr: repository.ErrChangeRequestExists}
	got, err := writingSvc(&fakeGhRepo{mapping: mapped()}, &fakeGhClient{}, m).
		HandleWebhook(context.Background(), ghDelivery("issues", "labeled", crLabels()))
	if err != nil {
		t.Fatalf("a racing duplicate should not error: %v", err)
	}
	if got.Skipped == "" {
		t.Fatalf("want a skip, got %+v", got)
	}
}

func TestGithubSync_RelaysCommentToTheRecord(t *testing.T) {
	m := &fakeMutate{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1"}}
	d := ghDelivery("issue_comment", "created", crLabels())
	d.Payload.Comment = &struct {
		Body    string      `json:"body"`
		HTMLURL string      `json:"html_url"`
		User    github.User `json:"user"`
	}{Body: "Please schedule this for the weekend.", User: github.User{Login: "octocat"}}

	got, err := writingSvc(repo, &fakeGhClient{}, m).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "comment_relayed" {
		t.Fatalf("action = %q", got.Action)
	}
	if len(m.comments) != 1 || !strings.Contains(m.comments[0], "Please schedule this") {
		t.Fatalf("comment not written: %v", m.comments)
	}
	// Attributed, so a reader sees who said it.
	if !strings.Contains(m.comments[0], "octocat") {
		t.Errorf("comment lost its author: %q", m.comments[0])
	}
}

func TestGithubSync_StateLabelMovesTheRecord(t *testing.T) {
	m := &fakeMutate{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: "NEW"}}
	d := ghDelivery("issues", "labeled", append(crLabels(), "Authorized"))
	d.Payload.Label = &struct {
		Name string `json:"name"`
	}{Name: "Authorized"}

	got, err := writingSvc(repo, &fakeGhClient{}, m).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "state_changed" {
		t.Fatalf("action = %q", got.Action)
	}
	if len(m.states) != 1 || m.states[0] != "AUTHORIZE" {
		t.Fatalf("states = %v, want [AUTHORIZE]", m.states)
	}
}

func TestGithubSync_CloseMovesTheRecordToClosed(t *testing.T) {
	m := &fakeMutate{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: stateReview}}
	got, err := writingSvc(repo, &fakeGhClient{}, m).
		HandleWebhook(context.Background(), ghDelivery("issues", "closed", crLabels()))
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "closed" {
		t.Fatalf("action = %q", got.Action)
	}
	if len(m.states) != 1 || m.states[0] != stateClosed {
		t.Fatalf("states = %v", m.states)
	}
}

// ── the guards ───────────────────────────────────────────────────────────────

// A closed change request is finished: a label change is put back and answered.
func TestGithubSync_ClosedRecordRefusesLabelChanges(t *testing.T) {
	for _, action := range []string{"labeled", "unlabeled"} {
		t.Run(action, func(t *testing.T) {
			client := &fakeGhClient{}
			repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: stateClosed}}
			d := ghDelivery("issues", action, append(crLabels(), "extra"))
			d.Payload.Label = &struct {
				Name string `json:"name"`
			}{Name: "extra"}

			got, err := writingSvc(repo, client, &fakeMutate{}).HandleWebhook(context.Background(), d)
			if err != nil {
				t.Fatalf("HandleWebhook: %v", err)
			}
			if got.Action != "label_change_refused" {
				t.Fatalf("action = %q", got.Action)
			}
			if len(client.labels) != 1 {
				t.Fatalf("labels were not put back: %v", client.labels)
			}
			if len(client.comments) != 1 || !strings.Contains(client.comments[0], "closed") {
				t.Errorf("no explanation posted: %v", client.comments)
			}
		})
	}
}

// The type is fixed at creation.
func TestGithubSync_TypeLabelIsReverted(t *testing.T) {
	client := &fakeGhClient{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: "ASSESS"}}
	d := ghDelivery("issues", "labeled", append(crLabels(), "CRType/Emergency"))
	d.Payload.Label = &struct {
		Name string `json:"name"`
	}{Name: "CRType/Emergency"}

	got, err := writingSvc(repo, client, &fakeMutate{}).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "type_label_reverted" {
		t.Fatalf("action = %q", got.Action)
	}
	if len(client.removed) != 1 || client.removed[0] != "CRType/Emergency" {
		t.Fatalf("removed = %v", client.removed)
	}
}

// Removing a label the record owns puts it back.
func TestGithubSync_ProtectedLabelIsRestored(t *testing.T) {
	for _, label := range []string{"Authorized", "CRType/Normal"} {
		t.Run(label, func(t *testing.T) {
			client := &fakeGhClient{}
			repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: "ASSESS"}}
			d := ghDelivery("issues", "unlabeled", crLabels())
			d.Payload.Label = &struct {
				Name string `json:"name"`
			}{Name: label}

			got, err := writingSvc(repo, client, &fakeMutate{}).HandleWebhook(context.Background(), d)
			if err != nil {
				t.Fatalf("HandleWebhook: %v", err)
			}
			if got.Action != "protected_label_restored" {
				t.Fatalf("action = %q", got.Action)
			}
			if len(client.labels) != 1 {
				t.Fatalf("label not restored: %v", client.labels)
			}
		})
	}
}

// Exactly one scope: a new one replaces the old rather than sitting alongside.
func TestGithubSync_ScopeLabelReplacesTheOld(t *testing.T) {
	client := &fakeGhClient{}
	m := &fakeMutate{}
	repo := &fakeGhRepo{mapping: mapped(), cr: &repository.GithubChangeRequest{ID: "cr1", State: "ASSESS"}}
	d := ghDelivery("issues", "labeled", []string{labelChangeRequest, "CRType/Normal", labelScopeApp, labelScopeInfra})
	d.Payload.Label = &struct {
		Name string `json:"name"`
	}{Name: labelScopeInfra}

	got, err := writingSvc(repo, client, m).HandleWebhook(context.Background(), d)
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if got.Action != "scope_replaced" {
		t.Fatalf("action = %q", got.Action)
	}
	written := client.labels[0]
	for _, l := range written {
		if l == labelScopeApp {
			t.Errorf("the old scope survived: %v", written)
		}
	}
	if len(m.updated) != 1 || m.updated[0].Type != "INFRA" {
		t.Errorf("the record's type was not updated: %+v", m.updated)
	}
}

// GitHub retries any delivery it did not get a 2xx for. Replaying one must not
// redo the work -- an issue labelled once must not become two change requests.
//
// This shipped broken: ClaimDelivery, ReleaseDelivery and LinkDelivery were all
// implemented, none were called, and the fake returned nil unconditionally so
// nothing noticed.
func TestHandleWebhook_ReplayedDeliveryIsRefused(t *testing.T) {
	r := &fakeGhRepo{mapping: mapped()}
	svc := newGhSvc(r, &fakeGhClient{})
	d := ghDelivery("issues", "labeled", crLabels())

	if _, err := svc.HandleWebhook(context.Background(), d); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	_, err := svc.HandleWebhook(context.Background(), d)
	if !errors.Is(err, repository.ErrDeliverySeen) {
		t.Fatalf("replay was processed again; err = %v, want ErrDeliverySeen", err)
	}
}

// A delivery that failed must be retryable: the claim is released so GitHub's
// retry is not mistaken for a replay of something that already succeeded.
func TestHandleWebhook_FailedDeliveryReleasesItsClaim(t *testing.T) {
	r := &fakeGhRepo{mappingErr: errors.New("database is down")}
	svc := newGhSvc(r, &fakeGhClient{})
	d := ghDelivery("issues", "labeled", crLabels())

	if _, err := svc.HandleWebhook(context.Background(), d); err == nil {
		t.Fatal("want the underlying failure")
	}
	if r.claimed[d.ID] {
		t.Error("the claim survived a failure, so GitHub's retry would be refused as a replay")
	}
}
