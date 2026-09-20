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

import {
  Box,
  Button,
  Card,
  Checkbox,
  FormControlLabel,
  Grid,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { useEffect, useMemo, useState, type JSX } from "react";
import type { BeSubscriptionType } from "@api/backend/types";
import EditorWithSourceToggle from "@components/rich-text-editor/EditorWithSourceToggle";
import { useErrorBanner } from "@context/error-banner/ErrorBannerContext";
import { DRY_RUN_TAG_LABEL, useAnnouncementDryRun } from "@features/csm-announcements/api/useAnnouncementDryRun";
import { useAnnouncementExcludedProjectKeys } from "@features/csm-announcements/api/useAnnouncementExcludedProjectKeys";
import { useResolveAnnouncementAudience } from "@features/csm-announcements/api/useResolveAnnouncementAudience";
import { useCreateAnnouncementRequest } from "@features/csm-announcements/api/useCreateAnnouncementRequest";
import { useUpdateAnnouncementRequest } from "@features/csm-announcements/api/useUpdateAnnouncementRequest";
import { useRecordAnnouncementRequestDryRun } from "@features/csm-announcements/api/useRecordAnnouncementRequestDryRun";
import { useSubmitAnnouncementRequest } from "@features/csm-announcements/api/useSubmitAnnouncementRequest";
import AnnouncementDryRunCard from "@features/csm-announcements/components/AnnouncementDryRunCard";
import AudienceScopeControls, {
  type AnnouncementAudienceScope,
} from "@features/csm-announcements/components/AudienceScopeControls";
import ResolvedAudienceList from "@features/csm-announcements/components/ResolvedAudienceList";
import type { CustomerAudienceDefinition } from "@features/csm-announcements/types/announcementRequests";
import { useNavTransition } from "@hooks/useNavTransition";

/**
 * The two default exclusions offered for the "all customer projects" scope,
 * mirroring the ServiceNow flow conditions this replaces (see
 * AudienceScopeControls' own doc comment for the Account Life Cycle caveat).
 */
const CLOUD_SUBSCRIPTION_TYPES: BeSubscriptionType[] = ["cloud_support", "cloud_evaluation_support"];
const CLOSED_STATES: string[] = ["Restricted", "Suspended"];

/**
 * The fixed tag label attached to every case in a security announcement.
 * There is no dedicated "announcement type" field anywhere in the platform
 * yet (confirmed: CreateCaseRequest's announcement branch only validates
 * subject/description) — this reuses the existing free-text case-tag
 * mechanism, which already works end-to-end for announcement cases and
 * already renders as a chip on the case detail page's Details tab with no
 * further changes needed. A fixed, exact string (rather than free typing)
 * is what makes it findable/filterable later despite tags having no closed
 * vocabulary on the backend.
 */
export const SECURITY_ANNOUNCEMENT_TAG_LABEL = "Security Announcement";

/** The rich-text editor emits `<p></p>` when empty; check the stripped text. */
function isEmptyHtml(html: string): boolean {
  return html.replace(/<[^>]*>/g, "").replace(/&nbsp;/g, " ").trim().length === 0;
}

const PENDING_TARGET = "/announcements?tab=pending";

/**
 * The "Create announcement for customers" flow (Option 1 of the two-option
 * announcement create page — see AnnouncementKindSelector). Builds a
 * `kind: "customer"` announcement request (Phase 2's draft/approval
 * workflow) rather than sending immediately: this page only ever produces a
 * brand-new `draft` and, optionally, submits it for approval — every other
 * lifecycle step (further edits, re-approval, publish) happens through
 * {@link AnnouncementRequestDialog} from the registry's "Pending" tab, not
 * here. That's a deliberate boundary (see the Phase 2 plan): this component
 * doesn't need any "resume an existing draft" state machine of its own.
 *
 * Two audience scopes (see AudienceScopeControls): a hand-picked project
 * list, or "all customer projects" resolved from the entity service under a
 * pair of default exclusions. `ResolvedAudienceList` here is purely a
 * pre-submit review aid (matching Phase 1's "resolved recipient list before
 * send" intent) — the actual audience that gets messaged is resolved fresh,
 * server-side, at Submit time from the request's stored `audienceDefinition`
 * (with the mandatory excluded-project-key denylist applied), not from
 * whatever this list happened to show a moment earlier.
 *
 * "Dry run" is the shared useAnnouncementDryRun/AnnouncementDryRunCard pair
 * (also used by the EOL/product-version flow) — creates exactly one real
 * case in a single fixed test project. Running it for the first time lazily
 * creates the draft (see the effects below); recording it on the draft is
 * what unblocks "Submit for approval" — mirrors the mandatory dry-run gate
 * `AnnouncementRequestService.Submit` enforces server-side.
 */
export default function CreateCustomerAnnouncementForm(): JSX.Element {
  const navigate = useNavTransition();
  const { showError } = useErrorBanner();

  const [scope, setScope] = useState<AnnouncementAudienceScope>("specific");
  const [projectIds, setProjectIds] = useState<string[]>([]);
  const [excludeCloudTypes, setExcludeCloudTypes] = useState(true);
  const [excludeClosedStates, setExcludeClosedStates] = useState(true);
  const [subject, setSubject] = useState("");
  const [description, setDescription] = useState("");
  const [isSecurityAnnouncement, setIsSecurityAnnouncement] = useState(false);
  const [savingDraft, setSavingDraft] = useState(false);

  const [draftId, setDraftId] = useState<string | null>(null);
  const [dryRunRecordedForCaseId, setDryRunRecordedForCaseId] = useState<string | null>(null);

  const createDraft = useCreateAnnouncementRequest();
  const updateDraft = useUpdateAnnouncementRequest(draftId ?? undefined);
  const recordDryRun = useRecordAnnouncementRequestDryRun(draftId ?? undefined);
  const submitRequest = useSubmitAnnouncementRequest(draftId ?? undefined);

  const dryRunTagLabels = useMemo(
    () => [DRY_RUN_TAG_LABEL, ...(isSecurityAnnouncement ? [SECURITY_ANNOUNCEMENT_TAG_LABEL] : [])],
    [isSecurityAnnouncement],
  );
  const { runningDryRun, dryRunResult, canRunDryRun, handleRunDryRun } = useAnnouncementDryRun({
    subject,
    description,
    tagLabels: dryRunTagLabels,
    extraCanRun: !savingDraft,
  });

  const audienceFilters = useMemo(
    () => ({
      excludeSubscriptionTypes: excludeCloudTypes ? CLOUD_SUBSCRIPTION_TYPES : [],
      excludeClosureStates: excludeClosedStates ? CLOSED_STATES : [],
    }),
    [excludeCloudTypes, excludeClosedStates],
  );
  const resolvedAudience = useResolveAnnouncementAudience(scope === "all", audienceFilters);
  const excludedProjectKeysQuery = useAnnouncementExcludedProjectKeys();

  // The scope actually being sent: today's hand-picked list, or every id
  // resolved for "all customer projects" (see AudienceScopeControls' own doc
  // comment on what that scope does and doesn't filter). Just a pre-submit
  // sanity check here — the real audience is resolved again server-side.
  const targetProjectIds = useMemo(
    () => (scope === "all" ? resolvedAudience.projects.map((p) => p.id) : projectIds),
    [scope, resolvedAudience.projects, projectIds],
  );

  const audienceDefinition: CustomerAudienceDefinition = useMemo(
    () =>
      scope === "specific"
        ? { scope: "specific", projectIds }
        : {
            scope: "all",
            excludeClosureStates: excludeClosedStates ? CLOSED_STATES : [],
            excludeSubscriptionTypes: excludeCloudTypes ? CLOUD_SUBSCRIPTION_TYPES : [],
          },
    [scope, projectIds, excludeClosedStates, excludeCloudTypes],
  );

  // A dry run is the trigger that actually creates the draft — running one
  // is the mandatory first step either way, so there's no reason to make the
  // sender explicitly "Save as draft" first just to give the dry run
  // somewhere to attach to.
  useEffect(() => {
    if (dryRunResult && !draftId && !createDraft.isPending) {
      createDraft.mutate(
        {
          kind: "customer",
          subject: subject.trim(),
          description,
          isSecurityAnnouncement,
          audienceDefinition,
        },
        { onSuccess: (created) => setDraftId(created.id) },
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dryRunResult, draftId]);

  // Persists the dry run onto the draft once both exist, then re-syncs the
  // draft's content to whatever was actually just dry-run-verified — the
  // draft may have been created earlier (or with different content, if the
  // sender edited subject/description between an earlier dry run and this
  // one) and drifted since.
  useEffect(() => {
    if (dryRunResult && draftId && dryRunRecordedForCaseId !== dryRunResult.caseId) {
      setDryRunRecordedForCaseId(dryRunResult.caseId);
      recordDryRun.mutate(
        { caseId: dryRunResult.caseId },
        {
          onSuccess: () =>
            updateDraft.mutate({
              subject: subject.trim(),
              description,
              isSecurityAnnouncement,
              audienceDefinition,
            }),
        },
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dryRunResult, draftId, dryRunRecordedForCaseId]);

  // The dry run itself never touches audience (it always targets the fixed
  // test project), so an audience-only edit after a dry run has already been
  // recorded wouldn't otherwise re-sync — keep the draft's audience current
  // independently of dry-run state.
  useEffect(() => {
    if (!draftId) return;
    updateDraft.mutate({ audienceDefinition });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draftId, audienceDefinition]);

  useEffect(() => {
    if (createDraft.isError) {
      showError("Could not save this draft. Please try again.");
    }
  }, [createDraft.isError, showError]);

  // True only once a dry run has been both run AND confirmed persisted onto
  // the current draft — mirrors AnnouncementRequestService.Submit's own
  // server-side gate, so this button's disabled state never promises
  // something the actual submit call would then reject.
  const dryRunConfirmed = !!dryRunResult && dryRunRecordedForCaseId === dryRunResult.caseId;

  const canSaveDraft = subject.trim().length > 0 && !isEmptyHtml(description) && !savingDraft;

  const canSubmit = useMemo(
    () =>
      dryRunConfirmed &&
      !!draftId &&
      targetProjectIds.length > 0 &&
      !(scope === "all" && resolvedAudience.isLoading) &&
      // TanStack Query can retain a previous successful fetch's `data` after
      // a later refetch fails (isLoading goes back to false, but the stale
      // list is still sitting there) — without this check, targetProjectIds
      // would still look populated and a resolution failure could let the
      // sender submit against an audience that's actually out of date.
      !(scope === "all" && resolvedAudience.isError) &&
      !submitRequest.isPending,
    [
      dryRunConfirmed,
      draftId,
      targetProjectIds,
      scope,
      resolvedAudience.isLoading,
      resolvedAudience.isError,
      submitRequest.isPending,
    ],
  );

  const handleSaveDraft = async (): Promise<void> => {
    if (!canSaveDraft) return;
    setSavingDraft(true);
    try {
      if (draftId) {
        await updateDraft.mutateAsync({
          subject: subject.trim(),
          description,
          isSecurityAnnouncement,
          audienceDefinition,
        });
      } else {
        await createDraft.mutateAsync({
          kind: "customer",
          subject: subject.trim(),
          description,
          isSecurityAnnouncement,
          audienceDefinition,
        });
      }
      navigate(PENDING_TARGET);
    } catch {
      showError("Could not save this draft. Please try again.");
    } finally {
      setSavingDraft(false);
    }
  };

  const handleSubmitForApproval = async (): Promise<void> => {
    if (!canSubmit) return;
    try {
      await submitRequest.mutateAsync();
      navigate(PENDING_TARGET);
    } catch (error) {
      showError(
        error instanceof Error && error.message.trim()
          ? error.message
          : "Could not submit this request for approval. Please try again.",
      );
    }
  };

  return (
    <Card variant="outlined" sx={{ p: 3 }}>
      <Grid container spacing={2.5}>
        <Grid size={{ xs: 12 }}>
          <AudienceScopeControls
            scope={scope}
            onScopeChange={setScope}
            projectIds={projectIds}
            onProjectIdsChange={setProjectIds}
            excludeCloudTypes={excludeCloudTypes}
            onExcludeCloudTypesChange={setExcludeCloudTypes}
            excludeClosedStates={excludeClosedStates}
            onExcludeClosedStatesChange={setExcludeClosedStates}
            excludedProjectKeys={excludedProjectKeysQuery.data ?? []}
            disabled={savingDraft}
          />
        </Grid>

        {scope === "all" && (
          <Grid size={{ xs: 12 }}>
            <ResolvedAudienceList
              projects={resolvedAudience.projects}
              total={resolvedAudience.total}
              isLoading={resolvedAudience.isLoading}
              isError={resolvedAudience.isError}
            />
          </Grid>
        )}

        <Grid size={{ xs: 12 }}>
          <TextField
            label="Subject"
            size="small"
            fullWidth
            required
            value={subject}
            onChange={(e) => setSubject(e.target.value.slice(0, 200))}
            helperText={
              subject.length >= 160 ? `${subject.length}/200` : undefined
            }
          />
        </Grid>
        <Grid size={{ xs: 12 }}>
          <FormControlLabel
            control={
              <Checkbox
                size="small"
                checked={isSecurityAnnouncement}
                disabled={savingDraft}
                onChange={(e) => setIsSecurityAnnouncement(e.target.checked)}
              />
            }
            label="This is a security announcement"
          />
          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: -0.5 }}>
            Attaches a &quot;{SECURITY_ANNOUNCEMENT_TAG_LABEL}&quot; label to every case this
            creates.
          </Typography>
        </Grid>
        <Grid size={{ xs: 12 }}>
          <Typography
            id="announcement-description-label"
            component="label"
            variant="caption"
            color="text.secondary"
            sx={{ display: "block", mb: 0.5 }}
          >
            Description
          </Typography>
          {/* Editor doesn't accept an `id`, so associate the label by wrapping
              the editor in a labelled group for assistive tech. */}
          <Box role="group" aria-labelledby="announcement-description-label">
            <EditorWithSourceToggle
              value={description}
              onChange={setDescription}
              placeholder="Describe the announcement…"
              minHeight={180}
              maxHeight={420}
              toolbarVariant="full"
              disabled={savingDraft}
            />
          </Box>
        </Grid>
      </Grid>

      <AnnouncementDryRunCard
        runningDryRun={runningDryRun}
        dryRunResult={dryRunResult}
        canRunDryRun={canRunDryRun}
        onRunDryRun={() => void handleRunDryRun()}
      />

      <Box
        sx={{
          display: "flex",
          justifyContent: "flex-end",
          alignItems: "center",
          gap: 1.5,
          mt: 2.5,
          pt: 2,
          borderTop: 1,
          borderColor: "divider",
        }}
      >
        <Button variant="outlined" onClick={() => navigate("/announcements")}>
          Cancel
        </Button>
        <Button variant="outlined" onClick={() => void handleSaveDraft()} disabled={!canSaveDraft}>
          {savingDraft ? "Saving…" : "Save as draft"}
        </Button>
        <Button variant="contained" onClick={() => void handleSubmitForApproval()} disabled={!canSubmit}>
          {submitRequest.isPending ? "Submitting…" : "Submit for approval"}
        </Button>
      </Box>
      {!dryRunConfirmed && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", textAlign: "right", mt: 0.5 }}>
          Run a dry run above before submitting for approval.
        </Typography>
      )}
    </Card>
  );
}
