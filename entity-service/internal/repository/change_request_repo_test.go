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

package repository

import (
	"testing"
	"time"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

// TestChangeRequestChangeModelToType covers all eight real
// change_request_change_model_enum labels (migration 000055) -- a coverage
// gap here would let a mistyped or omitted label silently drop a real
// change request's type on read.
func TestChangeRequestChangeModelToType(t *testing.T) {
	want := map[string]domain.ChangeRequestType{
		"AZURE":                domain.ChangeRequestTypeAzure,
		"CHANGE_REGISTRATION":  domain.ChangeRequestTypeChangeRegistration,
		"CLOUD_INFRASTRUCTURE": domain.ChangeRequestTypeCloudInfrastructure,
		"EMERGENCY":            domain.ChangeRequestTypeEmergency,
		"INFRA":                domain.ChangeRequestTypeInfra,
		"NORMAL":               domain.ChangeRequestTypeNormal,
		"STANDARD":             domain.ChangeRequestTypeStandard,
		"UNAUTHORIZED_CHANGE":  domain.ChangeRequestTypeUnauthorizedChange,
	}
	if len(changeRequestChangeModelToType) != len(want) {
		t.Fatalf("changeRequestChangeModelToType has %d entries, want %d", len(changeRequestChangeModelToType), len(want))
	}
	for enumValue, wantType := range want {
		got, ok := changeRequestChangeModelToType[enumValue]
		if !ok {
			t.Errorf("changeRequestChangeModelToType[%q] missing", enumValue)
			continue
		}
		if got != wantType {
			t.Errorf("changeRequestChangeModelToType[%q] = %q, want %q", enumValue, got, wantType)
		}
	}
}

// TestChangeRequestTypeToChangeModel_SupportedTypesRoundTrip proves every
// change-model-backed ChangeRequestType can be written back to its
// original enum label -- this is the map PatchChangeRequest's Type
// handling consults before persisting.
func TestChangeRequestTypeToChangeModel_SupportedTypesRoundTrip(t *testing.T) {
	for enumValue, t1 := range changeRequestChangeModelToType {
		got, ok := changeRequestTypeToChangeModel[t1]
		if !ok {
			t.Errorf("changeRequestTypeToChangeModel[%q] missing (from enum %q)", t1, enumValue)
			continue
		}
		if got != enumValue {
			t.Errorf("changeRequestTypeToChangeModel[%q] = %q, want %q", t1, got, enumValue)
		}
	}
}

// TestChangeRequestTypeToChangeModel_UnsupportedTypesRejected proves the
// two pre-existing ChangeRequestType values that predate change_model
// (migration 000055) have no reverse mapping -- PatchChangeRequest relies
// on this absence to reject them with a ValidationError instead of
// silently writing a wrong or empty change_model value.
func TestChangeRequestTypeToChangeModel_UnsupportedTypesRejected(t *testing.T) {
	for _, unsupported := range []domain.ChangeRequestType{
		domain.ChangeRequestTypeModel,
		domain.ChangeRequestTypeSiteReliabilityOps,
	} {
		if _, ok := changeRequestTypeToChangeModel[unsupported]; ok {
			t.Errorf("changeRequestTypeToChangeModel[%q] unexpectedly present -- PatchChangeRequest would silently accept a type with no real change_model equivalent", unsupported)
		}
	}
}

// fakeChangeRequestDetailRow feeds scanChangeRequestViewAndDetail fixed
// values without a real database connection, exercising the exact Scan
// destination order/types changeRequestSelectColumns+changeRequestDetailColumns
// produce. Zero-value fields are nil pointers, i.e. SQL NULL.
type fakeChangeRequestDetailRow struct {
	id, number                                                         string
	subject, description                                               *string
	projectID, projectName                                             *string
	caseID, caseNumber                                                 *string
	depID, depName                                                     *string
	dpID, dpName                                                       *string
	prodID, prodName                                                   *string
	svcID, svcName                                                     *string
	soID, soName                                                       *string
	aeID, aeName                                                       *string
	startOn, endOn                                                     *time.Time
	impact, state, changeModel                                         *string
	createdOn, updatedOn                                               time.Time
	createdBy                                                          string
	justification, impactDescription, serviceOutage                    *string
	communicationPlan, rollbackPlan, testPlan                          *string
	isCustomerApproved, isCustomerReviewed                             *bool
	implementationPlan, priority, category                             *string
	rbID, rbName                                                       *string
	affectedServicesText, affectedComponentsText, rollbackDurationText *string
	cgID, cgName                                                       *string
	changeRequestType, likelihood                                      *string
	isPlanningVisibleToCustomers                                       *bool
	confirmCustomerUpdatedDate                                         *string
	customerUpdatedOn, workStart, workEnd                              *time.Time
	gitReference                                                       *string
}

func (f fakeChangeRequestDetailRow) Scan(dest ...any) error {
	vals := []any{
		f.id, f.number, f.subject, f.description,
		f.projectID, f.projectName,
		f.caseID, f.caseNumber,
		f.depID, f.depName,
		f.dpID, f.dpName,
		f.prodID, f.prodName,
		f.svcID, f.svcName,
		f.soID, f.soName,
		f.aeID, f.aeName,
		f.startOn, f.endOn, f.impact, f.state, f.changeModel,
		f.createdOn, f.updatedOn,
		f.createdBy, f.justification, f.impactDescription, f.serviceOutage, f.communicationPlan, f.rollbackPlan, f.testPlan,
		f.isCustomerApproved, f.isCustomerReviewed,
		f.implementationPlan, f.priority, f.category,
		f.rbID, f.rbName,
		f.affectedServicesText, f.affectedComponentsText, f.rollbackDurationText,
		f.cgID, f.cgName,
		f.changeRequestType, f.likelihood, f.isPlanningVisibleToCustomers,
		f.confirmCustomerUpdatedDate, f.customerUpdatedOn,
		f.workStart, f.workEnd, f.gitReference,
	}
	if len(dest) != len(vals) {
		panic("fakeChangeRequestDetailRow: dest/vals length mismatch -- update this fake to match scanChangeRequestViewAndDetail's Scan call")
	}
	for i, v := range vals {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case **string:
			*d = v.(*string)
		case *time.Time:
			*d = v.(time.Time)
		case **time.Time:
			*d = v.(*time.Time)
		case **bool:
			*d = v.(*bool)
		default:
			panic("fakeChangeRequestDetailRow: unhandled dest type at index")
		}
	}
	return nil
}

