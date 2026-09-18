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

import { Box, Card, LinearProgress, Typography } from "@wso2/oxygen-ui";
import { CheckCircle, Megaphone, XCircle } from "@wso2/oxygen-ui-icons-react";
import type { JSX } from "react";

export interface AnnouncementSendProgressState {
  total: number;
  completed: number;
  succeeded: number;
  failed: number;
}

interface AnnouncementSendProgressProps {
  progress: AnnouncementSendProgressState;
}

/**
 * Live progress for a batch announcement send — one real `POST /cases` per
 * target project, fanned out through settleWithConcurrencyLimit. Both create
 * forms render this the moment handleSubmit starts (see each form's own
 * `sendProgress` state, updated via settleWithConcurrencyLimit's `onSettle`
 * callback as each project's create call actually lands), so a sender
 * targeting a large audience sees "N of total" tick up in real time instead
 * of a single opaque "Creating…" button label for however long the whole
 * batch takes.
 *
 * Stays mounted after the batch finishes, showing the final tally ("N/N",
 * "Announcement sent") — for a full success both forms navigate away
 * immediately after, so this is only visible for a moment, but on a partial
 * or total failure the form stays on screen (no navigate) and this card's
 * final counts sit alongside the existing error banner, which separately
 * reports the outcome by project id for retrying. A fresh submit resets the
 * counts back to 0 before the next batch starts.
 */
export default function AnnouncementSendProgress({
  progress,
}: AnnouncementSendProgressProps): JSX.Element {
  const { total, completed, succeeded, failed } = progress;
  const percent = total > 0 ? Math.round((completed / total) * 100) : 0;

  return (
    <Card
      variant="outlined"
      sx={{
        mt: 2.5,
        p: 2.5,
        bgcolor: "action.hover",
        display: "flex",
        flexDirection: "column",
        gap: 1.25,
      }}
      role="status"
      aria-live="polite"
    >
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Megaphone size={18} />
          <Typography variant="subtitle1" fontWeight={700}>
            {completed >= total ? "Announcement sent" : "Sending announcement…"}
          </Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" fontWeight={600}>
          {completed}/{total}
        </Typography>
      </Box>

      <LinearProgress
        variant="determinate"
        value={percent}
        color={failed > 0 ? "warning" : "primary"}
        sx={{ height: 8, borderRadius: 1 }}
      />

      <Box sx={{ display: "flex", gap: 2.5 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, color: "success.main" }}>
          <CheckCircle size={14} aria-hidden />
          <Typography variant="caption" color="success.main">
            {succeeded} succeeded
          </Typography>
        </Box>
        {failed > 0 && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, color: "error.main" }}>
            <XCircle size={14} aria-hidden />
            <Typography variant="caption" color="error.main">
              {failed} failed
            </Typography>
          </Box>
        )}
      </Box>
    </Card>
  );
}
