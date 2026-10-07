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

import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type {
  BeCaseAggregatePayload,
  BeCaseFieldFilter,
  BeCaseSearchPayload,
  BeGroupByResponse,
} from "@api/backend/types";
import { priorityFromSeverity } from "@api/backend/mappers";
import type { CaseState, Severity } from "@features/csm-dashboard/types/abtDashboard";

const postMock = vi.fn();

vi.mock("@api/backend/client", () => ({
  useBackendApi: () => ({ post: postMock }),
}));

import {
  MATRIX_SEVERITIES,
  MATRIX_STATES,
  useCaseCountsMatrix,
} from "@features/csm-dashboard/api/useCaseCountsMatrix";
import {
  COMPOSITION_STATES,
  useCaseComposition,
} from "@features/csm-dashboard/api/useCaseComposition";

/** A case as the fake backend stores it. `severity: null` is the common real case. */
interface FakeCase {
  severity: string | null;
  state: string;
  type: string;
}

/** The filters both request kinds carry, evaluated like the entity service does:
 * every filter ANDed, `in` meaning "one of"; a null severity never matches one. */
function matches(c: FakeCase, filters: BeCaseFieldFilter[] | undefined): boolean {
  return (filters ?? []).every((f) => {
    const value = f.field === "severity" ? c.severity : f.field === "state" ? c.state : c.type;
    return value !== null && (f.values ?? []).includes(value);
  });
}

function aggregate(cases: FakeCase[], body: BeCaseAggregatePayload): BeGroupByResponse {
  const counts = new Map<string, number>();
  for (const c of cases.filter((x) => matches(x, body.filters?.filters))) {
    counts.set(c.state, (counts.get(c.state) ?? 0) + 1);
  }
  const groups = [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([key, count]) => ({ key, label: key, count }));
  return { groups, othersCount: 0, totalRecords: groups.reduce((s, g) => s + g.count, 0) };
}

function searchTotal(cases: FakeCase[], body: BeCaseSearchPayload): number {
  return cases.filter((x) => matches(x, body.filters?.filters)).length;
}

/** A backend holding `cases`, answering aggregate and (for the reference) search calls. */
function backend(cases: FakeCase[]) {
  postMock.mockImplementation(async (path: string, body: unknown) => {
    if (path === "/cases/aggregate") return aggregate(cases, body as BeCaseAggregatePayload);
    if (path === "/cases/search") return { total: searchTotal(cases, body as BeCaseSearchPayload) };
    throw new Error("unexpected path " + path);
  });
}

function fakeCases(): FakeCase[] {
  const out: FakeCase[] = [];
  const add = (n: number, severity: string | null, state: string, type = "case") => {
    for (let i = 0; i < n; i++) out.push({ severity, state, type });
  };
  add(3, "catastrophic", "open");
  add(2, "catastrophic", "work_in_progress");
  add(5, "critical", "open");
  add(1, "critical", "awaiting_info");
  add(7, "high", "waiting_on_wso2");
  add(4, "high", "solution_proposed");
  add(6, "medium", "open");
  add(2, "medium", "work_in_progress");
  add(9, "low", "awaiting_info");
  add(4, "low", "closed");
  add(8, "high", "closed");
  add(11, null, "open"); // no severity: counted by no severity filter
  add(5, null, "closed");
  add(13, "high", "open", "service_request"); // not a plain case
  add(6, "critical", "closed", "engagement");
  return out;
}

