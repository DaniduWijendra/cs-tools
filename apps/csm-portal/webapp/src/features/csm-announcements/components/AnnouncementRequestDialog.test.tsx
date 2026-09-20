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

import type { ReactElement } from "react";
import { fireEvent, render as rtlRender, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import "@testing-library/jest-dom/vitest";
import AnnouncementRequestDialog from "@features/csm-announcements/components/AnnouncementRequestDialog";
import { useGetAnnouncementRequest } from "@features/csm-announcements/api/useGetAnnouncementRequest";
import { useUpdateAnnouncementRequest } from "@features/csm-announcements/api/useUpdateAnnouncementRequest";
import { useRecordAnnouncementRequestDryRun } from "@features/csm-announcements/api/useRecordAnnouncementRequestDryRun";
import { useSubmitAnnouncementRequest } from "@features/csm-announcements/api/useSubmitAnnouncementRequest";
import { useApproveAnnouncementRequest } from "@features/csm-announcements/api/useApproveAnnouncementRequest";
import { usePublishAnnouncementRequest } from "@features/csm-announcements/api/usePublishAnnouncementRequest";
import { useAnnouncementDryRun } from "@features/csm-announcements/api/useAnnouncementDryRun";
import type { AnnouncementRequest } from "@features/csm-announcements/types/announcementRequests";

vi.mock("@api/backend/client", () => ({
  BackendApiError: class BackendApiError extends Error {},
  useBackendApi: () => ({ post: vi.fn(), postEmpty: vi.fn() }),
}));

vi.mock("@features/csm-announcements/api/useGetAnnouncementRequest", () => ({
  useGetAnnouncementRequest: vi.fn(),
}));
vi.mock("@features/csm-announcements/api/useUpdateAnnouncementRequest", () => ({
  useUpdateAnnouncementRequest: vi.fn(),
}));
vi.mock("@features/csm-announcements/api/useRecordAnnouncementRequestDryRun", () => ({
  useRecordAnnouncementRequestDryRun: vi.fn(),
}));
vi.mock("@features/csm-announcements/api/useSubmitAnnouncementRequest", () => ({
  useSubmitAnnouncementRequest: vi.fn(),
}));
vi.mock("@features/csm-announcements/api/useApproveAnnouncementRequest", () => ({
  useApproveAnnouncementRequest: vi.fn(),
}));
vi.mock("@features/csm-announcements/api/usePublishAnnouncementRequest", () => ({
  usePublishAnnouncementRequest: vi.fn(),
}));
vi.mock("@features/csm-announcements/api/useAnnouncementDryRun", () => ({
  DRY_RUN_TAG_LABEL: "Dry Run",
  useAnnouncementDryRun: vi.fn(),
}));
vi.mock("@features/csm-announcements/components/CreateCustomerAnnouncementForm", () => ({
  SECURITY_ANNOUNCEMENT_TAG_LABEL: "Security Announcement",
}));
// The rich-text editor isn't this dialog's concern (see EditorWithSourceToggle's
// own tests) — stubbed to a plain textarea so edits are simple to simulate.
vi.mock("@components/rich-text-editor/EditorWithSourceToggle", () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea aria-label="Description" value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}));

const mockedGet = vi.mocked(useGetAnnouncementRequest);
const mockedUpdate = vi.mocked(useUpdateAnnouncementRequest);
const mockedRecordDryRun = vi.mocked(useRecordAnnouncementRequestDryRun);
const mockedSubmit = vi.mocked(useSubmitAnnouncementRequest);
const mockedApprove = vi.mocked(useApproveAnnouncementRequest);
const mockedPublish = vi.mocked(usePublishAnnouncementRequest);
const mockedDryRun = vi.mocked(useAnnouncementDryRun);

const BASE_REQUEST: AnnouncementRequest = {
  id: "req-1",
  kind: "customer",
  state: "draft",
  subject: "Scheduled maintenance",
  description: "<p>Details</p>",
  isSecurityAnnouncement: false,
  audienceDefinition: { scope: "specific", projectIds: ["p-1"] },
  createdBy: "jane@example.com",
  createdAt: "2026-07-01T10:00:00Z",
  updatedAt: "2026-07-01T10:00:00Z",
};

