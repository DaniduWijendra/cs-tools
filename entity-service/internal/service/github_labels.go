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
	"fmt"
	"strings"
)

// GithubLabels is the label vocabulary the sync recognises.
//
// CONFIGURABLE VALUES, FIXED SHAPE. Which strings a repository uses is a
// deployment's business and may differ between dev and production, so every
// one is overridable. What is NOT configurable is the shape: that a type label
// exists, that scope is prefix-grouped, that exactly one scope is allowed. The
// parsing depends on those, and an operator who could change them in config
// would be able to break the code that reads them without any way to find out
// until a webhook arrived.
//
// Defaults are ServiceNow's values verbatim, so an unconfigured deployment
// behaves exactly as the integration being replaced.
type GithubLabels struct {
	// ChangeRequest marks an issue as a change request at all.
	ChangeRequest string
	// TypePrefix groups the type labels (CRType/Normal, CRType/Emergency, ...).
	// Which suffixes exist is the repository's business; the sync only needs
	// to know there is exactly one.
	TypePrefix string
	// ScopePrefix groups the scope labels.
	ScopePrefix string
	// ScopeToType maps a full scope label onto change_request_type.
	ScopeToType map[string]string
	// ImpactByLabel and LikelihoodByLabel map a label onto their enums.
	ImpactByLabel     map[string]string
	LikelihoodByLabel map[string]string
	// StateByLabel maps a label onto change_request_state_enum.
	StateByLabel map[string]string
	// StrippedOnCreate are removed from the issue when the change request is
	// created, because the new record starts at its own initial state.
	StrippedOnCreate map[string]bool
}

// DefaultGithubLabels is ServiceNow's vocabulary, verbatim.
func DefaultGithubLabels() GithubLabels {
	return GithubLabels{
		ChangeRequest: "Type/ChangeRequest",
		TypePrefix:    "CRType/",
		ScopePrefix:   "CRScope/",
		ScopeToType: map[string]string{
			"CRScope/Application":    "GENERAL",
			"CRScope/Infrastructure": "INFRA",
		},
		ImpactByLabel:     map[string]string{"Impact 1": "HIGH", "Impact 2": "MEDIUM"},
		LikelihoodByLabel: map[string]string{"Likelihood 1": "HIGH", "Likelihood 2": "MEDIUM"},
		StateByLabel: map[string]string{
			"Assessed":    "ASSESS",
			"Authorized":  "AUTHORIZE",
			"Scheduled":   "SCHEDULED",
			"Implemented": "IMPLEMENT",
			"Reviewed":    "REVIEW",
		},
		// ServiceNow's issueStates: Closed is stripped, Canceled is not. That
		// asymmetry looks accidental, but reproducing it keeps a migrated
		// repository looking the same either side of the cutover.
		StrippedOnCreate: map[string]bool{
			"Assessed": true, "Authorized": true, "Scheduled": true,
			"Implemented": true, "Reviewed": true, "Closed": true,
		},
	}
}

// GithubLabelOverrides is the flat, single-line configuration Choreo can carry.
// Empty fields keep the ServiceNow default for that entry.
type GithubLabelOverrides struct {
	ChangeRequest string // GITHUB_LABEL_CHANGE_REQUEST
	TypePrefix    string // GITHUB_LABEL_TYPE_PREFIX
	ScopePrefix   string // GITHUB_LABEL_SCOPE_PREFIX
	// Each of these is "label:value,label:value".
	ScopeToType      string // GITHUB_LABELS_SCOPE
	Impact           string // GITHUB_LABELS_IMPACT
	Likelihood       string // GITHUB_LABELS_LIKELIHOOD
	State            string // GITHUB_LABELS_STATE
	StrippedOnCreate string // GITHUB_LABELS_STRIPPED_ON_CREATE  ("a,b,c")
}

