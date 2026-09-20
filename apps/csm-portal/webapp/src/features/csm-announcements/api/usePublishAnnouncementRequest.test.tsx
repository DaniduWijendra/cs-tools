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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const postEmptyMock = vi.fn();
const showErrorMock = vi.fn();
const postCaseMutateAsyncMock = vi.fn();
const addTagMutateAsyncMock = vi.fn();

vi.mock("@api/backend/client", () => ({
  useBackendApi: () => ({ postEmpty: postEmptyMock }),
}));
vi.mock("@context/error-banner/ErrorBannerContext", () => ({
  useErrorBanner: () => ({ showError: showErrorMock }),
}));
vi.mock("@features/csm-cases/api/usePostCsmCase", () => ({
  usePostCsmCase: () => ({ mutateAsync: postCaseMutateAsyncMock }),
}));
vi.mock("@features/csm-cases/api/useCaseTags", () => ({
  useAddTagToCase: () => ({ mutateAsync: addTagMutateAsyncMock }),
}));

// Imported after the mocks above so the module picks them up.
import { usePublishAnnouncementRequest } from "@features/csm-announcements/api/usePublishAnnouncementRequest";
import type { AnnouncementRequest } from "@features/csm-announcements/types/announcementRequests";

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

const APPROVED_REQUEST: AnnouncementRequest = {
  id: "req-1",
  kind: "customer",
  state: "approved",
  subject: "Scheduled maintenance",
  description: "<p>Details</p>",
  isSecurityAnnouncement: false,
  audienceDefinition: { scope: "specific", projectIds: ["p-1", "p-2"] },
  resolvedProjectIds: ["p-1", "p-2"],
  resolvedProjectCount: 2,
  createdBy: "jane@example.com",
  createdAt: "2026-07-01T10:00:00Z",
  updatedAt: "2026-07-01T10:00:00Z",
};

beforeEach(() => {
  postEmptyMock.mockReset();
  showErrorMock.mockReset();
  postCaseMutateAsyncMock.mockReset();
  addTagMutateAsyncMock.mockReset();
});

describe("usePublishAnnouncementRequest — guards", () => {
  it("does nothing when the request isn't approved", async () => {
    const { result } = renderHook(
      () => usePublishAnnouncementRequest({ ...APPROVED_REQUEST, state: "draft" }),
      { wrapper },
    );
    await act(async () => {
      await result.current.handlePublish();
    });
    expect(postCaseMutateAsyncMock).not.toHaveBeenCalled();
  });

  it("shows an error and sends nothing when there's no resolved audience", async () => {
    const { result } = renderHook(
      () => usePublishAnnouncementRequest({ ...APPROVED_REQUEST, resolvedProjectIds: [] }),
      { wrapper },
    );
    await act(async () => {
      await result.current.handlePublish();
    });
    expect(postCaseMutateAsyncMock).not.toHaveBeenCalled();
    expect(showErrorMock).toHaveBeenCalledWith(expect.stringMatching(/no resolved audience/i));
  });
});

describe("usePublishAnnouncementRequest — full success", () => {
  it("creates one case per resolved project, then marks the request published", async () => {
    postCaseMutateAsyncMock.mockImplementation(({ projectId }: { projectId: string }) =>
      Promise.resolve({ id: `case-${projectId}`, internalId: "X-1", number: "N-1" }),
    );
    postEmptyMock.mockResolvedValue({ ...APPROVED_REQUEST, state: "published" });

    const { result } = renderHook(() => usePublishAnnouncementRequest(APPROVED_REQUEST), { wrapper });
    await act(async () => {
      await result.current.handlePublish();
    });

    expect(postCaseMutateAsyncMock).toHaveBeenCalledTimes(2);
    expect(postCaseMutateAsyncMock).toHaveBeenCalledWith(
      expect.objectContaining({ type: "announcement", projectId: "p-1", subject: "Scheduled maintenance" }),
    );
    expect(postEmptyMock).toHaveBeenCalledWith("/announcement-requests/req-1/publish");
    expect(result.current.published?.state).toBe("published");
    expect(result.current.failedProjectIds).toEqual([]);
    expect(result.current.succeededProjectIds.sort()).toEqual(["p-1", "p-2"]);
  });

  it("attaches the security tag per case when isSecurityAnnouncement is set", async () => {
    postCaseMutateAsyncMock.mockResolvedValue({ id: "case-1", internalId: "X-1", number: "N-1" });
    postEmptyMock.mockResolvedValue({ ...APPROVED_REQUEST, state: "published" });
    addTagMutateAsyncMock.mockResolvedValue({});

    const { result } = renderHook(
      () =>
        usePublishAnnouncementRequest({
          ...APPROVED_REQUEST,
          isSecurityAnnouncement: true,
          resolvedProjectIds: ["p-1"],
        }),
      { wrapper },
    );
    await act(async () => {
      await result.current.handlePublish();
    });

    expect(addTagMutateAsyncMock).toHaveBeenCalledWith({ caseId: "case-1", label: "Security Announcement" });
    expect(result.current.failedTagProjectIds).toEqual([]);
  });
});

