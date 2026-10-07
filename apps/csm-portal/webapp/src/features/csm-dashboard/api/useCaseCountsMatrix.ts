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

// Kept exported from here: the composition hook, the matrix table and the
// pies all import them from this module.
export { MATRIX_SEVERITIES, MATRIX_STATES };

export interface CaseCountsMatrix {
  /** counts[severity][state] = number of cases. */
  counts: Record<Severity, Record<CaseState, number>>;
  severityTotals: Record<Severity, number>;
  stateTotals: Record<CaseState, number>;
  total: number;
}

/** Adds the row, column and grand totals to a snapshot's cell counts. */
function selectMatrix({ counts }: CaseCountsSnapshot): CaseCountsMatrix {
  const severityTotals = {} as Record<Severity, number>;
  const stateTotals = {} as Record<CaseState, number>;
  for (const st of MATRIX_STATES) stateTotals[st] = 0;
  let total = 0;
  for (const s of MATRIX_SEVERITIES) {
    severityTotals[s] = 0;
    for (const st of MATRIX_STATES) {
      const n = counts[s][st];
      severityTotals[s] += n;
      stateTotals[st] += n;
      total += n;
    }
  }
  return { counts, severityTotals, stateTotals, total };
}

/**
 * Case counts broken down by severity x state, for the dashboard matrix.
 *
 * Reads the shared snapshot {@link fetchCaseCounts} fetches in six aggregate
 * requests (see there for what it replaced and why), so it costs nothing extra
 * when the composition pies are on the same page. Counts are exact; row, column
 * and grand totals are summed from the cells.
 */
export function useCaseCountsMatrix(): UseQueryResult<CaseCountsMatrix, Error> {
  const api = useBackendApi();

  return useQuery<CaseCountsSnapshot, Error, CaseCountsMatrix>({
    queryKey: CASE_COUNTS_QUERY_KEY,
    queryFn: () => fetchCaseCounts(api),
    select: selectMatrix,
    staleTime: 60_000,
  });
}
