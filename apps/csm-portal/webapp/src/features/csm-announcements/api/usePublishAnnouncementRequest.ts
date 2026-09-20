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

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useBackendApi } from "@api/backend/client";
import { ApiQueryKeys } from "@constants/apiConstants";
import { useErrorBanner } from "@context/error-banner/ErrorBannerContext";
import { useAddTagToCase } from "@features/csm-cases/api/useCaseTags";
import { usePostCsmCase } from "@features/csm-cases/api/usePostCsmCase";
import { SECURITY_ANNOUNCEMENT_TAG_LABEL } from "@features/csm-announcements/components/CreateCustomerAnnouncementForm";
import {
  ANNOUNCEMENT_CASE_CREATE_CONCURRENCY_LIMIT,
  settleWithConcurrencyLimit,
} from "@features/csm-announcements/utils/settleWithConcurrencyLimit";
import type { AnnouncementRequest } from "@features/csm-announcements/types/announcementRequests";

export interface PublishProgress {
  completed: number;
  total: number;
}

export interface UsePublishAnnouncementRequestResult {
  /** True while a fan-out (initial attempt or retry) is in flight. */
  publishing: boolean;
  /** Live counter for the current fan-out; `null` when nothing is in flight. */
  progress: PublishProgress | null;
  /** Every resolved project id that has a real case so far, across all attempts. */
  succeededProjectIds: string[];
  /** Only the projects still outstanding after the most recent attempt — call `handlePublish` again to retry just these. */
  failedProjectIds: string[];
  failedTagProjectIds: string[];
  /** Set once every resolved project has a case and the backend has marked the request published. */
  published: AnnouncementRequest | null;
  handlePublish: () => Promise<void>;
}

/**
 * The real "send" for an `approved` announcement request: creates one real
 * case per id in `resolvedProjectIds` (the audience snapshot frozen at
 * submit time — see that field's own doc comment), then marks the request
 * `published` once every project has succeeded. This is the same
 * `settleWithConcurrencyLimit` fan-out `CreateCustomerAnnouncementForm`/
 * `CreateEolAnnouncementForm` already use for their own immediate-send
 * button (see those components' `handleSubmit`), pulled out here so the
 * pending-request dialog's Publish action gets the identical mechanism
 * rather than a re-implementation that could drift. Those two forms keep
 * their own copy until they're changed to create drafts instead of sending
 * immediately (a later PR) — this hook isn't extracted *from* them yet, just
 * built to the same shape so that extraction is a pure move once it happens.
 *
 * Unlike those forms' one-shot "succeeded or list the failures" ending, this
 * tracks cumulative success across attempts, so calling `handlePublish` again
 * after a partial failure only resends to the projects still outstanding —
 * already-succeeded projects are never sent a duplicate case. The backend
 * doesn't persist this progress anywhere (see the Phase 2 design note on no
 * per-project delivery ledger): if the dialog is closed mid-retry, progress
 * made so far is lost and a fresh attempt resends to every resolved project
 * again, since there's nowhere to read "which ones already went out" back
 * from.
 */
export function usePublishAnnouncementRequest(
  request: AnnouncementRequest | null | undefined,
): UsePublishAnnouncementRequestResult {
  const api = useBackendApi();
  const queryClient = useQueryClient();
  const { showError } = useErrorBanner();
  const postCase = usePostCsmCase();
  const addTag = useAddTagToCase();

  const [publishing, setPublishing] = useState(false);
  const [progress, setProgress] = useState<PublishProgress | null>(null);
  const [succeededProjectIds, setSucceededProjectIds] = useState<string[]>([]);
  const [failedProjectIds, setFailedProjectIds] = useState<string[]>([]);
  const [failedTagProjectIds, setFailedTagProjectIds] = useState<string[]>([]);
  const [published, setPublished] = useState<AnnouncementRequest | null>(null);

  const handlePublish = async (): Promise<void> => {
    if (!request || request.state !== "approved" || publishing) return;

    const allProjectIds = request.resolvedProjectIds ?? [];
    if (allProjectIds.length === 0) {
      showError("This request has no resolved audience to publish to.");
      return;
    }
    const pendingProjectIds = allProjectIds.filter((id) => !succeededProjectIds.includes(id));

    setPublishing(true);

    // Skip the fan-out entirely when every project already has a case from an
    // earlier attempt — but still fall through to the publish-marking call
    // below, since a retry here is exactly for the case where the fan-out
    // fully succeeded last time but *that* call failed. Returning early
    // instead (as this used to) left that state permanently stuck: nothing
    // was ever outstanding to retry, yet the request was never marked
    // published either.
    if (pendingProjectIds.length > 0) {
      setProgress({ completed: 0, total: pendingProjectIds.length });
      const newlyFailedTagIds: string[] = [];

      const results = await settleWithConcurrencyLimit(
        pendingProjectIds,
        ANNOUNCEMENT_CASE_CREATE_CONCURRENCY_LIMIT,
        async (projectId) => {
          const created = await postCase.mutateAsync({
            type: "announcement",
            projectId,
            subject: request.subject,
            description: request.description,
          });
          if (request.isSecurityAnnouncement) {
            try {
              await addTag.mutateAsync({ caseId: created.id, label: SECURITY_ANNOUNCEMENT_TAG_LABEL });
            } catch {
              newlyFailedTagIds.push(projectId);
            }
          }
          return created;
        },
        (completed, total) => setProgress({ completed, total }),
      );

      const newlySucceeded = pendingProjectIds.filter((_, i) => results[i].status === "fulfilled");
      const stillFailing = pendingProjectIds.filter((_, i) => results[i].status === "rejected");

      setSucceededProjectIds((prev) => [...prev, ...newlySucceeded]);
      setFailedProjectIds(stillFailing);
      setFailedTagProjectIds((prev) => [...prev, ...newlyFailedTagIds]);
      setProgress(null);

      if (stillFailing.length > 0) {
        setPublishing(false);
        showError(
          `Sent to ${newlySucceeded.length} of ${pendingProjectIds.length} remaining project${
            pendingProjectIds.length === 1 ? "" : "s"
          } — failed for project${stillFailing.length === 1 ? "" : "s"} ${stillFailing.join(
            ", ",
          )}. Retry to resend just those.`,
        );
        return;
      }
    }

    try {
      const result = await api.postEmpty<AnnouncementRequest>(
        `/announcement-requests/${encodeURIComponent(request.id)}/publish`,
      );
      setPublished(result);
      queryClient.invalidateQueries({
        queryKey: [ApiQueryKeys.ANNOUNCEMENT_REQUEST_DETAIL, request.id],
      });
      queryClient.invalidateQueries({ queryKey: [ApiQueryKeys.ANNOUNCEMENT_REQUESTS_SEARCH] });
      queryClient.invalidateQueries({ queryKey: [ApiQueryKeys.CSM_ANNOUNCEMENTS] });
    } catch {
      // The cases are real and already sent either way — only the request's
      // own bookkeeping row failed to flip, so this must not imply the send
      // itself needs retrying (it would duplicate every case).
      showError(
        "Every project received the announcement, but marking the request published failed. Try again — it won't resend the cases.",
      );
    } finally {
      setPublishing(false);
    }
  };

  return {
    publishing,
    progress,
    succeededProjectIds,
    failedProjectIds,
    failedTagProjectIds,
    published,
    handlePublish,
  };
}