describe("usePublishAnnouncementRequest — partial failure and retry", () => {
  it("keeps the succeeded project and only reports the failed one, without marking published", async () => {
    postCaseMutateAsyncMock.mockImplementation(({ projectId }: { projectId: string }) =>
      projectId === "p-1"
        ? Promise.resolve({ id: "case-p-1", internalId: "X-1", number: "N-1" })
        : Promise.reject(new Error("boom")),
    );

    const { result } = renderHook(() => usePublishAnnouncementRequest(APPROVED_REQUEST), { wrapper });
    await act(async () => {
      await result.current.handlePublish();
    });

    expect(result.current.succeededProjectIds).toEqual(["p-1"]);
    expect(result.current.failedProjectIds).toEqual(["p-2"]);
    expect(result.current.published).toBeNull();
    expect(postEmptyMock).not.toHaveBeenCalled();
    expect(showErrorMock).toHaveBeenCalledWith(expect.stringMatching(/failed for project p-2/i));
  });

  it("retrying only resends to the previously-failed project, not the one that already succeeded", async () => {
    postCaseMutateAsyncMock.mockImplementation(({ projectId }: { projectId: string }) =>
      projectId === "p-1"
        ? Promise.resolve({ id: "case-p-1", internalId: "X-1", number: "N-1" })
        : Promise.reject(new Error("boom")),
    );

    const { result } = renderHook(() => usePublishAnnouncementRequest(APPROVED_REQUEST), { wrapper });
    await act(async () => {
      await result.current.handlePublish();
    });
    expect(postCaseMutateAsyncMock).toHaveBeenCalledTimes(2);
    postCaseMutateAsyncMock.mockClear();

    // Fix the failing project and retry.
    postCaseMutateAsyncMock.mockResolvedValue({ id: "case-p-2", internalId: "X-2", number: "N-2" });
    postEmptyMock.mockResolvedValue({ ...APPROVED_REQUEST, state: "published" });

    await act(async () => {
      await result.current.handlePublish();
    });

    // Only the previously-failed project is resent — p-1 isn't duplicated.
    expect(postCaseMutateAsyncMock).toHaveBeenCalledTimes(1);
    expect(postCaseMutateAsyncMock).toHaveBeenCalledWith(expect.objectContaining({ projectId: "p-2" }));
    expect(result.current.failedProjectIds).toEqual([]);
    expect(result.current.succeededProjectIds.sort()).toEqual(["p-1", "p-2"]);
    expect(postEmptyMock).toHaveBeenCalledTimes(1);
  });

  it("retries the publish call (without resending any case) when every project already succeeded but marking published failed last time", async () => {
    postCaseMutateAsyncMock.mockResolvedValue({ id: "case-1", internalId: "X-1", number: "N-1" });
    postEmptyMock.mockRejectedValueOnce(new Error("db down"));

    const { result } = renderHook(() => usePublishAnnouncementRequest(APPROVED_REQUEST), { wrapper });
    await act(async () => {
      await result.current.handlePublish();
    });
    // Every project succeeded, but the bookkeeping publish call itself failed.
    expect(result.current.succeededProjectIds.sort()).toEqual(["p-1", "p-2"]);
    expect(result.current.failedProjectIds).toEqual([]);
    expect(result.current.published).toBeNull();
    postCaseMutateAsyncMock.mockClear();

    postEmptyMock.mockResolvedValueOnce({ ...APPROVED_REQUEST, state: "published" });
    await act(async () => {
      await result.current.handlePublish();
    });

    // No case is ever recreated — only the publish call itself is retried.
    expect(postCaseMutateAsyncMock).not.toHaveBeenCalled();
    expect(postEmptyMock).toHaveBeenCalledTimes(2);
    expect(result.current.published?.state).toBe("published");
  });
});
