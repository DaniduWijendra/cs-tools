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

/**
 * The Phase 2 draft/approval workflow's state machine — see entity-service's
 * CLAUDE.md ("Announcement requests") for the authoritative definition. Every
 * transition is enforced server-side; the UI only reflects it.
 */
export type AnnouncementRequestState = "draft" | "pending_approval" | "approved" | "published";

export type AnnouncementRequestKind = "customer" | "eol";

/**
 * The customer flow's audience shape, mirroring the scope/exclusion fields
 * `CreateCustomerAnnouncementForm`/`AudienceScopeControls` already collect —
 * see that form's `audienceFilters`/`targetProjectIds`. Stored verbatim as
 * `audienceDefinition` (opaque JSONB to the backend) so a draft can be
 * re-opened without re-deriving anything.
 */
export interface CustomerAudienceDefinition {
  scope: "specific" | "all";
  /** Only meaningful when `scope` is `"specific"`. */
  projectIds?: string[];
  /** Only meaningful when `scope` is `"all"` — see `useResolveAnnouncementAudience`'s `AudienceFilters`. */
  excludeClosureStates?: string[];
  excludeSubscriptionTypes?: string[];
}

/** The EOL/product-version flow's audience shape. */
export interface EolAudienceDefinition {
  productId: string;
  productVersionId: string;
}

export type AnnouncementRequestAudienceDefinition =
  | CustomerAudienceDefinition
  | EolAudienceDefinition;

/**
 * Passthrough shape from csm-portal-backend's own `AnnouncementRequest`
 * (itself a passthrough from entity-service) — see that OpenAPI schema's doc
 * comment. `audienceDefinition` is untyped JSON on the wire; callers narrow it
 * by `kind` (`"customer"` → {@link CustomerAudienceDefinition}, `"eol"` →
 * {@link EolAudienceDefinition}).
 */
export interface AnnouncementRequest {
  id: string;
  kind: AnnouncementRequestKind;
  state: AnnouncementRequestState;
  subject: string;
  description: string;
  isSecurityAnnouncement: boolean;
  audienceDefinition: Record<string, unknown>;
  resolvedProjectIds?: string[] | null;
  resolvedProjectCount?: number | null;
  dryRunCaseId?: string | null;
  dryRunAt?: string | null;
  dryRunBy?: string | null;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  submittedBy?: string | null;
  submittedAt?: string | null;
  approvedBy?: string | null;
  approvedAt?: string | null;
  publishedBy?: string | null;
  publishedAt?: string | null;
}

export interface CreateAnnouncementRequestPayload {
  kind: AnnouncementRequestKind;
  subject: string;
  description: string;
  isSecurityAnnouncement: boolean;
  audienceDefinition: AnnouncementRequestAudienceDefinition;
}

/** `additionalProperties: false` server-side — only send fields that actually changed. */
export interface UpdateAnnouncementRequestPayload {
  subject?: string;
  description?: string;
  isSecurityAnnouncement?: boolean;
  audienceDefinition?: AnnouncementRequestAudienceDefinition;
}

export interface RecordAnnouncementRequestDryRunPayload {
  caseId: string;
}

export interface SearchAnnouncementRequestsPayload {
  state?: AnnouncementRequestState;
  createdBy?: string;
  pagination: { offset: number; limit: number };
}

export interface SearchAnnouncementRequestsResponse {
  requests: AnnouncementRequest[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
}
