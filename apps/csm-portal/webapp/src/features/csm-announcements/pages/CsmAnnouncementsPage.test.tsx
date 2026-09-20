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
import {
  fireEvent,
  render as rtlRender,
  screen,
  within,
} from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { MemoryRouter } from "react-router";
import "@testing-library/jest-dom/vitest";
import CsmAnnouncementsPage from "@features/csm-announcements/pages/CsmAnnouncementsPage";
import { useSearchAnnouncements } from "@features/csm-announcements/api/useSearchAnnouncements";
import { useSearchAnnouncementRequests } from "@features/csm-announcements/api/useSearchAnnouncementRequests";
import type { CsmAnnouncementRow } from "@features/csm-announcements/types/csmAnnouncements";
import type { AnnouncementRequest } from "@features/csm-announcements/types/announcementRequests";
import { formatBackendTimestampForDisplay } from "@utils/dateTime";

// The backend client reads runtime config (`CSM_PORTAL_BACKEND_BASE_URL`) at
// module load, which isn't present under vitest. QueryErrorState imports
// `BackendApiError` from it, so stub the module (same approach as
// useCsmCaseActivities.test.ts).
vi.mock("@api/backend/client", () => ({
  BackendApiError: class BackendApiError extends Error {},
  useBackendApi: () => ({ post: vi.fn() }),
}));

vi.mock("@features/csm-announcements/api/useSearchAnnouncements", () => ({
  useSearchAnnouncements: vi.fn(),
}));

vi.mock("@features/csm-announcements/api/useSearchAnnouncementRequests", () => ({
  useSearchAnnouncementRequests: vi.fn(),
}));

// The dialog pulls in its own web of hooks (get/update/submit/approve/publish)
// that aren't this page's concern — stubbed so the page test only asserts it
// opens with the right id, not what's inside it (see AnnouncementRequestDialog.test.tsx).
vi.mock("@features/csm-announcements/components/AnnouncementRequestDialog", () => ({
  default: ({ requestId, onClose }: { requestId: string; onClose: () => void }) => (
    <div data-testid="request-dialog">
      <span>request dialog: {requestId}</span>
      <button onClick={onClose}>close dialog</button>
    </div>
  ),
}));

// The real project picker fetches from the backend; stub it so the page test
// stays focused on the list + its own state filter.
vi.mock("@features/csm-cases/components/AsyncProjectMultiSelect", () => ({
  default: () => <div data-testid="project-filter" />,
}));

// The signed-in user's profile/id-token claims aren't relevant to this page's
// own behavior — only the column picker's storage key derives from them
// (see useColumnPreferences.test.ts for that logic).
vi.mock("@context/current-user/CurrentUserContext", () => ({
  useCurrentUser: () => ({ user: { id: "user-1" }, isLoading: false, isError: false }),
}));
vi.mock("@hooks/useIdTokenClaims", () => ({
  useIdTokenClaims: () => ({ email: "user@example.test" }),
}));

const mockedUseSearch = vi.mocked(useSearchAnnouncements);
const mockedUseSearchRequests = vi.mocked(useSearchAnnouncementRequests);
// Number, Reference, Subject, Project, State, Created by, Created, Updated.
const ANNOUNCEMENT_COLUMN_COUNT = 8;

/** `CsmAnnouncementsPage` renders a `RouterLink` per row, so every render
 * here needs router context. `initialPath` lets a test land on a specific
 * URL (e.g. `?tab=pending`, the create form's post-save redirect target). */
function render(ui: ReactElement, initialPath = "/"): ReturnType<typeof rtlRender> {
  return rtlRender(<MemoryRouter initialEntries={[initialPath]}>{ui}</MemoryRouter>);
}

const ROW: CsmAnnouncementRow = {
  id: "a-1",
  number: "ANN-1001",
  subject: "Scheduled maintenance on Choreo",
  projectName: "IAM Production",
  state: "open",
  createdBy: "jane@example.com",
  createdAt: "2026-07-01T10:00:00Z",
  updatedAt: "2026-07-02T10:00:00Z",
};

function mockResult(
  overrides: Partial<ReturnType<typeof useSearchAnnouncements>>,
): void {
  mockedUseSearch.mockReturnValue({
    data: undefined,
    isLoading: false,
    isFetching: false,
    isError: false,
    error: null,
    ...overrides,
  } as unknown as ReturnType<typeof useSearchAnnouncements>);
}

const PENDING_REQUEST: AnnouncementRequest = {
  id: "req-1",
  kind: "customer",
  state: "pending_approval",
  subject: "Upcoming maintenance",
  description: "<p>Details</p>",
  isSecurityAnnouncement: false,
  audienceDefinition: { scope: "specific", projectIds: ["p-1"] },
  createdBy: "jane@example.com",
  createdAt: "2026-07-01T10:00:00Z",
  updatedAt: "2026-07-01T10:00:00Z",
};

