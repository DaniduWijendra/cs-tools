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
	"log/slog"
	"strings"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/github"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

// GithubSyncService applies a GitHub issue webhook to a change request.
//
// Ported from ServiceNow's GitHubIssueContentProcessor, but the target schema
// is not the one it was written against, so several mappings are decisions
// rather than transcriptions. Each is named where it is made.
type GithubSyncService interface {
	// HandleWebhook applies one delivery. A delivery with nothing to do is not
	// an error -- most webhooks from a watched repository are not about a
	// change request at all.
	HandleWebhook(ctx context.Context, d Delivery) (Outcome, error)
}

// Delivery is one webhook, already authenticated.
type Delivery struct {
	ID      string
	Event   string
	Payload IssuePayload
}

// Outcome says what a delivery did, for the response and the log.
type Outcome struct {
	Action          string
	ChangeRequestID string
	// Skipped is why nothing happened, empty when something did.
	Skipped string
}

// IssuePayload is the part of GitHub's issues / issue_comment payload this
// reads. Everything else in a webhook body is ignored.
type IssuePayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		Labels  []struct {
			Name string `json:"name"`
		} `json:"labels"`
		User github.User `json:"user"`
	} `json:"issue"`
	Label *struct {
		Name string `json:"name"`
	} `json:"label"`
	Comment *struct {
		Body    string      `json:"body"`
		HTMLURL string      `json:"html_url"`
		User    github.User `json:"user"`
	} `json:"comment"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender github.User `json:"sender"`
}

// LabelNames flattens the issue's labels.
func (p IssuePayload) LabelNames() []string {
	out := make([]string, 0, len(p.Issue.Labels))
	for _, l := range p.Issue.Labels {
		out = append(out, l.Name)
	}
	return out
}

// The label protocol. These are constants rather than configuration: the
// parsing logic depends on their shape, so an operator who changed them in
// config would break the code that reads them. ServiceNow kept them in two
// places -- hardcoded in the processor AND in github.label.* properties -- and
// the two could disagree.
const (
	labelChangeRequest  = "Type/ChangeRequest"
	labelPrefixCRType   = "CRType/"
	labelPrefixCRScope  = "CRScope/"
	labelScopeApp       = "CRScope/Application"
	labelScopeInfra     = "CRScope/Infrastructure"
	labelImpactHigh     = "Impact 1"
	labelImpactMedium   = "Impact 2"
	labelLikelihoodHigh = "Likelihood 1"
	labelLikelihoodMed  = "Likelihood 2"
)

// githubStateByLabel maps a state label onto change_request_state_enum.
//
// BY NAME, NOT BY NUMBER. ServiceNow mapped these through its numeric codes,
// and its own table disagreed with its own constants: stateAssessed was -3
// while stateMap called -3 "Authorize". Mapping the label directly to the
// state it names avoids importing that off-by-one.
//
// "Closed" and "Canceled" are absent on purpose. Closing is driven by the
// issue's closed action, which has a guard; a label should not be able to
// bypass it.
var githubStateByLabel = map[string]string{
	"Assessed":    "ASSESS",
	"Authorized":  "AUTHORIZE",
	"Scheduled":   "SCHEDULED",
	"Implemented": "IMPLEMENT",
	"Reviewed":    "REVIEW",
}

// githubImpactByLabel maps the impact labels onto change_request_impact_enum.
// ServiceNow used 1/2/3 where 1 was the most severe; the enum says so instead.
var githubImpactByLabel = map[string]string{
	labelImpactHigh:   "HIGH",
	labelImpactMedium: "MEDIUM",
}

var githubLikelihoodByLabel = map[string]string{
	labelLikelihoodHigh: "HIGH",
	labelLikelihoodMed:  "MEDIUM",
}

// githubTypeByScope maps the scope label onto change_request_type_enum.
//
// A DECISION, NOT A TRANSCRIPTION. ServiceNow stored scope in u_crscope
// (Application / Infrastructure) and a separate u_type (normal / standard /
// emergency). This schema has neither: it has change_request_type, whose
// values are INFRA and GENERAL. Scope maps onto it cleanly; the normal /
// standard / emergency distinction has nowhere to go and is dropped, which is
// recorded as an open question in docs/github-cr-sync-spec.md rather than
// silently discarded.
var githubTypeByScope = map[string]string{
	labelScopeApp:   "GENERAL",
	labelScopeInfra: "INFRA",
}

// stateReview is the only state from which the issue may be closed.
const stateReview = "REVIEW"

// stateClosed is where a successful close lands.
const stateClosed = "CLOSED"

type githubSyncService struct {
	repo repository.GithubSyncRepository
	gh   githubIssueClient
	// labels is the vocabulary this deployment recognises. Defaults to
	// ServiceNow's values; overridable because a repository's labels are a
	// deployment's business and dev need not match production.
	labels GithubLabels
	// mutate writes the change request. Nil leaves the sync read-only, which
	// is how it behaved before the mutation layer existed and is still useful
	// for a dry run against a live repository.
	mutate repository.GithubMutationRepository
	// integrationLogin is our own GitHub account. Events it sent are our own
	// writes coming back and are dropped -- identity, not string-matching the
	// comment body the way the case webhook does.
	integrationLogin string
}

// githubIssueClient is the slice of *github.Client this service needs.
type githubIssueClient interface {
	CreateComment(ctx context.Context, issue github.Issue, body string) (*github.Comment, error)
	SetLabels(ctx context.Context, issue github.Issue, labels []string) error
	RemoveLabel(ctx context.Context, issue github.Issue, label string) error
	AddLabel(ctx context.Context, issue github.Issue, label string) error
	SetState(ctx context.Context, issue github.Issue, state github.State) error
}

// NewGithubSyncService constructs the webhook policy.
func NewGithubSyncService(repo repository.GithubSyncRepository, gh githubIssueClient, integrationLogin string) GithubSyncService {
	return NewGithubSyncServiceWithLabels(repo, gh, integrationLogin, DefaultGithubLabels())
}

// NewGithubSyncServiceWithLabels is NewGithubSyncService with an explicit
// label vocabulary.
func NewGithubSyncServiceWithLabels(repo repository.GithubSyncRepository, gh githubIssueClient, integrationLogin string, labels GithubLabels) GithubSyncService {
	return &githubSyncService{repo: repo, gh: gh, integrationLogin: integrationLogin, labels: labels}
}

// WithMutations returns the service able to write change requests. Without it
// the sync recognises and reports but changes nothing.
func (s *githubSyncService) WithMutations(m repository.GithubMutationRepository) GithubSyncService {
	s.mutate = m
	return s
}

// NewGithubSyncServiceWriting is the full service: recognises, writes, and
// pushes the resulting label changes back to the issue.
func NewGithubSyncServiceWriting(repo repository.GithubSyncRepository, mutate repository.GithubMutationRepository, gh githubIssueClient, integrationLogin string, labels GithubLabels) GithubSyncService {
	return &githubSyncService{repo: repo, gh: gh, integrationLogin: integrationLogin, labels: labels, mutate: mutate}
}

func skip(reason string) (Outcome, error) { return Outcome{Skipped: reason}, nil }

// HandleWebhook implements GithubSyncService.
//
// THE DELIVERY CLAIM WRAPS EVERY WRITE. GitHub retries any delivery it did not
// get a 2xx for, and an issue labelled once must not become two change
// requests -- the spec calls idempotency mandatory here, unlike the CR
// notification flows where a duplicate is only a duplicate email.
//
// The claim is taken before any write and released if the work fails, so a
// retry after a genuine failure still runs. ClaimDelivery, ReleaseDelivery and
// LinkDelivery were all implemented and none of them were called; a replayed
// delivery was reprocessed in full, and the handler's ErrDeliverySeen branch
// was unreachable.
func (s *githubSyncService) HandleWebhook(ctx context.Context, d Delivery) (Outcome, error) {
	p := d.Payload

	// The cheap guards come first, deliberately outside the claim: they touch
	// nothing, so recording a delivery we are going to ignore would fill the
	// log with rows that protect nothing.
	//
	// Our own writes come back as webhooks. Dropping them by sender identity
	// is what stops a comment we posted from being synced back as a new one.
	if s.integrationLogin != "" && strings.EqualFold(p.Sender.Login, s.integrationLogin) {
		return skip("event was sent by the integration account")
	}
	if d.Event != "issues" && d.Event != "issue_comment" {
		return skip("event " + d.Event + " is not handled")
	}

	if err := s.repo.ClaimDelivery(ctx, d.ID, d.Event, p.Action); err != nil {
		// ErrDeliverySeen travels up to the handler, which answers 200 so
		// GitHub stops retrying something already applied.
		return Outcome{}, err
	}

	out, err := s.handleClaimed(ctx, d)
	if err != nil {
		// Release so GitHub's retry can run. A failed release is logged, not
		// returned: the original error is the one worth surfacing, and a
		// stuck claim blocks one delivery rather than corrupting anything.
		if rerr := s.repo.ReleaseDelivery(ctx, d.ID); rerr != nil {
			slog.ErrorContext(ctx, "github: could not release delivery claim",
				"delivery", d.ID, "err", rerr)
		}
		return Outcome{}, err
	}

	if out.ChangeRequestID != "" {
		if lerr := s.repo.LinkDelivery(ctx, d.ID, out.ChangeRequestID); lerr != nil {
			// The work is committed. Failing now would re-run it on GitHub's
			// retry, so losing the audit link is the smaller loss.
			slog.WarnContext(ctx, "github: could not link delivery to change request",
				"delivery", d.ID, "err", lerr)
		}
	}
	return out, nil
}

// handleClaimed is the body of HandleWebhook, run with the delivery claimed.
func (s *githubSyncService) handleClaimed(ctx context.Context, d Delivery) (Outcome, error) {
	p := d.Payload

	// An unmapped repository is one we do not handle. This replaces
	// ServiceNow's separate git.valid.org.list -- the mapping table IS the
	// allow-list, so the two cannot drift apart.
	mapping, err := s.repo.RepoMapping(ctx, p.Repository.Owner.Login, p.Repository.Name)
	if err != nil {
		return Outcome{}, err
	}
	if mapping == nil {
		return skip(fmt.Sprintf("repository %s/%s is not mapped to a product",
			p.Repository.Owner.Login, p.Repository.Name))
	}

	issue, err := github.ParseIssueURL(p.Issue.HTMLURL)
	if err != nil {
		return Outcome{}, err
	}

	cr, err := s.repo.ChangeRequestByGitReference(ctx, p.Issue.HTMLURL)
	if err != nil {
		return Outcome{}, err
	}

	if d.Event == "issue_comment" {
		return s.handleComment(ctx, p, cr)
	}
	return s.handleIssue(ctx, p, issue, cr)
}

// handleComment mirrors a GitHub comment onto the change request.
func (s *githubSyncService) handleComment(ctx context.Context, p IssuePayload, cr *repository.GithubChangeRequest) (Outcome, error) {
	if p.Action != "created" {
		// Edits and deletions do not propagate, matching the original. A
		// comment history that rewrites itself is worse than one that only
		// grows.
		return skip("comment action " + p.Action + " is not mirrored")
	}
	if cr == nil {
		return skip("issue is not linked to a change request")
	}
	if p.Comment == nil {
		return skip("comment payload is absent")
	}
	// The CMD:: protocol ServiceNow defined is deliberately not carried over:
	// its handler was commented out, so no command ever executed, and the
	// comment was swallowed rather than mirrored. Mirroring it is strictly
	// better than the behaviour being replaced.
	if s.mutate == nil {
		return Outcome{Action: "comment_pending_write", ChangeRequestID: cr.ID}, nil
	}
	// Attributed to the GitHub author, so a reader of the change request can
	// see who said it rather than finding it under a service account.
	body := fmt.Sprintf("%s (on GitHub):\n\n%s", p.Comment.User.Login, p.Comment.Body)
	if err := s.mutate.AddComment(ctx, cr.ID, body, p.Comment.User.Login); err != nil {
		return Outcome{}, err
	}
	return Outcome{Action: "comment_relayed", ChangeRequestID: cr.ID}, nil
}

// handleIssue applies an issues event.
func (s *githubSyncService) handleIssue(ctx context.Context, p IssuePayload, issue github.Issue, cr *repository.GithubChangeRequest) (Outcome, error) {
	labels := p.LabelNames()

	switch p.Action {
	case "closed":
		if cr == nil {
			return skip("issue is not linked to a change request")
		}
		// The guard worth keeping: a change request may only be closed from
		// Review. Anything else reopens the issue and says why, rather than
		// letting GitHub drive the record into a state the process forbids.
		if cr.State != stateReview {
			if err := s.gh.SetState(ctx, issue, github.StateOpen); err != nil {
				return Outcome{}, err
			}
			msg := fmt.Sprintf(
				"This change request is in **%s**. It can only be closed from **Review**, so the issue has been reopened.",
				cr.State)
			if err := s.comment(ctx, issue, msg); err != nil {
				return Outcome{}, err
			}
			return Outcome{Action: "close_refused", ChangeRequestID: cr.ID}, nil
		}
		if s.mutate == nil {
			return Outcome{Action: "close_allowed_pending_write", ChangeRequestID: cr.ID}, nil
		}
		if _, err := s.mutate.SetState(ctx, cr.ID, stateClosed); err != nil {
			return Outcome{}, err
		}
		return Outcome{Action: "closed", ChangeRequestID: cr.ID}, nil

	case "labeled", "unlabeled":
		// Guards run before anything else: on a closed change request a label
		// change is refused and put back, which must happen whether or not the
		// issue still satisfies the label gate.
		if cr != nil {
			if done, out, err := s.guardLabelChange(ctx, p, issue, cr, labels); done {
				return out, err
			}
		}
		if !s.labels.Valid(labels) {
			return skip("issue does not carry the change-request label set")
		}
		// A state label moves the change request.
		if cr != nil && p.Action == "labeled" && p.Label != nil {
			if state, ok := s.labels.StateFor(p.Label.Name); ok {
				if s.mutate == nil {
					return Outcome{Action: "state_pending_write", ChangeRequestID: cr.ID}, nil
				}
				changed, err := s.mutate.SetState(ctx, cr.ID, state)
				if err != nil {
					return Outcome{}, err
				}
				if changed {
					return Outcome{Action: "state_changed", ChangeRequestID: cr.ID}, nil
				}
				return skip("already in " + state)
			}
		}
		if cr == nil {
			// Creation is gated on the label set, not on the issue being
			// opened -- opening an issue does not create a change request.
			// ServiceNow reached the same design by commenting out its
			// create-on-open branch; this states it directly.
			if p.Action != "labeled" {
				return skip("a change request is created by labelling, not by " + p.Action)
			}
			return s.prepareCreation(ctx, p, issue, labels)
		}
		return s.applyUpdate(ctx, p, cr, labels)

	case "edited", "opened":
		if !s.labels.Valid(labels) {
			return skip("issue does not carry the change-request label set")
		}
		if cr == nil {
			return skip("a change request is created by labelling, not by " + p.Action)
		}
		return s.applyUpdate(ctx, p, cr, labels)
	}
	return skip("issue action " + p.Action + " is not handled")
}

func (s *githubSyncService) comment(ctx context.Context, issue github.Issue, body string) error {
	if _, err := s.gh.CreateComment(ctx, issue, body); err != nil {
		var apiErr *github.Error
		if errors.As(err, &apiErr) && apiErr.RateLimited() {
			slog.Warn("github: rate limited posting a comment", "status", apiErr.StatusCode)
		}
		return err
	}
	return nil
}

// githubStateLabels are the labels that drive state. They are stripped when a
// change request is created: the record starts at its own initial state, and a
// leftover "Implemented" on the issue would claim otherwise.
//
// Matches ServiceNow's issueStates exactly, including the quirk that
// "Canceled" is NOT in it -- that list stripped Closed but not Canceled, and
// reproducing it keeps a migrated repository looking the same either side.
var githubStateLabels = map[string]bool{
	"Assessed":    true,
	"Authorized":  true,
	"Scheduled":   true,
	"Implemented": true,
	"Reviewed":    true,
	"Closed":      true,
}

// prepareCreation normalises the issue's labels and acknowledges on the issue.
//
// The change request itself is not written yet -- that needs work_item number
// generation and lands with the mutation layer. What IS done here is the part
// that is purely about the issue: reduce the labels to the set the record will
// own, and tell the author their issue has been picked up. Both are idempotent,
// so running this again before the record exists changes nothing.
func (s *githubSyncService) prepareCreation(ctx context.Context, p IssuePayload, issue github.Issue, labels []string) (Outcome, error) {
	resolved := s.labels.ResolveOnCreate(labels)

	// Writing labels is safe here and not on the outbound path: GitHub will
	// send a "labeled" webhook for our own write, and that arrives with the
	// integration account as sender, which HandleWebhook drops.
	if !sameLabels(labels, resolved) {
		if err := s.gh.SetLabels(ctx, issue, resolved); err != nil {
			return Outcome{}, err
		}
	}

	impact, likelihood, crType := s.labels.Attributes(labels)

	if s.mutate == nil {
		msg := fmt.Sprintf(
			"Picked up as a change request.\n\n- **Type**: %s\n- **Impact**: %s\n- **Likelihood**: %s",
			orDash(crType), orDash(impact), orDash(likelihood))
		if err := s.comment(ctx, issue, msg); err != nil {
			return Outcome{}, err
		}
		return Outcome{Action: "creation_prepared"}, nil
	}

	id, number, err := s.mutate.CreateFromIssue(ctx, repository.NewChangeRequestFromIssue{
		Subject:      p.Issue.Title,
		Description:  p.Issue.Body,
		GitReference: p.Issue.HTMLURL,
		Impact:       impact,
		Likelihood:   likelihood,
		Type:         crType,
		CreatedBy:    p.Sender.Login,
	})
	if err != nil {
		// Two deliveries for the same issue can race. The unique reference
		// means the loser finds the record already there, which is the correct
		// end state rather than an error.
		if errors.Is(err, repository.ErrChangeRequestExists) {
			return skip("a change request already exists for this issue")
		}
		return Outcome{}, err
	}

	msg := fmt.Sprintf(
		"Change request **%s** raised.\n\n- **Type**: %s\n- **Impact**: %s\n- **Likelihood**: %s",
		number, orDash(crType), orDash(impact), orDash(likelihood))
	if err := s.comment(ctx, issue, msg); err != nil {
		return Outcome{}, err
	}
	return Outcome{Action: "created", ChangeRequestID: id}, nil
}

func orDash(v string) string {
	if v == "" {
		return "_not set_"
	}
	return v
}

// sameLabels reports whether two label sets are identical as sets, so an
// unchanged set is not written back -- a pointless write would produce a
// webhook we then have to drop.
func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// applyUpdate writes an edited issue onto its change request.
func (s *githubSyncService) applyUpdate(ctx context.Context, p IssuePayload, cr *repository.GithubChangeRequest, labels []string) (Outcome, error) {
	if s.mutate == nil {
		return Outcome{Action: "update_pending_write", ChangeRequestID: cr.ID}, nil
	}
	impact, likelihood, crType := s.labels.Attributes(labels)
	in := repository.NewChangeRequestFromIssue{
		Subject:     p.Issue.Title,
		Description: p.Issue.Body,
		Impact:      impact,
		Likelihood:  likelihood,
		Type:        crType,
		CreatedBy:   p.Sender.Login,
	}
	if err := s.mutate.UpdateFromIssue(ctx, cr.ID, in); err != nil {
		return Outcome{}, err
	}
	return Outcome{Action: "updated", ChangeRequestID: cr.ID}, nil
}

// guardLabelChange enforces what may be changed on an issue whose change
// request is closed, and which labels the record owns rather than the author.
//
// Returns done=true when it handled the event, so the caller stops.
func (s *githubSyncService) guardLabelChange(ctx context.Context, p IssuePayload, issue github.Issue, cr *repository.GithubChangeRequest, labels []string) (bool, Outcome, error) {
	if p.Label == nil {
		return false, Outcome{}, nil
	}
	changed := p.Label.Name

	// A closed change request is a finished record. Label edits are put back
	// and answered, rather than silently ignored -- someone made a change and
	// deserves to know it did not take.
	if cr.State == stateClosed {
		restored := labels
		if p.Action == "labeled" {
			restored = withoutLabel(labels, changed)
		} else {
			restored = append(append([]string{}, labels...), changed)
		}
		if err := s.gh.SetLabels(ctx, issue, restored); err != nil {
			return true, Outcome{}, err
		}
		verb := "added to"
		if p.Action == "unlabeled" {
			verb = "removed from"
		}
		msg := fmt.Sprintf("Labels cannot be %s a **closed** change request, so `%s` has been put back.", verb, changed)
		if err := s.comment(ctx, issue, msg); err != nil {
			return true, Outcome{}, err
		}
		return true, Outcome{Action: "label_change_refused", ChangeRequestID: cr.ID}, nil
	}

	// The type is fixed at creation: a CRType label added afterwards is
	// removed again rather than quietly changing what the record is.
	if p.Action == "labeled" && strings.HasPrefix(changed, s.labels.TypePrefix) {
		if err := s.gh.RemoveLabel(ctx, issue, changed); err != nil {
			return true, Outcome{}, err
		}
		msg := fmt.Sprintf("The change request type is fixed at creation, so `%s` has been removed.", changed)
		if err := s.comment(ctx, issue, msg); err != nil {
			return true, Outcome{}, err
		}
		return true, Outcome{Action: "type_label_reverted", ChangeRequestID: cr.ID}, nil
	}

	// Removing a label the record owns -- its type, or a state -- puts it
	// back: those describe the change request, not the issue.
	if p.Action == "unlabeled" {
		_, isState := s.labels.StateFor(changed)
		if isState || strings.HasPrefix(changed, s.labels.TypePrefix) {
			if err := s.gh.SetLabels(ctx, issue, append(append([]string{}, labels...), changed)); err != nil {
				return true, Outcome{}, err
			}
			return true, Outcome{Action: "protected_label_restored", ChangeRequestID: cr.ID}, nil
		}
	}

	// A new scope label replaces the old one: exactly one scope is allowed,
	// and two would leave change_request_type ambiguous.
	if p.Action == "labeled" && strings.HasPrefix(changed, s.labels.ScopePrefix) {
		reduced := []string{changed}
		for _, l := range labels {
			if l != changed && !strings.HasPrefix(l, s.labels.ScopePrefix) {
				reduced = append(reduced, l)
			}
		}
		if !sameLabels(labels, reduced) {
			if err := s.gh.SetLabels(ctx, issue, reduced); err != nil {
				return true, Outcome{}, err
			}
		}
		if s.mutate != nil {
			if t, ok := s.labels.ScopeToType[changed]; ok {
				if err := s.mutate.UpdateFromIssue(ctx, cr.ID, repository.NewChangeRequestFromIssue{
					Subject: p.Issue.Title, Type: t, CreatedBy: p.Sender.Login,
				}); err != nil {
					return true, Outcome{}, err
				}
			}
		}
		return true, Outcome{Action: "scope_replaced", ChangeRequestID: cr.ID}, nil
	}
	return false, Outcome{}, nil
}

func withoutLabel(labels []string, drop string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l != drop {
			out = append(out, l)
		}
	}
	return out
}
