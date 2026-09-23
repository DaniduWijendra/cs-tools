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
	"strings"
	"time"

	"github.com/wso2-open-operations/cs-tools/entity-service/internal/apierror"
	"github.com/wso2-open-operations/cs-tools/entity-service/internal/domain"
)

// Salesforce Project_Contact__c roles (Role__c multi-picklist labels) the
// ingest understands. Anything else is reported back as ignored.
const (
	sfRolePortalUser      = "portal user"
	sfRoleSecurityContact = "security contact"
	sfRoleLead            = "lead"
	sfRoleAdmin           = "admin"
)

// Global role.name values (seeded by the ServiceNow sync) the ingest grants.
const (
	globalRoleExternal      = "external"
	globalRoleCustomer      = "customer"
	globalRolePartner       = "partner"
	globalRoleCustomerAdmin = "customer_admin"
	globalRolePartnerAdmin  = "partner_admin"
)

// project_group."group" values the ingest maps memberships to.
const (
	projectGroupFullAccess    = "Full Access"
	projectGroupGeneralAccess = "General Access"
	projectGroupSecurityOnly  = "Security Only"
	projectGroupLeadUserGroup = "Lead User Group"
)

// managedAdminRoles are the global roles the ingest owns exclusively: it
// grants them when the Salesforce record says admin and revokes them when it
// no longer does. Every other role a user holds is left alone.
var managedAdminRoles = []string{globalRoleCustomerAdmin, globalRolePartnerAdmin}

// salesforceLastModifiedLayout is how sales-entity-service renders Salesforce
// datetimes, e.g. 2026-09-18T06:37:07.000+0000.
const salesforceLastModifiedLayout = "2006-01-02T15:04:05.000-0700"

// membershipStateAliases folds the spellings Salesforce may use for the
// re-invited state onto project_contact_state_enum's own label.
var membershipStateAliases = map[string]string{
	"RE_INVITED": domain.MembershipStateReInvited,
	"REINVITED":  domain.MembershipStateReInvited,
}

var validMembershipState = map[string]bool{
	domain.MembershipStateInvited:     true,
	domain.MembershipStateRegistered:  true,
	domain.MembershipStateReInvited:   true,
	domain.MembershipStateDeactivated: true,
}

// normalizeMembershipState maps a Salesforce State__c value onto
// project_contact_state_enum. An unknown value is a ValidationError: writing
// it would fail the enum cast anyway, and a 400 tells the replayer exactly
// which record is malformed instead of a generic 500.
func normalizeMembershipState(raw string) (string, error) {
	state := strings.ToUpper(strings.TrimSpace(raw))
	if alias, ok := membershipStateAliases[state]; ok {
		state = alias
	}
	if state == "" {
		return "", &apierror.ValidationError{Msg: "project contact state is required"}
	}
	if !validMembershipState[state] {
		return "", &apierror.ValidationError{Msg: "project contact state " + state + " is not one of INVITED, REGISTERED, RE-INVITED, DEACTIVATED"}
	}
	return state, nil
}

// splitSalesforceRoles turns a Role__c multi-picklist string ("Admin;Portal
// user") into its trimmed parts. Both ';' (Salesforce's own separator) and
// ',' are accepted.
func splitSalesforceRoles(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// hasSalesforceRole reports whether roles contains want (case-insensitive,
// whitespace-insensitive).
func hasSalesforceRole(roles []string, want string) bool {
	for _, r := range roles {
		if strings.EqualFold(strings.TrimSpace(r), want) {
			return true
		}
	}
	return false
}

// mapGlobalRoles derives the role.name values a membership grants (§6.4):
// every contact is `external`; a PARTNER CONTACT is `partner`, anything else
// `customer`; `customer_admin`/`partner_admin` follow the contact's isCsAdmin
// flag or an Admin role on the membership. An integration user gets no
// global roles at all (it never signs in), and none are revoked either.
func mapGlobalRoles(membershipType string, isCsAdmin bool, roles []string, isIntegrationUser bool) (grant, managed []string) {
	if isIntegrationUser {
		return nil, nil
	}
	partner := strings.EqualFold(strings.TrimSpace(membershipType), domain.MembershipTypePartnerContact)
	grant = []string{globalRoleExternal}
	if partner {
		grant = append(grant, globalRolePartner)
	} else {
		grant = append(grant, globalRoleCustomer)
	}
	if isCsAdmin || hasSalesforceRole(roles, sfRoleAdmin) {
		if partner {
			grant = append(grant, globalRolePartnerAdmin)
		} else {
			grant = append(grant, globalRoleCustomerAdmin)
		}
	}
	return grant, managedAdminRoles
}

// mapProjectGroups derives the project_group."group" set for a membership's
// Salesforce roles (§6.4). Portal user + Security Contact is Full Access,
// Portal user alone General Access, Security Contact alone Security Only; a
// Lead additionally joins Lead User Group. Admin only affects global roles.
// Roles the mapping does not know are returned in ignored so the caller can
// log them; they never fail the ingest.
func mapProjectGroups(roles []string) (groups, ignored []string) {
	var portal, security, lead bool
	for _, raw := range roles {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case sfRolePortalUser:
			portal = true
		case sfRoleSecurityContact:
			security = true
		case sfRoleLead:
			lead = true
		case sfRoleAdmin, "":
			// global role only / blank
		default:
			ignored = append(ignored, strings.TrimSpace(raw))
		}
	}
	switch {
	case portal && security:
		groups = append(groups, projectGroupFullAccess)
	case portal:
		groups = append(groups, projectGroupGeneralAccess)
	case security:
		groups = append(groups, projectGroupSecurityOnly)
	}
	if lead {
		groups = append(groups, projectGroupLeadUserGroup)
	}
	return groups, ignored
}

// parseSalesforceLastModified parses sales-entity-service's rendering of
// LastModifiedDate. RFC3339 is accepted as well. ok is false when the value
// is absent or unparseable; the caller then skips the duplicate-event guard
// rather than failing the ingest.
func parseSalesforceLastModified(raw *string) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}
	v := strings.TrimSpace(*raw)
	if v == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{salesforceLastModifiedLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