beforeEach(() => {
  mockedUseSearch.mockReset();
  mockedUseSearchRequests.mockReset();
  mockedUseSearchRequests.mockReturnValue({
    data: { requests: [], total: 0, limit: 10, offset: 0, hasMore: false },
    isLoading: false,
    isFetching: false,
    isError: false,
    error: null,
  } as unknown as ReturnType<typeof useSearchAnnouncementRequests>);
  window.localStorage.clear();
});

describe("CsmAnnouncementsPage — list states", () => {
  it("renders a row from the search result", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);
    expect(screen.getByText("Scheduled maintenance on Choreo")).toBeInTheDocument();
    expect(screen.getByText("ANN-1001")).toBeInTheDocument();
    expect(screen.getByText("IAM Production")).toBeInTheDocument();
  });

  it("shows the empty state when there are no announcements", () => {
    mockResult({
      data: { announcements: [], total: 0, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);
    expect(screen.getByText(/no announcements found/i)).toBeInTheDocument();
  });

  it("surfaces the error message on failure", () => {
    mockResult({ isError: true, error: new Error("boom") });
    render(<CsmAnnouncementsPage />);
    expect(screen.getByText("boom")).toBeInTheDocument();
  });
});

describe("CsmAnnouncementsPage — filters default to show-all", () => {
  it("calls the search with no state/project filters on first render", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);
    const [filters] = mockedUseSearch.mock.calls[0];
    expect(filters).toEqual(
      expect.objectContaining({ states: [], projectIds: [] }),
    );
  });

  it("pushes a picked state into the search filters", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);

    fireEvent.mouseDown(screen.getByRole("combobox", { name: /state/i }));
    const listbox = screen.getByRole("listbox");
    fireEvent.click(within(listbox).getByRole("option", { name: /closed/i }));

    // The most recent render's filters carry the selected state.
    const lastCall = mockedUseSearch.mock.calls.at(-1)!;
    expect(lastCall[0].states).toContain("closed");
  });
});

describe("CsmAnnouncementsPage — customise columns", () => {
  it("shows the default columns and hides Created until the user adds it", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);

    expect(screen.getByRole("columnheader", { name: "Number" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Updated" })).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Created" })).not.toBeInTheDocument();
  });

  it("adds the Created column when checked in the picker, and it renders the row's createdAt", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Customise announcements columns" }));
    // Column order is Number, Reference, Subject, Project, State, Created by,
    // Created, Updated — "Created" is the 7th checkbox, one of the two
    // available-but-hidden columns (see DEFAULT_ANNOUNCEMENT_COLUMN_IDS).
    const checkboxes = screen.getAllByRole("checkbox");
    fireEvent.click(checkboxes[6]);

    // The open popover marks the rest of the page `aria-hidden` (MUI's Modal
    // machinery) — `hidden: true` looks past that to the table underneath.
    expect(
      screen.getByRole("columnheader", { name: "Created", hidden: true }),
    ).toBeInTheDocument();
    // Same format the component's own formatDate() uses for createdAt/updatedAt.
    const expectedCreatedAt = formatBackendTimestampForDisplay(ROW.createdAt, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
    expect(screen.getByText(expectedCreatedAt!)).toBeInTheDocument();
  });

  it("adds the Reference column when checked in the picker, and it renders the row's wso2CaseId", () => {
    mockResult({
      data: {
        announcements: [{ ...ROW, wso2CaseId: "ACMESUB-42" }],
        total: 1,
        limit: 20,
        offset: 0,
        hasMore: false,
      },
    });
    render(<CsmAnnouncementsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Customise announcements columns" }));
    // "Reference" is the 2nd checkbox — see the column order comment above.
    const checkboxes = screen.getAllByRole("checkbox");
    fireEvent.click(checkboxes[1]);

    expect(
      screen.getByRole("columnheader", { name: "Reference", hidden: true }),
    ).toBeInTheDocument();
    expect(screen.getByText("ACMESUB-42")).toBeInTheDocument();
  });

  it("never lets every column be unchecked", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Customise announcements columns" }));
    // Uncheck every column in turn (re-query each time — a re-render can
    // change stale element references — and skip whichever one the hook has
    // disabled as the last remaining visible column).
    for (let i = 0; i < ANNOUNCEMENT_COLUMN_COUNT; i++) {
      const checkbox = screen.getAllByRole("checkbox")[i];
      if (!checkbox.hasAttribute("disabled") && (checkbox as HTMLInputElement).checked) {
        fireEvent.click(checkbox);
      }
    }

    // At least one column (the last remaining) is still rendered underneath
    // the (still open) popover.
    expect(screen.getAllByRole("columnheader", { hidden: true }).length).toBeGreaterThan(0);
  });
});