function mockGet(overrides: Partial<AnnouncementRequest> | null, extra: Record<string, unknown> = {}): void {
  mockedGet.mockReturnValue({
    data: overrides === null ? null : { ...BASE_REQUEST, ...overrides },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    ...extra,
  } as unknown as ReturnType<typeof useGetAnnouncementRequest>);
}

const noopMutation = () =>
  ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, isError: false, error: null }) as unknown;

/** The dialog renders a `Link` for the dry-run result, so every render needs router context. */
function render(ui: ReactElement): ReturnType<typeof rtlRender> {
  return rtlRender(<MemoryRouter>{ui}</MemoryRouter>);
}

beforeEach(() => {
  mockedGet.mockReset();
  mockedUpdate.mockReset();
  mockedRecordDryRun.mockReset();
  mockedSubmit.mockReset();
  mockedApprove.mockReset();
  mockedPublish.mockReset();
  mockedDryRun.mockReset();

  mockedUpdate.mockReturnValue(noopMutation() as ReturnType<typeof useUpdateAnnouncementRequest>);
  mockedRecordDryRun.mockReturnValue(noopMutation() as ReturnType<typeof useRecordAnnouncementRequestDryRun>);
  mockedSubmit.mockReturnValue(noopMutation() as ReturnType<typeof useSubmitAnnouncementRequest>);
  mockedApprove.mockReturnValue(noopMutation() as ReturnType<typeof useApproveAnnouncementRequest>);
  mockedDryRun.mockReturnValue({
    runningDryRun: false,
    dryRunResult: null,
    canRunDryRun: true,
    handleRunDryRun: vi.fn(),
  });
  mockedPublish.mockReturnValue({
    publishing: false,
    progress: null,
    succeededProjectIds: [],
    failedProjectIds: [],
    failedTagProjectIds: [],
    published: null,
    handlePublish: vi.fn(),
  });
});

describe("AnnouncementRequestDialog — loading/error", () => {
  it("shows skeletons while loading", () => {
    mockGet(null, { isLoading: true });
    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    // MUI's Dialog portals into document.body, not the local render container.
    expect(document.body.querySelectorAll(".MuiSkeleton-root").length).toBeGreaterThan(0);
  });

  it("shows a retry button on error", () => {
    const refetch = vi.fn();
    mockGet(null, { isError: true, refetch });
    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(refetch).toHaveBeenCalled();
  });
});

describe("AnnouncementRequestDialog — draft", () => {
  it("shows editable fields and a disabled Submit button until a dry run is recorded", () => {
    mockGet({ state: "draft", dryRunCaseId: null });
    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);

    expect(screen.getByDisplayValue("Scheduled maintenance")).toBeInTheDocument();
    const submitBtn = screen.getByRole("button", { name: /submit for approval/i });
    expect(submitBtn).toBeDisabled();
    expect(screen.getByText(/run a dry run first/i)).toBeInTheDocument();
  });

  it("enables Submit once a dry run has been recorded, and submitting calls the mutation", () => {
    mockGet({ state: "draft", dryRunCaseId: "case-123" });
    const submitMutate = vi.fn();
    mockedSubmit.mockReturnValue({
      mutate: submitMutate,
      isPending: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof useSubmitAnnouncementRequest>);

    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    const submitBtn = screen.getByRole("button", { name: /submit for approval/i });
    expect(submitBtn).not.toBeDisabled();
    fireEvent.click(submitBtn);
    expect(submitMutate).toHaveBeenCalled();
  });

  it("saves edited content via the update mutation", () => {
    mockGet({ state: "draft" });
    const updateMutate = vi.fn();
    mockedUpdate.mockReturnValue({
      mutate: updateMutate,
      isPending: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof useUpdateAnnouncementRequest>);

    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    fireEvent.change(screen.getByDisplayValue("Scheduled maintenance"), {
      target: { value: "Updated subject" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    expect(updateMutate).toHaveBeenCalledWith(
      expect.objectContaining({ subject: "Updated subject" }),
    );
  });
});

describe("AnnouncementRequestDialog — pending_approval", () => {
  it("shows read-only content with Mark as approved and Edit, and approving calls the mutation", () => {
    mockGet({ state: "pending_approval", submittedBy: "jane@example.com", submittedAt: "2026-07-02T10:00:00Z" });
    const approveMutate = vi.fn();
    mockedApprove.mockReturnValue({
      mutate: approveMutate,
      isPending: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof useApproveAnnouncementRequest>);

    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    // Read-only: no editable subject field.
    expect(screen.queryByDisplayValue("Scheduled maintenance")).not.toBeInTheDocument();
    expect(screen.getByText("Scheduled maintenance")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /mark as approved/i }));
    expect(approveMutate).toHaveBeenCalled();
  });

  it("requires confirming before switching to edit mode", () => {
    mockGet({ state: "pending_approval" });
    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: /^edit$/i }));
    expect(screen.getByText(/revert this request to draft/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /continue editing/i }));
    expect(screen.getByDisplayValue("Scheduled maintenance")).toBeInTheDocument();
  });
});