/** What the thirty-six count-only searches this replaced reported, per the old hooks. */
async function oldStrategy(cases: FakeCase[]) {
  const total = (filters: BeCaseFieldFilter[]) =>
    searchTotal(cases, { pagination: { offset: 0, limit: 1 }, filters: { filters } });
  const active = MATRIX_STATES;
  const allPriorities = MATRIX_SEVERITIES.map(priorityFromSeverity);
  const caseOnly: BeCaseFieldFilter = { field: "type", op: "in", values: ["case"] };

  const counts = {} as Record<Severity, Record<CaseState, number>>;
  for (const s of MATRIX_SEVERITIES) {
    counts[s] = {} as Record<CaseState, number>;
    for (const st of active) {
      counts[s][st] = total([
        { field: "severity", op: "in", values: [priorityFromSeverity(s)] },
        { field: "state", op: "in", values: [st] },
        caseOnly,
      ]);
    }
  }
  const bySeverity = {} as Record<Severity, number>;
  for (const s of MATRIX_SEVERITIES) {
    bySeverity[s] = total([
      { field: "severity", op: "in", values: [priorityFromSeverity(s)] },
      { field: "state", op: "in", values: active },
      caseOnly,
    ]);
  }
  const byState = {} as Record<CaseState, number>;
  for (const st of active) {
    byState[st] = total([
      { field: "state", op: "in", values: [st] },
      { field: "severity", op: "in", values: allPriorities },
      caseOnly,
    ]);
  }
  const closedTotal = total([
    { field: "state", op: "in", values: ["closed"] },
    { field: "severity", op: "in", values: allPriorities },
    caseOnly,
  ]);
  return { counts, bySeverity, byState, closedTotal };
}

function makeWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return {
    queryClient,
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  };
}