describe("CsmAnnouncementsPage — Pending tab", () => {
  it("stays on the Announcements tab (and its table) by default", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    render(<CsmAnnouncementsPage />);
    expect(screen.getByText("Scheduled maintenance on Choreo")).toBeInTheDocument();
    expect(screen.queryByTestId("request-dialog")).not.toBeInTheDocument();
  });

  it("switches to the pending table and defaults to the Pending approval state filter", () => {
    mockResult({
      data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false },
    });
    mockedUseSearchRequests.mockReturnValue({
      data: { requests: [PENDING_REQUEST], total: 1, limit: 10, offset: 0, hasMore: false },
      isLoading: false,
      isFetching: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof useSearchAnnouncementRequests>);
    render(<CsmAnnouncementsPage />);

    fireEvent.click(screen.getByRole("tab", { name: "Pending" }));

    expect(screen.getByText("Upcoming maintenance")).toBeInTheDocument();
    // state is the 1st arg of useSearchAnnouncementRequests(state, page, pageSize).
    const lastCall = mockedUseSearchRequests.mock.calls.at(-1)!;
    expect(lastCall[0]).toBe("pending_approval");
  });

  it("opens the request dialog with the clicked row's id", () => {
    mockResult({ data: { announcements: [], total: 0, limit: 20, offset: 0, hasMore: false } });
    mockedUseSearchRequests.mockReturnValue({
      data: { requests: [PENDING_REQUEST], total: 1, limit: 10, offset: 0, hasMore: false },
      isLoading: false,
      isFetching: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof useSearchAnnouncementRequests>);
    render(<CsmAnnouncementsPage />);

    fireEvent.click(screen.getByRole("tab", { name: "Pending" }));
    fireEvent.click(screen.getByText("Upcoming maintenance"));

    expect(screen.getByText(`request dialog: ${PENDING_REQUEST.id}`)).toBeInTheDocument();
    fireEvent.click(screen.getByText("close dialog"));
    expect(screen.queryByTestId("request-dialog")).not.toBeInTheDocument();
  });

  it("shows the empty state text for the selected pending state", () => {
    mockResult({ data: { announcements: [], total: 0, limit: 20, offset: 0, hasMore: false } });
    render(<CsmAnnouncementsPage />);
    fireEvent.click(screen.getByRole("tab", { name: "Pending" }));
    expect(screen.getByText(/no pending approval requests/i)).toBeInTheDocument();
  });

  it("lands directly on the Pending tab when opened with ?tab=pending", () => {
    mockResult({ data: { announcements: [ROW], total: 1, limit: 20, offset: 0, hasMore: false } });
    mockedUseSearchRequests.mockReturnValue({
      data: { requests: [PENDING_REQUEST], total: 1, limit: 10, offset: 0, hasMore: false },
      isLoading: false,
      isFetching: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof useSearchAnnouncementRequests>);
    render(<CsmAnnouncementsPage />, "/announcements?tab=pending");

    // The Pending tab's own content shows immediately — no click needed —
    // and the Announcements tab's row isn't rendered.
    expect(screen.getByText("Upcoming maintenance")).toBeInTheDocument();
    expect(screen.queryByText("Scheduled maintenance on Choreo")).not.toBeInTheDocument();
  });
});

describe("CsmAnnouncementsPage — loading and pagination", () => {
  it("renders skeleton rows while the first page is loading", () => {
    mockResult({ isLoading: true, isFetching: true });
    const { container } = render(<CsmAnnouncementsPage />);
    expect(container.querySelectorAll(".MuiSkeleton-root").length).toBeGreaterThan(0);
    expect(screen.queryByText(/no announcements found/i)).not.toBeInTheDocument();
  });

  it("advances the page when the next-page control is clicked", () => {
    mockResult({
      data: { announcements: [ROW], total: 50, limit: 20, offset: 0, hasMore: true },
    });
    render(<CsmAnnouncementsPage />);
    fireEvent.click(screen.getByRole("button", { name: /go to next page/i }));
    // useSearchAnnouncements(filters, page, pageSize) — page is the 2nd arg.
    const lastCall = mockedUseSearch.mock.calls.at(-1)!;
    expect(lastCall[1]).toBe(1);
  });

  it("passes an updated page size when rows-per-page changes", () => {
    mockResult({
      data: { announcements: [ROW], total: 50, limit: 20, offset: 0, hasMore: true },
    });
    render(<CsmAnnouncementsPage />);
    fireEvent.mouseDown(screen.getByRole("combobox", { name: /rows per page/i }));
    fireEvent.click(screen.getByRole("option", { name: "50" }));
    // pageSize is the 3rd arg; changing rows-per-page also resets page to 0.
    const lastCall = mockedUseSearch.mock.calls.at(-1)!;
    expect(lastCall[2]).toBe(50);
    expect(lastCall[1]).toBe(0);
  });
});
