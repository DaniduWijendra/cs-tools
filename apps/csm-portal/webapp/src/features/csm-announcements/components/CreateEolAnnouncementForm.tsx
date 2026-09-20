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
  CircularProgress,
  FormControl,
  FormHelperText,
  Grid,
  InputLabel,
  MenuItem,
  Select,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { useEffect, useMemo, useState, type JSX } from "react";
import EditorWithSourceToggle from "@components/rich-text-editor/EditorWithSourceToggle";
import { useErrorBanner } from "@context/error-banner/ErrorBannerContext";
import { useAnnouncementDryRun, DRY_RUN_TAG_LABEL } from "@features/csm-announcements/api/useAnnouncementDryRun";
import { useResolveProductVersionAudience } from "@features/csm-announcements/api/useResolveProductVersionAudience";
import { useCreateAnnouncementRequest } from "@features/csm-announcements/api/useCreateAnnouncementRequest";
import { useUpdateAnnouncementRequest } from "@features/csm-announcements/api/useUpdateAnnouncementRequest";
import { useRecordAnnouncementRequestDryRun } from "@features/csm-announcements/api/useRecordAnnouncementRequestDryRun";
import { useSubmitAnnouncementRequest } from "@features/csm-announcements/api/useSubmitAnnouncementRequest";
import AnnouncementDryRunCard from "@features/csm-announcements/components/AnnouncementDryRunCard";
import ResolvedAudienceList from "@features/csm-announcements/components/ResolvedAudienceList";
import type { EolAudienceDefinition } from "@features/csm-announcements/types/announcementRequests";
import { useSearchProducts } from "@features/csm-projects/api/useSearchProducts";
import { useSearchProductVersions } from "@features/csm-projects/api/useSearchProductVersions";
import { useNavTransition } from "@hooks/useNavTransition";
import { formatDateOnlyForDisplay } from "@utils/dateTime";

/** The rich-text editor emits `<p></p>` when empty; check the stripped text. */
function isEmptyHtml(html: string): boolean {
  return html.replace(/<[^>]*>/g, "").replace(/&nbsp;/g, " ").trim().length === 0;
}

const PENDING_TARGET = "/announcements?tab=pending";

const DRY_RUN_TAG_LABELS = [DRY_RUN_TAG_LABEL];

/**
 * The "Product version / EOL announcement" flow (Option 2 of the two-option
 * announcement create page — see AnnouncementKindSelector). Builds a
 * `kind: "eol"` announcement request (Phase 2's draft/approval workflow)
 * rather than sending immediately — see CreateCustomerAnnouncementForm's own
 * doc comment for the shared draft/dry-run/submit design this mirrors; this
 * form only differs in its audience shape (`{productId, productVersionId}`,
 * no scope choice) and in having no security-announcement concept at all.
 *
 * The resolved audience always excludes Restricted/Suspended projects and
 * Cloud Support/Cloud Evaluation Support subscriptions — this is applied
 * unconditionally by the backend (see useResolveProductVersionAudience's own
 * doc comment), not a filter this form offers a toggle for.
 */
