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

import { useEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import {
  Box,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControlLabel,
  IconButton,
  LinearProgress,
  Skeleton,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { RefreshCw, X } from "@wso2/oxygen-ui-icons-react";
import { Link } from "react-router";
import EditorWithSourceToggle from "@components/rich-text-editor/EditorWithSourceToggle";
import { formatAbsoluteForUser } from "@utils/dateTime";
import { sanitizeRichTextHtml } from "@utils/sanitizeHtml";
import {
  DRY_RUN_TAG_LABEL,
  useAnnouncementDryRun,
} from "@features/csm-announcements/api/useAnnouncementDryRun";
import { useGetAnnouncementRequest } from "@features/csm-announcements/api/useGetAnnouncementRequest";
import { useUpdateAnnouncementRequest } from "@features/csm-announcements/api/useUpdateAnnouncementRequest";
import { useRecordAnnouncementRequestDryRun } from "@features/csm-announcements/api/useRecordAnnouncementRequestDryRun";
import { useSubmitAnnouncementRequest } from "@features/csm-announcements/api/useSubmitAnnouncementRequest";
import { useApproveAnnouncementRequest } from "@features/csm-announcements/api/useApproveAnnouncementRequest";
import { usePublishAnnouncementRequest } from "@features/csm-announcements/api/usePublishAnnouncementRequest";
import { SECURITY_ANNOUNCEMENT_TAG_LABEL } from "@features/csm-announcements/components/CreateCustomerAnnouncementForm";

interface AnnouncementRequestDialogProps {
  requestId: string;
  onClose: () => void;
}

const STATE_TITLE: Record<string, string> = {
  draft: "Draft",
  pending_approval: "Pending approval",
  approved: "Approved",
  published: "Published",
};

function DetailField({ label, children }: { label: string; children: ReactNode }): JSX.Element {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.25, minWidth: 0 }}>
      <Typography variant="caption" color="text.secondary">
        {label}
      </Typography>
      <Box>{children}</Box>
    </Box>
  );
}

function whoWhen(who?: string | null, when?: string | null): string {
  if (!who && !when) return "—";
  const whenText = when ? formatAbsoluteForUser(when) : null;
  if (who && whenText) return `${who} · ${whenText}`;
  return who ?? whenText ?? "—";
}

/** The rich-text editor emits `<p></p>` when empty; check the stripped text. */
function isEmptyHtml(html: string): boolean {
  return html.replace(/<[^>]*>/g, "").replace(/&nbsp;/g, " ").trim().length === 0;
}

/**
 * Detail + action dialog for a not-yet-published announcement request,
 * opened from the registry page's "Pending" tab. One dialog covers every
 * state rather than a separate route per state — the record is small and the
 * state-appropriate actions (Submit / Mark as approved / Publish) belong
 * right next to the content they act on. See the Phase 2 plan's own
 * per-state edit-behavior breakdown, mirrored exactly below:
 *  - draft: content is freely editable; "Submit for approval" itself runs
 *    the dry run (creating the one real case in the fixed test project),
 *    records it, and submits — one action, not a separate "run a dry run
 *    first" step, since the dry-run case *is* what gets shared with the
 *    approver (there's nothing to gain from reviewing it before submitting;
 *    editing afterward while `pending_approval` reverts to draft anyway).
 *  - pending_approval: read-only until "Edit" is explicitly confirmed —
 *    editing reverts the request to draft and clears its dry run, since the
 *    content is out for real review over email and a silent change under
 *    the reviewer isn't safe.
 *  - approved: content stays editable in place with no state reset (a human
 *    already said yes over email) — but the frozen audience snapshot from
 *    submit time is read-only here; Publish sends whatever's currently in
 *    the fields to that exact snapshot.
 *  - published: read-only summary, nothing left to do.
 */
