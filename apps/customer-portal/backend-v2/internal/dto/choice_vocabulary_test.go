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

package dto

import "testing"

func intPtr(n int) *int { return &n }

// The dashboard's outstanding-cases chart matches severity buckets on the
// LABEL, against "Catastrophic (P0)" .. "Low (P4)"
// (features/dashboard/constants/dashboard.ts, SEVERITY_LEGEND_ORDER). The
// Postgres data source returns the raw enum label as both id and label, so
// without this translation every bucket misses and the chart renders empty
// beside a non-zero outstanding count.
func TestNormalizeCaseSeverityChoices_PostgresEnumLabels(t *testing.T) {
	in := []ReferenceItem{
		{ID: "S0", Label: "S0", Count: intPtr(1)},
		{ID: "S1", Label: "S1", Count: intPtr(7)},
		{ID: "S2", Label: "S2", Count: intPtr(0)},
		{ID: "S3", Label: "S3", Count: intPtr(4)},
		{ID: "S4", Label: "S4", Count: intPtr(11)},
	}
	want := []ReferenceItem{
		{ID: "14", Label: "Catastrophic (P0)", Count: intPtr(1)},
		{ID: "10", Label: "Critical (P1)", Count: intPtr(7)},
		{ID: "11", Label: "High (P2)", Count: intPtr(0)},
		{ID: "12", Label: "Medium (P3)", Count: intPtr(4)},
		{ID: "13", Label: "Low (P4)", Count: intPtr(11)},
	}

	got := normalizeCaseSeverityChoices(in)
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Label != want[i].Label {
			t.Errorf("[%d] = {%s, %s}, want {%s, %s}", i, got[i].ID, got[i].Label, want[i].ID, want[i].Label)
		}
		if got[i].Count == nil || *got[i].Count != *want[i].Count {
			t.Errorf("[%d] count not preserved", i)
		}
	}
}

// entity-service's own documented contract is the lowercase domain enum, so
// that spelling has to resolve too.
func TestNormalizeCaseSeverityChoices_DomainEnums(t *testing.T) {
	got := normalizeCaseSeverityChoices([]ReferenceItem{{ID: "critical", Label: "critical"}})
	if got[0].ID != "10" || got[0].Label != "Critical (P1)" {
		t.Errorf("got {%s, %s}, want {10, Critical (P1)}", got[0].ID, got[0].Label)
	}
}

// ServiceNow already sends what the frontend wants. Translating it again would
// break the data source that works today, so an unrecognised id passes through
// untouched.
func TestNormalizeCaseSeverityChoices_ServiceNowIDsPassThrough(t *testing.T) {
	in := []ReferenceItem{
		{ID: "10", Label: "Critical (P1)", Count: intPtr(3)},
		{ID: "999", Label: "Something Unknown"},
	}
	got := normalizeCaseSeverityChoices(in)
	for i := range in {
		if got[i].ID != in[i].ID || got[i].Label != in[i].Label {
			t.Errorf("[%d] = {%s, %s}, want it unchanged {%s, %s}",
				i, got[i].ID, got[i].Label, in[i].ID, in[i].Label)
		}
	}
}

// The cases table derives its status filter with Number(status.id)
// (features/dashboard/utils/casesTable.ts). A non-numeric id becomes NaN,
// which serialises to null and silently drops the filter, so these ids have
// to come back numeric.
func TestNormalizeCaseStateChoices_PostgresEnumLabels(t *testing.T) {
	in := []ReferenceItem{
		{ID: "OPEN", Label: "OPEN"},
		{ID: "WORK_IN_PROGRESS", Label: "WORK_IN_PROGRESS"},
		{ID: "WAITING_ON_WSO2", Label: "WAITING_ON_WSO2"},
		{ID: "AWAITING_INFO", Label: "AWAITING_INFO"},
		{ID: "REOPENED", Label: "REOPENED"},
		{ID: "SOLUTION_PROPOSED", Label: "SOLUTION_PROPOSED"},
		{ID: "CLOSED", Label: "CLOSED"},
	}
	want := []ReferenceItem{
		{ID: "1", Label: "Open"},
		{ID: "10", Label: "Work In Progress"},
		{ID: "1003", Label: "Waiting On WSO2"},
		{ID: "18", Label: "Awaiting Info"},
		{ID: "1006", Label: "Reopened"},
		{ID: "6", Label: "Solution Proposed"},
		{ID: "3", Label: "Closed"},
	}

	got := normalizeCaseStateChoices(in)
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Label != want[i].Label {
			t.Errorf("[%d] = {%s, %s}, want {%s, %s}", i, got[i].ID, got[i].Label, want[i].ID, want[i].Label)
		}
	}
}

// The closed state must keep the exact label the table filters on when it
// builds its default "outstanding" status set.
func TestNormalizeCaseStateChoices_ClosedLabelIsRecognisable(t *testing.T) {
	got := normalizeCaseStateChoices([]ReferenceItem{{ID: "CLOSED", Label: "CLOSED"}})
	if got[0].Label != "Closed" {
		t.Errorf("closed label = %q, want %q", got[0].Label, "Closed")
	}
}

func TestNormalizeChoices_EmptyInputIsEmptyNotNil(t *testing.T) {
	if got := normalizeCaseSeverityChoices(nil); got == nil {
		t.Error("nil input must produce an empty slice, not nil -- it serialises as [] not null")
	}
}
