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
import { describe, expect, it } from "vitest";
import { getPortalAccess } from "@context/current-user/portalAccess";

const NONE = {
  hasAnyRole: false,
  canEscalate: false,
  canDownloadAttachment: false,
  canUseOperations: false,
  canUseTimeCardsAndUpdates: false,
  canWrite: false,
};

describe("getPortalAccess", () => {
  it("grants nothing when roles are missing or empty", () => {
    expect(getPortalAccess(undefined)).toEqual(NONE);
    expect(getPortalAccess([])).toEqual(NONE);
  });

  it("ignores roles the portal does not know", () => {
    expect(getPortalAccess(["internal", "customer", "agent"])).toEqual(NONE);
  });

  it("viewer can use the portal but do nothing else", () => {
    expect(getPortalAccess(["viewer"])).toEqual({ ...NONE, hasAnyRole: true });
  });

  it("each specialised role adds only its own ability", () => {
    expect(getPortalAccess(["escalator"])).toEqual({ ...NONE, hasAnyRole: true, canEscalate: true });
    expect(getPortalAccess(["attachment_downloader"])).toEqual({
      ...NONE,
      hasAnyRole: true,
      canDownloadAttachment: true,
    });
  });

  it("the feature roles are view-only for these controls", () => {
    for (const role of ["usage_metrics_viewer", "dashboard_designer"]) {
      expect(getPortalAccess([role])).toEqual({ ...NONE, hasAnyRole: true });
    }
  });

  it("the time-card approver also gets Time cards and Updates, and nothing else", () => {
    expect(getPortalAccess(["timecard_approver"])).toEqual({
      ...NONE,
      hasAnyRole: true,
      canUseTimeCardsAndUpdates: true,
    });
  });

  it("support engineer and admin can do everything", () => {
    const all = {
      hasAnyRole: true,
      canEscalate: true,
      canDownloadAttachment: true,
      canUseOperations: true,
      canUseTimeCardsAndUpdates: true,
      canWrite: true,
    };
    expect(getPortalAccess(["support_engineer"])).toEqual(all);
    expect(getPortalAccess(["admin"])).toEqual(all);
  });

  it("only support engineer, admin and the time-card approver get Time cards and Updates", () => {
    for (const role of [
      "viewer",
      "escalator",
      "attachment_downloader",
      "usage_metrics_viewer",
      "dashboard_designer",
    ]) {
      expect(getPortalAccess([role]).canUseTimeCardsAndUpdates).toBe(false);
    }
    expect(getPortalAccess(["viewer", "escalator"]).canUseTimeCardsAndUpdates).toBe(false);
    expect(getPortalAccess(["viewer", "timecard_approver"]).canUseTimeCardsAndUpdates).toBe(true);
    expect(getPortalAccess(["support_engineer"]).canUseTimeCardsAndUpdates).toBe(true);
    expect(getPortalAccess(["admin"]).canUseTimeCardsAndUpdates).toBe(true);
  });

  it("only full-access roles get the Operations section", () => {
    for (const role of [
      "viewer",
      "escalator",
      "attachment_downloader",
      "usage_metrics_viewer",
      "timecard_approver",
      "dashboard_designer",
    ]) {
      expect(getPortalAccess([role]).canUseOperations).toBe(false);
    }
    expect(getPortalAccess(["viewer", "escalator", "attachment_downloader"]).canUseOperations).toBe(false);
    expect(getPortalAccess(["support_engineer"]).canUseOperations).toBe(true);
    expect(getPortalAccess(["admin"]).canUseOperations).toBe(true);
  });

  it("a user holding several roles gets the union", () => {
    expect(getPortalAccess(["viewer", "escalator", "attachment_downloader"])).toEqual({
      ...NONE,
      hasAnyRole: true,
      canEscalate: true,
      canDownloadAttachment: true,
    });
  });

  it("matches role keys case-insensitively", () => {
    expect(getPortalAccess(["Support_Engineer"]).canWrite).toBe(true);
  });
});
