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

package handler

import "slices"

// issueSortField is GET /issues's `sort` query value. Matches the webapp's
// IssueSortField type (src/api/issueSort.ts) one-for-one.
type issueSortField string

// defaultIssueSort is used when the `sort` query param is absent.
const defaultIssueSort issueSortField = "sla_consumption"

// issueSortColumns is the whitelist of accepted `sort` values, mapped to the
// ORDER BY clause each one runs. This is the only place a new sortable field
// needs to be wired in on the backend — pair it with a new enum value in
// openapi.yaml and a new entry in the webapp's ISSUE_SORT_OPTIONS.
var issueSortColumns = map[issueSortField]string{
	defaultIssueSort: "s.pct_consumed DESC NULLS LAST", // uses the issue_sla.pct_consumed index
	"age":            "i.github_created_at ASC",        // oldest first
}

// validIssueSortValues returns every accepted `sort` value, sorted for a
// deterministic validation-error message.
func validIssueSortValues() []string {
	values := make([]string, 0, len(issueSortColumns))
	for k := range issueSortColumns {
		values = append(values, string(k))
	}
	slices.Sort(values)
	return values
}