describe("dashboard case counts", () => {
  beforeEach(() => {
    postMock.mockReset();
  });

  it("asks for six aggregations and no searches", async () => {
    backend(fakeCases());
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseCountsMatrix(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(postMock).toHaveBeenCalledTimes(6);
    const paths = postMock.mock.calls.map((c) => c[0]);
    expect(paths.every((p) => p === "/cases/aggregate")).toBe(true);
  });

  it("filters each severity to the active states of plain cases, grouped by state", async () => {
    backend(fakeCases());
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseCountsMatrix(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(postMock).toHaveBeenCalledWith("/cases/aggregate", {
      filters: {
        filters: [
          { field: "severity", op: "in", values: ["high"] },
          { field: "state", op: "in", values: MATRIX_STATES },
          { field: "type", op: "in", values: ["case"] },
        ],
      },
      groupBy: "state",
      maxGroups: expect.any(Number),
    });
    // and the closed total across all five severities
    expect(postMock).toHaveBeenCalledWith("/cases/aggregate", {
      filters: {
        filters: [
          { field: "severity", op: "in", values: ["catastrophic", "critical", "high", "medium", "low"] },
          { field: "state", op: "in", values: ["closed"] },
          { field: "type", op: "in", values: ["case"] },
        ],
      },
      groupBy: "state",
      maxGroups: expect.any(Number),
    });
  });

  it("reports the same matrix the per-cell searches did", async () => {
    const cases = fakeCases();
    backend(cases);
    const expected = await oldStrategy(cases);
    postMock.mockClear();

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseCountsMatrix(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data?.counts).toEqual(expected.counts);
    const grand = MATRIX_SEVERITIES.reduce(
      (a, s) => a + MATRIX_STATES.reduce((b, st) => b + expected.counts[s][st], 0),
      0,
    );
    expect(result.current.data?.total).toBe(grand);
    for (const s of MATRIX_SEVERITIES) {
      expect(result.current.data?.severityTotals[s]).toBe(
        MATRIX_STATES.reduce((a, st) => a + expected.counts[s][st], 0),
      );
    }
    for (const st of MATRIX_STATES) {
      expect(result.current.data?.stateTotals[st]).toBe(
        MATRIX_SEVERITIES.reduce((a, s) => a + expected.counts[s][st], 0),
      );
    }
  });

  it("reports the same composition the per-severity and per-state searches did", async () => {
    const cases = fakeCases();
    backend(cases);
    const expected = await oldStrategy(cases);
    postMock.mockClear();

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseComposition(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data?.bySeverity).toEqual(expected.bySeverity);
    expect(result.current.data?.byState).toEqual(expected.byState);
    expect(result.current.data?.closedTotal).toBe(expected.closedTotal);
    expect(result.current.data?.severityTotal).toBe(
      MATRIX_SEVERITIES.reduce((a, s) => a + expected.bySeverity[s], 0),
    );
    expect(result.current.data?.stateTotal).toBe(
      COMPOSITION_STATES.reduce((a, st) => a + expected.byState[st], 0),
    );
    // Severity and state each partition the same active population.
    expect(result.current.data?.severityTotal).toBe(result.current.data?.stateTotal);
  });

  it("shares one set of requests between the matrix and the composition", async () => {
    backend(fakeCases());
    const { wrapper } = makeWrapper();
    const { result } = renderHook(
      () => ({ matrix: useCaseCountsMatrix(), composition: useCaseComposition() }),
      { wrapper },
    );
    await waitFor(() => {
      expect(result.current.matrix.isSuccess).toBe(true);
      expect(result.current.composition.isSuccess).toBe(true);
    });

    expect(postMock).toHaveBeenCalledTimes(6);
    expect(result.current.composition.data?.severityTotal).toBe(result.current.matrix.data?.total);
  });

  it("refreshes both from either refresh button", async () => {
    backend(fakeCases());
    const { wrapper } = makeWrapper();
    const { result } = renderHook(
      () => ({ matrix: useCaseCountsMatrix(), composition: useCaseComposition() }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.composition.isSuccess).toBe(true));
    expect(postMock).toHaveBeenCalledTimes(6);

    await result.current.composition.refetch();
    await waitFor(() => expect(postMock).toHaveBeenCalledTimes(12));
  });

  it("treats a missing bucket as zero and ignores a state it does not track", async () => {
    postMock.mockImplementation(async (_path: string, body: BeCaseAggregatePayload) => {
      const severity = body.filters?.filters?.[0]?.values?.[0];
      if (severity === "critical") {
        return {
          groups: [
            { key: "open", label: "Open", count: 4 },
            { key: "on_hold", label: "On Hold", count: 99 }, // not an active matrix state
          ],
          othersCount: 0,
          totalRecords: 103,
        };
      }
      return { groups: [], othersCount: 0, totalRecords: 0 };
    });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseCountsMatrix(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data?.counts.S1.open).toBe(4);
    expect(result.current.data?.counts.S1.work_in_progress).toBe(0);
    expect(result.current.data?.counts.S0.open).toBe(0);
    expect(result.current.data?.total).toBe(4);
  });

  it("accepts ServiceNow-style state labels as bucket keys", async () => {
    postMock.mockImplementation(async (_path: string, body: BeCaseAggregatePayload) => {
      const severity = body.filters?.filters?.[0]?.values?.[0];
      if (severity === "high") {
        return {
          groups: [
            { key: "Work In Progress", label: "Work In Progress", count: 3 },
            { key: "Waiting On WSO2", label: "Waiting On WSO2", count: 2 },
          ],
          othersCount: 0,
          totalRecords: 5,
        };
      }
      return { groups: [], othersCount: 0, totalRecords: 0 };
    });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseCountsMatrix(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data?.counts.S2.work_in_progress).toBe(3);
    expect(result.current.data?.counts.S2.waiting_on_wso2).toBe(2);
  });

  it("sums the buckets when a response carries no totalRecords", async () => {
    postMock.mockImplementation(async (_path: string, body: BeCaseAggregatePayload) => {
      const states = body.filters?.filters?.[1]?.values ?? [];
      if (states.length === 1 && states[0] === "closed") {
        return { groups: [{ key: "closed", label: "Closed", count: 7 }], othersCount: 2 };
      }
      return { groups: [], othersCount: 0, totalRecords: 0 };
    });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseComposition(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data?.closedTotal).toBe(9);
  });

  it("is an error, not a partial result, when any request fails", async () => {
    postMock.mockImplementation(async (_path: string, body: BeCaseAggregatePayload) => {
      if (body.filters?.filters?.[0]?.values?.[0] === "medium") throw new Error("boom");
      return { groups: [], othersCount: 0, totalRecords: 0 };
    });
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useCaseCountsMatrix(), { wrapper });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.data).toBeUndefined();
  });
});
