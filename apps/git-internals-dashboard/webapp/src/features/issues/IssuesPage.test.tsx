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
import { act, fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import IssuesPage from "./IssuesPage";

/** Builds a 200 OK Response with a JSON body, for mocking fetch. */
function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

const EMPTY_OVERVIEW = {
  refreshedAt: "2026-01-01T00:00:00Z",
  filters: { repo: null, priority: null, abtTeam: null },
  abtTeams: [],
  hero: {
    violated: { n: 0, delta: 0, spark: [] },
    atRisk: { n: 0, delta: 0, spark: [] },
    cs: { n: 0, byStatus: [] },
    productSide: { n: 0, delta: 0, spark: [] },
  },
  projects: [],
  priorities: [],
  matrix: { rows: [], totals: { violated: 0, atRisk: 0, onTrack: 0, cs: 0 }, grandTotal: 0 },
  volume: [],
  unknownStatuses: [],
};

/** Two issues with title/abtTeam/openedBy set (one with openedBy: null), as the /issues envelope shape. */
const ISSUES_WITH_TITLES = {
  issues: [
    {
      id: 1,
      number: 101,
      state: "OPEN",
      url: "https://github.com/example/repo/issues/101",
      repo: "org/alpha",
      priority: "High(P2)",
      currentStatus: "Open",
      githubCreatedAt: "2026-01-01T00:00:00Z",
      githubUpdatedAt: "2026-01-01T00:00:00Z",
      sla: { budgetHours: 48, consumedHours: 10, remainingHours: 38, pctConsumed: 0.2, slaState: "OK", slaRunning: true },
      title: "Fix the widget",
      abtTeam: "Atlas",
      openedBy: "person@wso2.com",
    },
    {
      id: 2,
      number: 102,
      state: "OPEN",
      url: "https://github.com/example/repo/issues/102",
      repo: "org/alpha",
      priority: "High(P2)",
      currentStatus: "Open",
      githubCreatedAt: "2026-01-01T00:00:00Z",
      githubUpdatedAt: "2026-01-01T00:00:00Z",
      sla: { budgetHours: 48, consumedHours: 20, remainingHours: 28, pctConsumed: 0.42, slaState: "OK", slaRunning: true },
      title: "Untitled thing",
      abtTeam: null,
      openedBy: null,
    },
  ],
  total: 2,
  limit: 20,
  offset: 0,
  hasMore: false,
};

/** Renders IssuesPage under a fresh QueryClient and a memory router at /issues. */
function renderIssuesPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/issues", element: <IssuesPage /> }], {
    initialEntries: ["/issues"],
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

describe("IssuesPage", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // @ts-expect-error -- partial config is fine for this test
    window.config = { GID_BACKEND_BASE_URL: "https://backend.example.test" };
  });

  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    // @ts-expect-error -- test-only cleanup of the global window.config
    delete window.config;
  });

  it("keeps a filter changed mid-debounce instead of the search box's stale snapshot clobbering it", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/metrics/overview")) return Promise.resolve(jsonResponse(EMPTY_OVERVIEW));
      if (url.includes("/issues")) return Promise.resolve(jsonResponse([]));
      if (url.includes("/taxonomy")) return Promise.resolve(jsonResponse({ statuses: [], csStatuses: [] }));
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    const router = renderIssuesPage();

    // Let the initial overview/issues/taxonomy queries settle.
    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
    });

    const searchInput = screen.getByPlaceholderText("Search by issue #…");
    act(() => {
      fireEvent.change(searchInput, { target: { value: "42" } });
    });

    // Before the 300ms search debounce fires, apply a second, non-debounced
    // filter change (the "Violated" bucket chip) — this goes through the
    // same immediate setParams(...) path as the repo/priority <Select>s.
    act(() => {
      fireEvent.click(screen.getByText("Violated"));
    });

    await act(async () => {
      vi.advanceTimersByTime(300);
      await vi.runOnlyPendingTimersAsync();
    });

    const search = router.state.location.search;
    expect(search).toContain("q=42");
    expect(search).toContain("bucket=violated");
  });

  it("splits the CS-side chip into separate Waiting on CS Team / Pending Patch Queue tiles", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/metrics/overview")) return Promise.resolve(jsonResponse(EMPTY_OVERVIEW));
      if (url.includes("/issues")) return Promise.resolve(jsonResponse([]));
      if (url.includes("/taxonomy")) return Promise.resolve(jsonResponse({ statuses: [], csStatuses: ["WOC", "Pending Patch Queue"] }));
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    const router = renderIssuesPage();

    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
    });

    // There is no single combined "On CS Side" chip anymore.
    expect(screen.queryByText("On CS Side")).toBeNull();

    act(() => {
      fireEvent.click(screen.getByText("Waiting on CS Team"));
    });
    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
    });

    let search = router.state.location.search;
    expect(search).toContain("bucket=cs");
    expect(search).toContain("status=WOC");
    expect(screen.getByText("Waiting on CS Team issues")).toBeTruthy();

    act(() => {
      fireEvent.click(screen.getByText("Pending Patch Queue"));
    });
    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
    });

    search = router.state.location.search;
    expect(search).toContain("bucket=cs");
    expect(search).toContain("status=Pending+Patch+Queue");
    expect(screen.getByText("Pending Patch Queue issues")).toBeTruthy();
  });

  it("renders titles inline and the Opened by column without a separate titles request", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/metrics/overview")) return Promise.resolve(jsonResponse(EMPTY_OVERVIEW));
      if (url.includes("/issues")) return Promise.resolve(jsonResponse(ISSUES_WITH_TITLES));
      if (url.includes("/taxonomy")) return Promise.resolve(jsonResponse({ statuses: [], csStatuses: [] }));
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderIssuesPage();

    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
    });

    // The title renders directly from the /issues response, with no round trip
    // to a titles endpoint.
    expect(screen.getByText("Fix the widget")).toBeTruthy();
    expect(screen.getByText("Untitled thing")).toBeTruthy();
    expect(screen.getByText("person@wso2.com")).toBeTruthy();
    // The other issue's openedBy is null; both rows' sla is non-null so this
    // "—" can only be the empty Opened by cell.
    expect(screen.getByText("—")).toBeTruthy();

    // Only these three endpoints back the page; any other endpoint being
    // hit (including a round trip to fetch titles separately) would fail
    // this assertion.
    const calledEndpoints = new Set(
      fetchMock.mock.calls.map(([input]) => new URL(String(input)).pathname),
    );
    expect(calledEndpoints).toEqual(new Set(["/issues", "/metrics/overview", "/taxonomy"]));
  });

  it("does not render its own Project/Priority filter selects", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/metrics/overview")) return Promise.resolve(jsonResponse(EMPTY_OVERVIEW));
      if (url.includes("/issues")) return Promise.resolve(jsonResponse({ issues: [], total: 0, limit: 20, offset: 0, hasMore: false }));
      if (url.includes("/taxonomy")) return Promise.resolve(jsonResponse({ statuses: [], csStatuses: [] }));
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderIssuesPage();

    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
    });

    // IssuesPage renders only its own sort control — the Project/Priority
    // selects shown alongside it in the app header live in AppShell, which
    // this standalone render doesn't include.
    const comboboxes = screen.getAllByRole("combobox");
    expect(comboboxes).toHaveLength(1);
    expect(comboboxes[0]).toHaveTextContent("Sort:");
  });
});