describe("AnnouncementRequestDialog — approved", () => {
  it("keeps content editable in place and shows a Publish button", () => {
    mockGet({
      state: "approved",
      resolvedProjectIds: ["p-1", "p-2"],
      resolvedProjectCount: 2,
    });
    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);

    expect(screen.getByDisplayValue("Scheduled maintenance")).toBeInTheDocument();
    expect(screen.getByText("2 projects")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^publish$/i })).toBeInTheDocument();
  });

  it("calls handlePublish when Publish is clicked", () => {
    mockGet({ state: "approved", resolvedProjectIds: ["p-1"], resolvedProjectCount: 1 });
    const handlePublish = vi.fn();
    mockedPublish.mockReturnValue({
      publishing: false,
      progress: null,
      succeededProjectIds: [],
      failedProjectIds: [],
      failedTagProjectIds: [],
      published: null,
      handlePublish,
    });

    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /^publish$/i }));
    expect(handlePublish).toHaveBeenCalled();
  });

  it("shows live progress while publishing", () => {
    mockGet({ state: "approved", resolvedProjectIds: ["p-1", "p-2"], resolvedProjectCount: 2 });
    mockedPublish.mockReturnValue({
      publishing: true,
      progress: { completed: 1, total: 2 },
      succeededProjectIds: [],
      failedProjectIds: [],
      failedTagProjectIds: [],
      published: null,
      handlePublish: vi.fn(),
    });

    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    expect(screen.getByText(/sending 1 \/ 2/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /publishing/i })).toBeDisabled();
  });

  it("offers a retry with only the failed projects listed after a partial failure", () => {
    mockGet({ state: "approved", resolvedProjectIds: ["p-1", "p-2"], resolvedProjectCount: 2 });
    mockedPublish.mockReturnValue({
      publishing: false,
      progress: null,
      succeededProjectIds: ["p-1"],
      failedProjectIds: ["p-2"],
      failedTagProjectIds: [],
      published: null,
      handlePublish: vi.fn(),
    });

    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);
    expect(screen.getByRole("button", { name: /retry failed projects/i })).toBeInTheDocument();
    expect(screen.getByText(/failed for: p-2/i)).toBeInTheDocument();
  });
});

describe("AnnouncementRequestDialog — published", () => {
  it("shows a read-only summary with no action buttons", () => {
    mockGet({
      state: "published",
      resolvedProjectIds: ["p-1"],
      resolvedProjectCount: 1,
      publishedBy: "jane@example.com",
      publishedAt: "2026-07-03T10:00:00Z",
    });
    render(<AnnouncementRequestDialog requestId="req-1" onClose={vi.fn()} />);

    expect(screen.queryByDisplayValue("Scheduled maintenance")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /submit for approval/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /mark as approved/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^publish$/i })).not.toBeInTheDocument();
  });
});
