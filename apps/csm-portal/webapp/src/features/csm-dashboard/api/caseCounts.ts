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

import type { QueryKey } from "@tanstack/react-query";
import { ApiQueryKeys } from "@constants/apiConstants";
import type { BackendApi } from "@api/backend/client";
import {
  beStateFromUi,
  priorityFromSeverity,
  uiStateFromBe,
} from "@api/backend/mappers";
import type {
  BeCaseAggregatePayload,
  BeCaseFieldFilter,
  BeGroupByResponse,
} from "@api/backend/types";
import type {
  CaseState,
  Severity,
} from "@features/csm-dashboard/types/abtDashboard";

export const MATRIX_SEVERITIES: Severity[] = ["S0", "S1", "S2", "S3", "S4"];
// Closed cases are deliberately excluded: the dashboard matrix tracks active
// work, so the totals reflect open cases only.
export const MATRIX_STATES: CaseState[] = [
  "open",
  "work_in_progress",
  "waiting_on_wso2",
  "awaiting_info",
  "solution_proposed",
];

/** Larger than the number of case states, so no bucket is ever folded away. */
const MAX_STATE_BUCKETS = 20;

/** Active-case counts per severity and state, plus the closed total. */
export interface CaseCountsSnapshot {
  /** counts[severity][state] = number of (plain) cases; zero when absent. */
  counts: Record<Severity, Record<CaseState, number>>;
  /** Closed cases across all five severities, shown apart from the active counts. */
  closedTotal: number;
}

/**
 * Query key shared by every consumer of {@link fetchCaseCounts}. The dashboard's
 * severity x state matrix and its composition pies read the same snapshot (each
 * through its own `select`), so they cost one set of requests, not one each, and
 * refreshing either refreshes both.
 *
 * Keyed under its own root (not `CSM_CASES`) so case create/patch mutations,
 * which invalidate the `CSM_CASES` prefix, do not re-trigger these requests.
 */
export const CASE_COUNTS_QUERY_KEY: QueryKey = [
  ApiQueryKeys.CSM_CASE_COUNTS,
  "by-severity-and-state",
];

/** The type every count is pinned to: the dashboard drills into the cases list,
 * which is locked to plain cases, so the numbers must reconcile with it. */
const CASE_TYPE_FILTER: BeCaseFieldFilter = {
  field: "type",
  op: "in",
  values: ["case"],
};

function emptyCounts(): CaseCountsSnapshot["counts"] {
  const counts = {} as CaseCountsSnapshot["counts"];
  for (const s of MATRIX_SEVERITIES) {
    counts[s] = {} as Record<CaseState, number>;
    for (const st of MATRIX_STATES) counts[s][st] = 0;
  }
  return counts;
}

/** Sum of every bucket, however the response reports it. */
function aggregateTotal(res: BeGroupByResponse): number {
  if (typeof res.totalRecords === "number") return res.totalRecords;
  return (
    (res.groups ?? []).reduce((sum, g) => sum + (g.count ?? 0), 0) +
    (res.othersCount ?? 0)
  );
}

/**
 * Fetches everything the dashboard's case-count widgets show, in six requests
 * (five severities plus the closed total), each a single server-side
 * aggregation (`POST /cases/aggregate`, `groupBy: "state"`), rather than the
 * thirty-six count-only `POST /cases/search` calls this replaced: one per
 * (severity, state) cell of the matrix, one per severity, one per state and
 * one for closed. Each of those ran a COUNT and a page query, so one dashboard
 * load was seventy-two database statements; this is six.
 *
 * Counts are exact, with the same filters the per-cell searches used
 * (severity, active states, type `case`); the aggregate and the search build
 * their WHERE clause from the same code, so a bucket always agrees with the
 * total the equivalent search reported. Grouping is by `state` only, because
 * state buckets are keyed by the domain value on every data source (severity
 * buckets are not), with the severity as a filter.
 */
export async function fetchCaseCounts(
  api: BackendApi,
): Promise<CaseCountsSnapshot> {
  const activeStates = MATRIX_STATES.map(beStateFromUi);
  const allSeverities = MATRIX_SEVERITIES.map(priorityFromSeverity);

  const aggregateStates = (
    severities: string[],
    states: string[],
  ): Promise<BeGroupByResponse> =>
    api.post<BeCaseAggregatePayload, BeGroupByResponse>("/cases/aggregate", {
      filters: {
        filters: [
          { field: "severity", op: "in", values: severities },
          { field: "state", op: "in", values: states },
          CASE_TYPE_FILTER,
        ],
      },
      groupBy: "state",
      maxGroups: MAX_STATE_BUCKETS,
    });

  const [perSeverity, closed] = await Promise.all([
    Promise.all(
      MATRIX_SEVERITIES.map((severity) =>
        aggregateStates([priorityFromSeverity(severity)], activeStates).then(
          (res) => ({ severity, res }),
        ),
      ),
    ),
    aggregateStates(allSeverities, [beStateFromUi("closed")]),
  ]);

  const counts = emptyCounts();
  for (const { severity, res } of perSeverity) {
    for (const group of res.groups ?? []) {
      // The wire key is the domain state; normalising also accepts a
      // ServiceNow-style label ("Work In Progress").
      const state = uiStateFromBe(group.key);
      if (MATRIX_STATES.includes(state)) counts[severity][state] += group.count ?? 0;
    }
  }
  return { counts, closedTotal: aggregateTotal(closed) };
}