export default function CreateEolAnnouncementForm(): JSX.Element {
  const navigate = useNavTransition();
  const { showError } = useErrorBanner();

  const [productId, setProductId] = useState("");
  const [productVersionId, setProductVersionId] = useState("");
  const [subject, setSubject] = useState("");
  const [description, setDescription] = useState("");
  const [savingDraft, setSavingDraft] = useState(false);

  const [draftId, setDraftId] = useState<string | null>(null);
  const [dryRunRecordedForCaseId, setDryRunRecordedForCaseId] = useState<string | null>(null);

  const createDraft = useCreateAnnouncementRequest();
  const updateDraft = useUpdateAnnouncementRequest(draftId ?? undefined);
  const recordDryRun = useRecordAnnouncementRequestDryRun(draftId ?? undefined);
  const submitRequest = useSubmitAnnouncementRequest(draftId ?? undefined);

  const { data: products, isLoading: productsLoading } = useSearchProducts();
  const { data: versions, isLoading: versionsLoading } = useSearchProductVersions(
    productId || undefined,
  );

  const handleProductChange = (nextProductId: string): void => {
    setProductId(nextProductId);
    setProductVersionId("");
  };

  const resolvedAudience = useResolveProductVersionAudience(
    productId || undefined,
    productVersionId || undefined,
  );

  const { runningDryRun, dryRunResult, canRunDryRun, handleRunDryRun } = useAnnouncementDryRun({
    subject,
    description,
    tagLabels: DRY_RUN_TAG_LABELS,
    extraCanRun: !savingDraft,
  });

  const audienceDefinition: EolAudienceDefinition = useMemo(
    () => ({ productId, productVersionId }),
    [productId, productVersionId],
  );

  // See CreateCustomerAnnouncementForm's identical trio of effects for the
  // full reasoning — running a dry run is what actually creates the draft;
  // recording it re-syncs whatever content was just verified; audience
  // (here, product/version) is kept in sync independently since the dry run
  // never touches it.
  useEffect(() => {
    if (dryRunResult && !draftId && !createDraft.isPending) {
      createDraft.mutate(
        { kind: "eol", subject: subject.trim(), description, isSecurityAnnouncement: false, audienceDefinition },
        { onSuccess: (created) => setDraftId(created.id) },
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dryRunResult, draftId]);

  useEffect(() => {
    if (dryRunResult && draftId && dryRunRecordedForCaseId !== dryRunResult.caseId) {
      setDryRunRecordedForCaseId(dryRunResult.caseId);
      recordDryRun.mutate(
        { caseId: dryRunResult.caseId },
        {
          onSuccess: () =>
            updateDraft.mutate({ subject: subject.trim(), description, audienceDefinition }),
        },
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dryRunResult, draftId, dryRunRecordedForCaseId]);

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

  const dryRunConfirmed = !!dryRunResult && dryRunRecordedForCaseId === dryRunResult.caseId;

  const canSaveDraft =
    !!productId && !!productVersionId && subject.trim().length > 0 && !isEmptyHtml(description) && !savingDraft;

  const canSubmit = useMemo(
    () =>
      dryRunConfirmed &&
      !!draftId &&
      !resolvedAudience.isLoading &&
      // See CreateCustomerAnnouncementForm's identical check: a stale
      // successful fetch can leave `total` looking populated after a later
      // refetch fails.
      !resolvedAudience.isError &&
      resolvedAudience.total > 0 &&
      !submitRequest.isPending,
    [
      dryRunConfirmed,
      draftId,
      resolvedAudience.isLoading,
      resolvedAudience.isError,
      resolvedAudience.total,
      submitRequest.isPending,
    ],
  );

  const handleSaveDraft = async (): Promise<void> => {
    if (!canSaveDraft) return;
    setSavingDraft(true);
    try {
      if (draftId) {
        await updateDraft.mutateAsync({ subject: subject.trim(), description, audienceDefinition });
      } else {
        await createDraft.mutateAsync({
          kind: "eol",
          subject: subject.trim(),
          description,
          isSecurityAnnouncement: false,
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

  const selectedVersion = (versions ?? []).find((v) => v.id === productVersionId);
  const eolDateLabel = selectedVersion
    ? (formatDateOnlyForDisplay(selectedVersion.supportEolDate) ??
      (formatDateOnlyForDisplay(selectedVersion.earliestPossibleSupportEolDate)
        ? `Est. ${formatDateOnlyForDisplay(selectedVersion.earliestPossibleSupportEolDate)}`
        : null))
    : null;

  return (
    <Card variant="outlined" sx={{ p: 3 }}>
      <Grid container spacing={2.5}>
        <Grid size={{ xs: 12 }}>
          <Typography variant="subtitle2" sx={{ mb: 1 }}>
            Affected product version
          </Typography>
          <Box sx={{ display: "flex", gap: 1.5 }}>
            <FormControl size="small" fullWidth required disabled={savingDraft}>
              <InputLabel id="eol-product-label" shrink={productId !== ""} sx={{ top: "0px !important" }}>
                Product
              </InputLabel>
              <Select
                labelId="eol-product-label"
                label="Product"
                value={productId}
                onChange={(e) => handleProductChange(e.target.value as string)}
                notched={productId !== ""}
                startAdornment={
                  productsLoading ? <CircularProgress size={16} sx={{ mr: 1 }} /> : null
                }
              >
                {(products ?? []).map((p) => (
                  <MenuItem key={p.id} value={p.id}>
                    {p.name ?? p.id}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            <FormControl size="small" fullWidth required disabled={!productId || savingDraft}>
              <InputLabel id="eol-version-label" shrink={productVersionId !== ""} sx={{ top: "0px !important" }}>
                Version
              </InputLabel>
              <Select
                labelId="eol-version-label"
                label="Version"
                value={productVersionId}
                onChange={(e) => setProductVersionId(e.target.value as string)}
                notched={productVersionId !== ""}
                startAdornment={
                  versionsLoading ? <CircularProgress size={16} sx={{ mr: 1 }} /> : null
                }
              >
                {(versions ?? []).map((v) => (
                  <MenuItem key={v.id} value={v.id}>
                    {v.version ?? v.id}
                  </MenuItem>
                ))}
              </Select>
              {!productId && <FormHelperText>Select a product first.</FormHelperText>}
              {productId !== "" && !versionsLoading && (versions ?? []).length === 0 && (
                <FormHelperText>No versions available for this product.</FormHelperText>
              )}
            </FormControl>
          </Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1 }}>
            {eolDateLabel ? `End of support: ${eolDateLabel}. ` : ""}
            Always excludes Restricted or Suspended projects, and Cloud Support / Cloud Evaluation
            Support subscriptions — this can&apos;t be turned off.
          </Typography>
        </Grid>

        {productId !== "" && productVersionId !== "" && (
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
            helperText={subject.length >= 160 ? `${subject.length}/200` : undefined}
            disabled={savingDraft}
          />
        </Grid>
        <Grid size={{ xs: 12 }}>
          <Typography
            id="eol-announcement-description-label"
            component="label"
            variant="caption"
            color="text.secondary"
            sx={{ display: "block", mb: 0.5 }}
          >
            Description
          </Typography>
          {/* Editor doesn't accept an `id`, so associate the label by wrapping
              the editor in a labelled group for assistive tech. */}
          <Box role="group" aria-labelledby="eol-announcement-description-label">
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