func strPtrCR(s string) *string { return &s }

// TestScanChangeRequestViewAndDetail_FieldParityAdditions is the regression
// guard for the "written but never read back" gap this fix closes:
// CreateChangeRequestFromServiceNow already wrote ImplementationPlan/
// Priority/RequestedBy/AffectedServicesText/AffectedComponentsText/
// RollbackDurationText/CustomerGroup (plus several columns nothing writes
// yet -- ChangeRequestType/Likelihood/IsPlanningVisibleToCustomers/
// ConfirmCustomerUpdatedDate/CustomerUpdatedOn/WorkStart/WorkEnd/
// GitReference), but GetChangeRequestByID never selected any of them back.
func TestScanChangeRequestViewAndDetail_FieldParityAdditions(t *testing.T) {
	now := time.Now()

	t.Run("all field-parity columns populated", func(t *testing.T) {
		row := fakeChangeRequestDetailRow{
			id: "CR-1", number: "CHG0001", subject: strPtrCR("s"), description: strPtrCR("d"),
			createdOn: now, updatedOn: now, createdBy: "actor@wso2.com",
			implementationPlan: strPtrCR("do the thing"), priority: strPtrCR("HIGH"), category: strPtrCR("SOFTWARE"),
			rbID: strPtrCR("user-1"), rbName: strPtrCR("Jane Doe"),
			affectedServicesText: strPtrCR("svc-a"), affectedComponentsText: strPtrCR("comp-a"), rollbackDurationText: strPtrCR("2h"),
			cgID: strPtrCR("group-1"), cgName: strPtrCR("SRE Team"),
			changeRequestType: strPtrCR("INFRA"), likelihood: strPtrCR("HIGH"),
			isPlanningVisibleToCustomers: boolPtrCR(true),
			confirmCustomerUpdatedDate:   strPtrCR("AGREE"),
			customerUpdatedOn:            &now, workStart: &now, workEnd: &now,
			gitReference: strPtrCR("main@abc123"),
		}
		var cr domain.ChangeRequest
		if err := scanChangeRequestViewAndDetail(row, &cr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cr.ImplementationPlan == nil || *cr.ImplementationPlan != "do the thing" {
			t.Errorf("ImplementationPlan = %v, want \"do the thing\"", cr.ImplementationPlan)
		}
		if cr.Priority == nil || *cr.Priority != "high" {
			t.Errorf("Priority = %v, want \"high\" (lower-cased)", cr.Priority)
		}
		if cr.Category == nil || *cr.Category != "software" {
			t.Errorf("Category = %v, want \"software\" (lower-cased)", cr.Category)
		}
		if cr.RequestedBy == nil || cr.RequestedBy.ID != "user-1" || cr.RequestedBy.Name != "Jane Doe" {
			t.Errorf("RequestedBy = %+v, want {user-1 Jane Doe}", cr.RequestedBy)
		}
		if cr.CustomerGroup == nil || cr.CustomerGroup.ID != "group-1" || cr.CustomerGroup.Name != "SRE Team" {
			t.Errorf("CustomerGroup = %+v, want {group-1 SRE Team}", cr.CustomerGroup)
		}
		if cr.AffectedServicesText == nil || *cr.AffectedServicesText != "svc-a" {
			t.Errorf("AffectedServicesText = %v, want \"svc-a\"", cr.AffectedServicesText)
		}
		if cr.ChangeRequestType == nil || *cr.ChangeRequestType != "infra" {
			t.Errorf("ChangeRequestType = %v, want \"infra\"", cr.ChangeRequestType)
		}
		if cr.Likelihood == nil || *cr.Likelihood != "high" {
			t.Errorf("Likelihood = %v, want \"high\"", cr.Likelihood)
		}
		if !cr.IsPlanningVisibleToCustomers {
			t.Error("IsPlanningVisibleToCustomers = false, want true")
		}
		if cr.ConfirmCustomerUpdatedDate == nil || *cr.ConfirmCustomerUpdatedDate != "agree" {
			t.Errorf("ConfirmCustomerUpdatedDate = %v, want \"agree\"", cr.ConfirmCustomerUpdatedDate)
		}
		if cr.CustomerUpdatedOn == nil || cr.WorkStart == nil || cr.WorkEnd == nil {
			t.Errorf("CustomerUpdatedOn/WorkStart/WorkEnd unexpectedly nil: %v %v %v", cr.CustomerUpdatedOn, cr.WorkStart, cr.WorkEnd)
		}
		if cr.GitReference == nil || *cr.GitReference != "main@abc123" {
			t.Errorf("GitReference = %v, want \"main@abc123\"", cr.GitReference)
		}
	})

	t.Run("NULL field-parity columns (no RequestedBy/CustomerGroup, nothing written yet) do not error", func(t *testing.T) {
		// Regression: RequestedBy/CustomerGroup come from LEFT JOINs
		// (changeRequestDetailJoins) that can legitimately match no row,
		// and every other new column is nullable -- none of this should
		// panic the way a non-pointer scan destination would.
		row := fakeChangeRequestDetailRow{
			id: "CR-2", number: "CHG0002", subject: strPtrCR("s"), description: strPtrCR("d"),
			createdOn: now, updatedOn: now, createdBy: "actor@wso2.com",
		}
		var cr domain.ChangeRequest
		if err := scanChangeRequestViewAndDetail(row, &cr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cr.RequestedBy != nil || cr.CustomerGroup != nil {
			t.Errorf("RequestedBy/CustomerGroup = %+v/%+v, want both nil", cr.RequestedBy, cr.CustomerGroup)
		}
		if cr.ImplementationPlan != nil || cr.Priority != nil || cr.GitReference != nil {
			t.Errorf("expected NULL field-parity columns to stay nil, got ImplementationPlan=%v Priority=%v GitReference=%v",
				cr.ImplementationPlan, cr.Priority, cr.GitReference)
		}
	})
}

func boolPtrCR(b bool) *bool { return &b }
