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

import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useBackendApi } from "@api/backend/client";
import {
  CASE_COUNTS_QUERY_KEY,
  fetchCaseCounts,
  MATRIX_SEVERITIES,
  MATRIX_STATES,
  type CaseCountsSnapshot,
} from "@features/csm-dashboard/api/caseCounts";
import type {
  CaseState,
  Severity,
} from "@features/csm-dashboard/types/abtDashboard";

/**
 * The states the composition pies break down — the SAME active set the
 * severity×state matrix tracks, so the pie totals reconcile with the table
 * above them. `closed` is intentionally excluded from both pies and surfaced
 * separately as {@link CaseComposition.closedTotal}, so a large backlog of
 * closed cases can't make the active counts disagree with the matrix.
 */
export const COMPOSITION_STATES: CaseState[] = MATRIX_STATES;

export interface CaseComposition {
  /** Active-case count per severity (excludes closed). */
  bySeverity: Record<Severity, number>;
  /** Active-case count per state (excludes closed). */
  byState: Record<CaseState, number>;
  severityTotal: number;
  stateTotal: number;
  /** Closed cases — excluded from the pies, shown as a separate figure. */
  closedTotal: number;
}

/**
 * Collapses the snapshot's severity x state counts into the pies' two 1-D
 * breakdowns, each over the *active* cases only, with the closed count apart.
 * A severity's count is its row of the matrix and a state's count is its
 * column, which is exactly what the per-severity and per-state searches this
 * replaced filtered for (a severity AND the active states; a state AND every
 * severity), so the pies, the matrix and their totals reconcile by construction.
 */
function selectComposition({
  counts,
  closedTotal,
}: CaseCountsSnapshot): CaseComposition {
  const bySeverity = {} as Record<Severity, number>;
  const byState = {} as Record<CaseState, number>;
  COMPOSITION_STATES.forEach((st) => (byState[st] = 0));
  for (const s of MATRIX_SEVERITIES) {
    bySeverity[s] = 0;
    for (const st of COMPOSITION_STATES) {
      bySeverity[s] += counts[s][st];
      byState[st] += counts[s][st];
    }
  }
  const severityTotal = MATRIX_SEVERITIES.reduce((a, s) => a + bySeverity[s], 0);
  const stateTotal = COMPOSITION_STATES.reduce((a, s) => a + byState[s], 0);
  return { bySeverity, byState, severityTotal, stateTotal, closedTotal };
}

/**
 * Case composition for the dashboard pies: a 1-D breakdown by severity and a
 * 1-D breakdown by state, each over the *active* cases only (closed excluded,
 * matching the severity×state matrix). The closed count is returned separately.
 *
 * Reads the same snapshot as {@link useCaseCountsMatrix} (one shared query,
 * six aggregate requests; see `fetchCaseCounts`), so showing both costs the
 * same as showing either, and refreshing one refreshes the other.
 */
export function useCaseComposition(): UseQueryResult<CaseComposition, Error> {
  const api = useBackendApi();

  return useQuery<CaseCountsSnapshot, Error, CaseComposition>({
    queryKey: CASE_COUNTS_QUERY_KEY,
    queryFn: () => fetchCaseCounts(api),
    select: selectComposition,
    staleTime: 60_000,
  });
}