export default function AnnouncementRequestDialog({
  requestId,
  onClose,
}: AnnouncementRequestDialogProps): JSX.Element {
  const { data: request, isLoading, isError, refetch } = useGetAnnouncementRequest(requestId);
  const update = useUpdateAnnouncementRequest();
  const recordDryRun = useRecordAnnouncementRequestDryRun();
  const submit = useSubmitAnnouncementRequest();
  const approve = useApproveAnnouncementRequest();
  const publish = usePublishAnnouncementRequest(request);

  const [subject, setSubject] = useState("");
  const [description, setDescription] = useState("");
  const [isSecurityAnnouncement, setIsSecurityAnnouncement] = useState(false);
  const [pendingApprovalEditUnlocked, setPendingApprovalEditUnlocked] = useState(false);
  const [confirmEditOpen, setConfirmEditOpen] = useState(false);

  // Re-sync local editable fields whenever the server's own copy changes —
  // covers both the initial load and a save round-tripping back with the
  // server's canonical value.
  useEffect(() => {
    if (!request) return;
    setSubject(request.subject);
    setDescription(request.description);
    setIsSecurityAnnouncement(request.isSecurityAnnouncement);
    // Deliberately narrowed to the specific fields read above, not the whole
    // `request` object, so this doesn't re-fire (and stomp in-progress local
    // edits) on every refetch that leaves those fields unchanged.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [request?.id, request?.subject, request?.description, request?.isSecurityAnnouncement]);

  // A save that reverts pending_approval to draft moves request.state itself,
  // so the "unlocked" flag no longer applies to whatever state comes next —
  // clear it rather than let a stale unlock leak into a future re-approval.
  useEffect(() => {
    if (request?.state !== "pending_approval") setPendingApprovalEditUnlocked(false);
  }, [request?.state]);

  const dryRunTagLabels = useMemo(
    () =>
      isSecurityAnnouncement
        ? [DRY_RUN_TAG_LABEL, SECURITY_ANNOUNCEMENT_TAG_LABEL]
        : [DRY_RUN_TAG_LABEL],
    [isSecurityAnnouncement],
  );
  const dryRun = useAnnouncementDryRun({
    subject,
    description,
    tagLabels: dryRunTagLabels,
    extraCanRun: request?.state === "draft" && !submit.isPending && !recordDryRun.isPending,
  });

  const isEditable =
    request?.state === "draft" ||
    request?.state === "approved" ||
    (request?.state === "pending_approval" && pendingApprovalEditUnlocked);

  const handleSaveContent = (): void => {
    if (!request) return;
    update.mutate({
      id: request.id,
      subject: subject.trim(),
      description,
      isSecurityAnnouncement,
    });
  };

  const submittingForApproval = dryRun.runningDryRun || recordDryRun.isPending || submit.isPending;

  // "Submit for approval" runs the dry run, records it, and submits in one
  // action — see this component's own doc comment for why there's no
  // separate "run a dry run first" step here. Each mutation's own `isError`
  // already renders inline below, so a failure partway through just leaves
  // the button re-clickable rather than needing its own error handling here.
  const handleSubmitForApproval = async (): Promise<void> => {
    if (!request || submittingForApproval) return;
    const result = await dryRun.handleRunDryRun();
    if (!result) return;
    try {
      await recordDryRun.mutateAsync({ id: request.id, caseId: result.caseId });
      await submit.mutateAsync({ id: request.id });
    } catch {
      /* surfaced inline via recordDryRun.isError / submit.isError below */
    }
  };

  const dryRunLink = request?.dryRunCaseId ? (
    <Link to={`/announcements/${request.dryRunCaseId}`} target="_blank" rel="noopener noreferrer">
      view dry run
    </Link>
  ) : (
    "not yet run"
  );

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1 }}>
        <Typography variant="subtitle1" component="span">
          Announcement request{request ? ` · ${STATE_TITLE[request.state] ?? request.state}` : ""}
        </Typography>
        <IconButton size="small" onClick={onClose} aria-label="Close">
          <X size={16} />
        </IconButton>
      </DialogTitle>
      <DialogContent dividers sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {isLoading && (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
            <Skeleton variant="text" width="60%" />
            <Skeleton variant="rounded" height={120} />
          </Box>
        )}

        {isError && (
          <Box sx={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 1, py: 3 }}>
            <Typography variant="body2" color="error">
              Could not load this announcement request.
            </Typography>
            <Button
              size="small"
              variant="outlined"
              startIcon={<RefreshCw size={14} />}
              onClick={() => void refetch()}
            >
              Retry
            </Button>
          </Box>
        )}

        {!isLoading && !isError && request && (
          <>
            {isEditable ? (
              <>
                <TextField
                  label="Subject"
                  value={subject}
                  onChange={(e) => setSubject(e.target.value)}
                  fullWidth
                  size="small"
                />
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.5 }}>
                    Description
                  </Typography>
                  <EditorWithSourceToggle
                    value={description}
                    onChange={setDescription}
                    placeholder="Describe the announcement…"
                    minHeight={140}
                    maxHeight={320}
                    toolbarVariant="full"
                  />
                </Box>
                {request.kind === "customer" && (
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={isSecurityAnnouncement}
                        onChange={(e) => setIsSecurityAnnouncement(e.target.checked)}
                      />
                    }
                    label="Security announcement"
                  />
                )}
                <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                  <Button
                    variant="outlined"
                    size="small"
                    onClick={handleSaveContent}
                    disabled={update.isPending || subject.trim().length === 0}
                  >
                    {update.isPending ? "Saving…" : "Save changes"}
                  </Button>
                  {update.isError && (
                    <Typography variant="caption" color="error">
                      {update.error instanceof Error ? update.error.message : "Could not save changes."}
                    </Typography>
                  )}
                </Box>
              </>
            ) : (
              <>
                <Typography variant="body1" sx={{ fontWeight: 600 }}>
                  {request.subject || "(no subject)"}
                </Typography>
                <Box
                  sx={{ fontSize: "0.875rem", lineHeight: 1.5, wordBreak: "break-word" }}
                  dangerouslySetInnerHTML={{ __html: sanitizeRichTextHtml(request.description) }}
                />
              </>
            )}

            <Divider />

            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: "repeat(auto-fill, minmax(160px, 1fr))",
                gap: 2,
              }}
            >
              <DetailField label="Kind">
                <Typography variant="body2">{request.kind === "eol" ? "EOL" : "Customer"}</Typography>
              </DetailField>
              <DetailField label="Dry run">
                <Typography variant="body2">{dryRunLink}</Typography>
              </DetailField>
              <DetailField label="Audience">
                <Typography variant="body2">
                  {request.resolvedProjectCount != null
                    ? `${request.resolvedProjectCount} project${request.resolvedProjectCount === 1 ? "" : "s"}`
                    : "not resolved yet"}
                </Typography>
              </DetailField>
              <DetailField label="Created">
                <Typography variant="body2">{whoWhen(request.createdBy, request.createdAt)}</Typography>
              </DetailField>
              {request.submittedAt && (
                <DetailField label="Submitted">
                  <Typography variant="body2">{whoWhen(request.submittedBy, request.submittedAt)}</Typography>
                </DetailField>
              )}
              {request.approvedAt && (
                <DetailField label="Approved">
                  <Typography variant="body2">{whoWhen(request.approvedBy, request.approvedAt)}</Typography>
                </DetailField>
              )}
              {request.publishedAt && (
                <DetailField label="Published">
                  <Typography variant="body2">{whoWhen(request.publishedBy, request.publishedAt)}</Typography>
                </DetailField>
              )}
            </Box>

            {request.resolvedProjectIds && request.resolvedProjectIds.length > 0 && (
              <Box
                sx={{
                  border: 1,
                  borderColor: "divider",
                  borderRadius: 1,
                  maxHeight: 120,
                  overflowY: "auto",
                  p: 1,
                }}
              >
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ fontFamily: "monospace", whiteSpace: "pre-wrap" }}
                >
                  {request.resolvedProjectIds.join(", ")}
                </Typography>
              </Box>
            )}

            {request.state === "pending_approval" && !pendingApprovalEditUnlocked && (
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Button
                  variant="contained"
                  size="small"
                  onClick={() => approve.mutate({ id: request.id })}
                  disabled={approve.isPending}
                >
                  {approve.isPending ? "Approving…" : "Mark as approved"}
                </Button>
                <Button variant="text" size="small" onClick={() => setConfirmEditOpen(true)}>
                  Edit
                </Button>
                {approve.isError && (
                  <Typography variant="caption" color="error">
                    {approve.error instanceof Error ? approve.error.message : "Could not approve."}
                  </Typography>
                )}
              </Box>
            )}

            {request.state === "draft" && (
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Button
                  variant="contained"
                  color="primary"
                  size="small"
                  onClick={() => void handleSubmitForApproval()}
                  disabled={
                    submittingForApproval ||
                    subject.trim().length === 0 ||
                    isEmptyHtml(description)
                  }
                >
                  {dryRun.runningDryRun
                    ? "Running dry run…"
                    : submittingForApproval
                      ? "Submitting…"
                      : "Submit for approval"}
                </Button>
                <Typography variant="caption" color="text.secondary">
                  Creates a real case in the DCPSUB test project to share with your approver.
                </Typography>
                {(recordDryRun.isError || submit.isError) && (
                  <Typography variant="caption" color="error">
                    {submit.error instanceof Error
                      ? submit.error.message
                      : recordDryRun.error instanceof Error
                        ? recordDryRun.error.message
                        : "Could not submit for approval."}
                  </Typography>
                )}
              </Box>
            )}

            {request.state === "approved" && (
              <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
                {publish.progress && (
                  <Box>
                    <LinearProgress
                      variant="determinate"
                      value={(publish.progress.completed / publish.progress.total) * 100}
                    />
                    <Typography variant="caption" color="text.secondary">
                      Sending {publish.progress.completed} / {publish.progress.total}…
                    </Typography>
                  </Box>
                )}
                <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                  <Button
                    variant="contained"
                    color="primary"
                    size="small"
                    onClick={() => void publish.handlePublish()}
                    disabled={publish.publishing}
                  >
                    {publish.publishing
                      ? "Publishing…"
                      : publish.failedProjectIds.length > 0
                        ? "Retry failed projects"
                        : "Publish"}
                  </Button>
                </Box>
                {publish.failedProjectIds.length > 0 && !publish.publishing && (
                  <Typography variant="caption" color="error">
                    Failed for: {publish.failedProjectIds.join(", ")}
                  </Typography>
                )}
                {publish.failedTagProjectIds.length > 0 && (
                  <Typography variant="caption" color="warning.main">
                    Security label couldn't be attached for: {publish.failedTagProjectIds.join(", ")}
                  </Typography>
                )}
              </Box>
            )}
          </>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>

      {confirmEditOpen && (
        <Dialog open onClose={() => setConfirmEditOpen(false)} maxWidth="xs" fullWidth>
          <DialogTitle>Edit this request?</DialogTitle>
          <DialogContent>
            <Typography variant="body2">
              Editing will revert this request to draft and clear its recorded dry run, since the
              content is currently out for review over email. You'll need to run a new dry run
              and submit it again.
            </Typography>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setConfirmEditOpen(false)}>Cancel</Button>
            <Button
              variant="contained"
              color="warning"
              onClick={() => {
                setPendingApprovalEditUnlocked(true);
                setConfirmEditOpen(false);
              }}
            >
              Continue editing
            </Button>
          </DialogActions>
        </Dialog>
      )}
    </Dialog>
  );
}
