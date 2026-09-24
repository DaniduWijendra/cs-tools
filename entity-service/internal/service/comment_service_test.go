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
	"testing"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/repository"
)

// TestCommentRowToDomain_PrefersResolvedName covers a real bug found live:
// the webapp showed a commenter's raw email ("dinithin@wso2.com") instead of
// their resolved display name, while sibling activity-feed entries (state
// changes, attachments) on the same case already showed a real name --
// because SearchComments never resolved CommentRow.CreatedByName at all, so
// commentRowToDomain always built CreatedBy with an empty Name, and the
// webapp's own authorDisplayName helper falls back to the email whenever
// Name is empty.
func TestCommentRowToDomain_PrefersResolvedName(t *testing.T) {
	row := repository.CommentRow{
		ID:            "c-1",
		WorkItemID:    "wi-1",
		Content:       "hello",
		CreatedBy:     "jane@example.com",
		CreatedByName: "Jane Doe",
	}

	got := commentRowToDomain(row)

	if got.CreatedBy == nil {
		t.Fatal("CreatedBy is nil, want a reference")
	}
	if got.CreatedBy.Name != "Jane Doe" {
		t.Errorf("CreatedBy.Name = %q, want %q", got.CreatedBy.Name, "Jane Doe")
	}
	if got.CreatedBy.Email != "jane@example.com" {
		t.Errorf("CreatedBy.Email = %q, want %q", got.CreatedBy.Email, "jane@example.com")
	}
}

// TestCommentRowToDomain_NoMatchingUserLeavesNameEmpty covers the other half:
// an automation/integration account (e.g. "github_pipeline") has no "user"
// row to resolve against, so SearchComments' LEFT JOIN leaves
// CreatedByName "" -- commentRowToDomain must pass that through as-is, not
// synthesize a name, so the webapp's own email fallback still applies for
// this legitimate case.
func TestCommentRowToDomain_NoMatchingUserLeavesNameEmpty(t *testing.T) {
	row := repository.CommentRow{
		ID:            "c-2",
		WorkItemID:    "wi-1",
		Content:       "automated update",
		CreatedBy:     "github_pipeline",
		CreatedByName: "",
	}

	got := commentRowToDomain(row)

	if got.CreatedBy == nil {
		t.Fatal("CreatedBy is nil, want a reference")
	}
	if got.CreatedBy.Name != "" {
		t.Errorf("CreatedBy.Name = %q, want empty", got.CreatedBy.Name)
	}
	if got.CreatedBy.Email != "github_pipeline" {
		t.Errorf("CreatedBy.Email = %q, want %q", got.CreatedBy.Email, "github_pipeline")
	}
}
