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

import { useState, useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { Box, MenuItem, Skeleton, TablePagination } from "@mui/material";
import { useOverview, useIssues, useTaxonomy, makeIsCsStatus } from "@api/hooks";
import type { BucketKey } from "@api/types";
import { DEFAULT_ISSUE_SORT, ISSUE_SORT_OPTIONS, type IssueSortField } from "@api/issueSort";
import { BackButton } from "@components/BackButton";
import { ErrorState } from "@components/ErrorState";
import { StaleDataAlert } from "@components/StaleDataAlert";
import { FilterSelect } from "@components/FilterSelect";
import { errorMessage } from "@lib/apiError";
import { useGlobalFilters, projectNameFor } from "@lib/filters";
import { useReportFetchProgress } from "@lib/fetchProgress";
import { IssueTimelineRow } from "@components/IssueTimelineRow";
import { gridTemplate } from "@lib/grid";
import { acrylicSurfaceSx } from "@lib/surfaces";

const ROWS_PER_PAGE_OPTIONS = [20, 50, 100];
const DEFAULT_ROWS_PER_PAGE = 20;

const KIND_CHIPS: { key: BucketKey; status?: string; label: string }[] = [
  { key: "all", label: "All Open" },
  { key: "violated", label: "Violated" },
  { key: "at_risk", label: "At Risk" },
  { key: "cs", status: "WOC", label: "Waiting on CS Team" },
  { key: "cs", status: "Pending Patch Queue", label: "Pending Patch Queue" },
  { key: "product_side", label: "On Product Team Side" },
  { key: "untracked", label: "Untracked" },
];

const BUCKET_TITLES: Partial<Record<BucketKey, string>> = {
  violated: "Violated issues",
  at_risk: "At-risk issues",
  on_track: "On-track issues",
  cs: "On-CS-side issues",
  product_side: "On-product-side issues",
  tracked: "Open tracked issues",
  untracked: "Untracked / missing priority",
  attention: "Attention set",
  all: "All open issues",
};

// Friendlier page heading for a single-status drill-down than the raw status name.
const STATUS_TITLES: Record<string, string> = {
  WOC: "Waiting on CS Team issues",
  "Pending Patch Queue": "Pending Patch Queue issues",
};

/** The filtered/drill-down issue list page, URL-driven by repo/priority/bucket/status/q. */
export default function IssuesPage() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const [qInput, setQInput] = useState(params.get("q") ?? "");

  const { repo, priority, abtTeam } = useGlobalFilters();
  const status = params.get("status") ?? undefined;
  const bucket = (params.get("bucket") ?? "all") as BucketKey;
  const sort = (params.get("sort") as IssueSortField | null) ?? DEFAULT_ISSUE_SORT;
  const rowsPerPage = Number(params.get("pageSize")) || DEFAULT_ROWS_PER_PAGE;
  const page = Number(params.get("page")) || 0;

  useEffect(() => {
    const t = setTimeout(() => {
      // This effect re-runs on every params change (e.g. clicking to page 2
      // sets ?page=1, which re-fires it) since it's re-based on the current
      // `params` every time so a repo/priority change applied while this
      // timer is pending is never clobbered by a stale snapshot when it
      // finally fires — see below. But that means it must bail out here
      // whenever qInput isn't actually introducing a new search, or it would
      // unconditionally strip `page` on every unrelated param change (e.g.
      // pagination), bouncing the page back to 1 a moment after any click.
      if ((params.get("q") ?? "") === qInput) return;
      const next = new URLSearchParams(params);
      if (qInput) next.set("q", qInput);
      else next.delete("q");
      next.delete("page"); // a new search always starts back at page 1
      void navigate(`/issues?${next.toString()}`.replace(/\?$/, ""), { replace: true });
    }, 300);
    return () => clearTimeout(t);
  }, [qInput, navigate, params]);

  // Sets or clears (empty value) one URL search param, replacing history, and
  // resets back to page 1 — a filter/sort/page-size change invalidates
  // whatever page the user was on.
  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    next.delete("page");
    setParams(next, { replace: true });
  };

  // Switching kind (or "All open") clears any single-status refinement, unless
  // the chip itself carries one (e.g. the CS-side "Waiting on CS Team" / "Pending
  // Patch Queue" chips, which share bucket=cs and differ only by status).
  const setBucket = (key: BucketKey, chipStatus?: string) => {
    const next = new URLSearchParams(params);
    if (key === "all") next.delete("bucket");
    else next.set("bucket", key);
    if (chipStatus) next.set("status", chipStatus);
    else next.delete("status");
    next.delete("page");
    setParams(next, { replace: true });
  };

  // TablePagination's page is 0-indexed; the URL stores it 1-indexed-minus-1
  // implicitly (0 = unset = page 1) so a bare /issues URL has no ?page=0 noise.
  const setPage = (newPage: number) => {
    const next = new URLSearchParams(params);
    if (newPage > 0) next.set("page", String(newPage));
    else next.delete("page");
    setParams(next, { replace: true });
  };

  const setRowsPerPage = (newRowsPerPage: number) => {
    const next = new URLSearchParams(params);
    if (newRowsPerPage !== DEFAULT_ROWS_PER_PAGE) next.set("pageSize", String(newRowsPerPage));
    else next.delete("pageSize");
    next.delete("page");
    setParams(next, { replace: true });
  };

  const { data: overview } = useOverview({ repo, priority, abtTeam });
  const { data: taxonomy } = useTaxonomy();
  const isCsStatus = makeIsCsStatus(taxonomy?.csStatuses);
  const {
    data,
    isLoading,
    isPlaceholderData,
    isError,
    error,
    errorUpdatedAt,
    refetch,
  } = useIssues({
    bucket,
    repo,
    priority,
    abtTeam,
    status,
    q: params.get("q") ?? undefined,
    sort,
    limit: rowsPerPage,
    offset: page * rowsPerPage,
  });
  useReportFetchProgress(isPlaceholderData);

  const issues = data?.issues;
  const total = data?.total ?? 0;

  const projName = repo ? projectNameFor(overview?.projects, repo ?? null) : "All Projects";

  // A single CS status gets its own titled list.
  const title = status ? (STATUS_TITLES[status] ?? `${status} issues`) : (BUCKET_TITLES[bucket] ?? "Issues");
  const cols = gridTemplate("full");

  return (
    <Box aria-busy={isPlaceholderData}>
      <BackButton />

      {isError && issues && (
        <StaleDataAlert key={errorUpdatedAt} message={errorMessage(error, "Failed to refresh the issue list")} />
      )}

      <Box sx={{ mb: 2, mt: "18px", display: "flex", flexWrap: "wrap", alignItems: "flex-end", justifyContent: "space-between", gap: "14px" }}>
        <Box>
          <Box component="h1" sx={{ m: 0, fontSize: 22, fontWeight: 600, lineHeight: 1.2, letterSpacing: "-0.01em" }}>{title}</Box>
          <Box sx={{ mt: 0.75, fontSize: 13, color: "var(--sla-fg3)" }}>
            {abtTeam ? `${abtTeam} · ` : ""}{projName} ·{" "}
            <Box component="b" sx={{ fontWeight: 600, color: "var(--sla-fg2)", fontFamily: "var(--font-mono)" }}>
              {total}
            </Box>{" "}
            matching open issues · click any row to open it on GitHub
          </Box>
        </Box>
        <Box sx={{ display: "flex", alignItems: "center", gap: "10px" }}>
          <Box
            component="input"
            placeholder="Search by issue #…"
            value={qInput}
            onChange={(e) => setQInput((e.target as HTMLInputElement).value.replace(/\D/g, ""))}
            sx={{
              height: 36, width: 192, borderRadius: "9px", border: "1px solid var(--sla-border)", bgcolor: "var(--sla-card)",
              px: 1.5, fontSize: 13, color: "var(--sla-fg)", fontFamily: "inherit", "&:focus": { outline: "none", borderColor: "var(--sla-fg3)" },
            }}
          />
          <FilterSelect value={sort} onChange={(v) => setParam("sort", v === DEFAULT_ISSUE_SORT ? "" : v)}>
            {ISSUE_SORT_OPTIONS.map((o) => (
              <MenuItem key={o.value} value={o.value}>
                Sort: {o.label}
              </MenuItem>
            ))}
          </FilterSelect>
        </Box>
      </Box>

      {/* Kind chips */}
      <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap", gap: 1 }}>
        {KIND_CHIPS.map((chip) => {
          const active = bucket === chip.key && (chip.status ?? undefined) === status;
          return (
            <Box
              key={chip.status ? `${chip.key}:${chip.status}` : chip.key}
              component="button"
              type="button"
              onClick={() => setBucket(chip.key, chip.status)}
              sx={{
                borderRadius: "8px", border: "1px solid", px: 1.5, py: 0.75, fontSize: 12.5, fontWeight: 600, cursor: "pointer",
                borderColor: active ? "var(--sla-primary)" : "var(--sla-border)",
                bgcolor: active ? "var(--sla-primary)" : "var(--sla-card)",
                color: active ? "var(--sla-contrast-text)" : "var(--sla-fg2)",
              }}
            >
              {chip.label}
            </Box>
          );
        })}
      </Box>

      {/* Table */}
      <Box sx={{ ...acrylicSurfaceSx, overflow: "hidden", borderRadius: "16px", border: "1px solid var(--sla-border)", boxShadow: "0 1px 2px rgba(17,24,39,.04)" }}>
        <Box
          sx={{
            display: "grid", borderBottom: "1px solid var(--sla-border-soft)", bgcolor: "var(--sla-surface-muted)", px: "22px", py: 1.25,
            fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.05em", color: "var(--sla-fg3)",
            gridTemplateColumns: cols,
          }}
        >
          <span>Issue</span>
          <span>Project</span>
          <span>Opened by</span>
          <span>Pri</span>
          <span>Status</span>
          <span>SLA state</span>
          <span>Budget</span>
          <Box component="span" sx={{ textAlign: "right" }}>Age</Box>
        </Box>

        {isError && !issues ? (
          <ErrorState message={errorMessage(error, "Failed to load issues")} onRetry={() => void refetch()} />
        ) : isLoading ? (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1, p: 2 }}>
            {Array.from({ length: rowsPerPage }).map((_, i) => (
              <Skeleton key={i} variant="rounded" sx={{ height: 36, width: "100%" }} />
            ))}
          </Box>
        ) : (issues?.length ?? 0) === 0 ? (
          <Box sx={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 1.25, px: 2.5, py: 6 }}>
            <Box component="span" sx={{ display: "flex", height: 40, width: 40, alignItems: "center", justifyContent: "center", borderRadius: "50%", bgcolor: "var(--sla-ok-tint)", fontSize: 20, fontWeight: 700, color: "var(--sla-ok)" }}>✓</Box>
            <Box component="span" sx={{ fontSize: 14, fontWeight: 600, color: "var(--sla-ok)" }}>No matching issues</Box>
            <Box component="span" sx={{ fontSize: 12.5, color: "var(--sla-fg3)" }}>Nothing matches this filter combination right now.</Box>
          </Box>
        ) : (
          issues!.map((issue) => (
            <IssueTimelineRow
              key={issue.id}
              issue={issue}
              variant="full"
              projectName={projectNameFor(overview?.projects, issue.repo)}
              isCsStatus={isCsStatus}
            />
          ))
        )}

        {total > 0 && (
          <TablePagination
            component="div"
            count={total}
            page={page}
            onPageChange={(_, newPage) => setPage(newPage)}
            rowsPerPage={rowsPerPage}
            onRowsPerPageChange={(e) => setRowsPerPage(Number(e.target.value))}
            rowsPerPageOptions={ROWS_PER_PAGE_OPTIONS}
            showFirstButton
            showLastButton
            sx={{ borderTop: "1px solid var(--sla-border-soft)" }}
          />
        )}
      </Box>

      <Box sx={{ mt: 3, textAlign: "center", fontSize: 11.5, color: "var(--sla-no-sla)" }}>
        Read-only · issues open in GitHub in a new tab · Closed &amp; Terminal issues excluded
      </Box>
    </Box>
  );
}
