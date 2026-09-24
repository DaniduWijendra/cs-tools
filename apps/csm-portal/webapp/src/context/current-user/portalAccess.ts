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
 * Portal roles as returned in `GET /users/me`'s `roles` (stable keys, not the
 * IdP role names). A user can hold several.
 */
export const PORTAL_ROLE = {
  viewer: "viewer",
  escalator: "escalator",
  attachmentDownloader: "attachment_downloader",
  supportEngineer: "support_engineer",
  usageMetricsViewer: "usage_metrics_viewer",
  timecardApprover: "timecard_approver",
  dashboardDesigner: "dashboard_designer",
  admin: "admin",
} as const;

const ALL_PORTAL_ROLES: readonly string[] = Object.values(PORTAL_ROLE);

export interface PortalAccess {
  /** Holds at least one portal role — the minimum to use the portal at all. */
  hasAnyRole: boolean;
  canEscalate: boolean;
  canDownloadAttachment: boolean;
  /**
   * The Operations section (incidents, change requests, problems, outages,
   * service requests). Support-portal-lite had no such area, so its
   * view-oriented roles never saw one: only full-access roles do here.
   */
  canUseOperations: boolean;
  /**
   * The Time Cards and Updates sections, and every time-card and update-level
   * call behind them. Full-access roles have it, and so does the time-card
   * approver, so approving does not require being a support engineer.
   */
  canUseTimeCardsAndUpdates: boolean;
  /** Every other state-changing action (create/update cases, tasks, ...). */
  canWrite: boolean;
  /**
   * Creating a new platform user. Unlike every other flag here, this is
   * `admin` only — `support_engineer` does not hold it, mirroring the
   * backend's `PermAdmin` (the one permission `support_engineer` does not
   * share with `admin`).
   */
  canCreateUser: boolean;
}

/**
 * What a user's `GET /users/me` roles let them see and do. Matched
 * case-insensitively. `admin` and `support_engineer` can do everything;
 * `escalator` and `attachment_downloader` each add just that one ability; every other role is view-only here.
 *
 * Mirrors the backend's `AccessGuard` policy so controls can be hidden up
 * front — but it is a UX affordance only. The backend's 403 is the real gate,
 * so if the two ever disagree the backend wins. `undefined` roles (profile not
 * loaded, or the request failed) grant nothing, so controls fail closed.
 */
export function getPortalAccess(roles: string[] | undefined): PortalAccess {
  const held = new Set((roles ?? []).map((r) => r.toLowerCase()));
  const has = (role: string): boolean => held.has(role);
  const full = has(PORTAL_ROLE.admin) || has(PORTAL_ROLE.supportEngineer);
  return {
    hasAnyRole: ALL_PORTAL_ROLES.some(has),
    canEscalate: full || has(PORTAL_ROLE.escalator),
    canDownloadAttachment: full || has(PORTAL_ROLE.attachmentDownloader),
    canUseOperations: full,
    canUseTimeCardsAndUpdates: full || has(PORTAL_ROLE.timecardApprover),
    canWrite: full,
    canCreateUser: has(PORTAL_ROLE.admin),
  };
}