// NewGithubLabels applies overrides onto the ServiceNow defaults.
//
// An unparseable override is an error rather than a silent fallback: a typo in
// a label map would otherwise leave the sync quietly recognising nothing, which
// is exactly the failure that took ServiceNow's own integration down for a year
// when git.valid.org.list stopped parsing.
func NewGithubLabels(o GithubLabelOverrides) (GithubLabels, error) {
	l := DefaultGithubLabels()

	if v := strings.TrimSpace(o.ChangeRequest); v != "" {
		l.ChangeRequest = v
	}
	if v := strings.TrimSpace(o.TypePrefix); v != "" {
		l.TypePrefix = v
	}
	if v := strings.TrimSpace(o.ScopePrefix); v != "" {
		l.ScopePrefix = v
	}

	for _, spec := range []struct {
		raw   string
		name  string
		apply func(map[string]string)
	}{
		{o.ScopeToType, "GITHUB_LABELS_SCOPE", func(m map[string]string) { l.ScopeToType = m }},
		{o.Impact, "GITHUB_LABELS_IMPACT", func(m map[string]string) { l.ImpactByLabel = m }},
		{o.Likelihood, "GITHUB_LABELS_LIKELIHOOD", func(m map[string]string) { l.LikelihoodByLabel = m }},
		{o.State, "GITHUB_LABELS_STATE", func(m map[string]string) { l.StateByLabel = m }},
	} {
		if strings.TrimSpace(spec.raw) == "" {
			continue
		}
		m, err := parseLabelMap(spec.raw)
		if err != nil {
			return GithubLabels{}, fmt.Errorf("%s: %w", spec.name, err)
		}
		spec.apply(m)
	}

	if v := strings.TrimSpace(o.StrippedOnCreate); v != "" {
		set := map[string]bool{}
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				set[item] = true
			}
		}
		l.StrippedOnCreate = set
	}
	return l, nil
}

// parseLabelMap reads "label:value,label:value".
//
// Splits on the LAST colon, so a label may contain one: "CRScope/A:B:GENERAL"
// means the label "CRScope/A:B" maps to GENERAL. GitHub allows colons in label
// names and the first-colon reading would silently mangle them.
func parseLabelMap(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		i := strings.LastIndex(pair, ":")
		if i <= 0 || i == len(pair)-1 {
			return nil, fmt.Errorf("%q is not label:value", pair)
		}
		label := strings.TrimSpace(pair[:i])
		value := strings.TrimSpace(pair[i+1:])
		if label == "" || value == "" {
			return nil, fmt.Errorf("%q has an empty label or value", pair)
		}
		out[label] = value
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no entries")
	}
	return out, nil
}

// Valid reports whether an issue's labels mark it as a change request: the
// type label, one type, and exactly one scope.
func (l GithubLabels) Valid(labels []string) bool {
	var hasCR, hasType bool
	scopes := 0
	for _, s := range labels {
		switch {
		case s == l.ChangeRequest:
			hasCR = true
		case strings.HasPrefix(s, l.TypePrefix):
			hasType = true
		case strings.HasPrefix(s, l.ScopePrefix):
			scopes++
		}
	}
	return hasCR && hasType && scopes == 1
}

// Attributes derives the change request's fields from an issue's labels.
func (l GithubLabels) Attributes(labels []string) (impact, likelihood, crType string) {
	for _, s := range labels {
		if v, ok := l.ImpactByLabel[s]; ok {
			impact = v
		}
		if v, ok := l.LikelihoodByLabel[s]; ok {
			likelihood = v
		}
		if v, ok := l.ScopeToType[s]; ok {
			crType = v
		}
	}
	return impact, likelihood, crType
}

// StateFor reports the state a label moves a change request into.
func (l GithubLabels) StateFor(label string) (string, bool) {
	s, ok := l.StateByLabel[label]
	return s, ok
}

// ResolveOnCreate computes the labels an issue should carry once its change
// request exists: one type label first, state labels stripped, everything else
// the author put there kept.
func (l GithubLabels) ResolveOnCreate(issueLabels []string) []string {
	out := make([]string, 0, len(issueLabels))
	seen := map[string]bool{}

	for _, s := range issueLabels {
		if strings.HasPrefix(s, l.TypePrefix) {
			out = append(out, s)
			seen[s] = true
			break
		}
	}
	for _, s := range issueLabels {
		if s == "" || seen[s] || strings.HasPrefix(s, l.TypePrefix) || l.StrippedOnCreate[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// DefaultCommentSkipAuthors are the comment authors whose comments are not
// mirrored to GitHub unless GITHUB_COMMENT_SKIP_AUTHORS overrides the list.
//
// "system" writes the auto-closure reminders. The two integration accounts
// wrote the old ServiceNow-era GitHub sync's own entries; nothing should push
// those back at GitHub, whatever else is decided about machine-written text.
//
// Set GITHUB_COMMENT_SKIP_AUTHORS to an empty-but-present value to mirror
// everything, which is what ServiceNow may have done -- see the type comment
// on githubOutboundService.skipAuthors.
func DefaultCommentSkipAuthors() []string {
	return []string{"system", "github_integration", "github_pipeline"}
}

// DefaultAssignedLabel is the label the outbound sync puts on an issue when
// its case is assigned and removes when the case closes. Taken from the
// GitHub Actions workflow this replaces, which hardcoded it.
// Override with GITHUB_LABEL_STATUS_ASSIGNED; empty disables the behaviour.
const DefaultAssignedLabel = "Status/Assigned"
