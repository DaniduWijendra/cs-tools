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

import { Card, Typography, useTheme } from "@wso2/oxygen-ui";
import { Info } from "@wso2/oxygen-ui-icons-react";
import type { JSX } from "react";

/**
 * Stand-in for the "Product version / EOL" announcement flow. Deliberately
 * not a form yet: resolving "which customers are running an end-of-life
 * product version" needs a reverse query entity-service doesn't have —
 * SearchDeployedProducts only filters by deploymentIds today, nothing by
 * product or version — so there's no real audience to target until that's
 * built. This placeholder exists so the two-option choice itself
 * (AnnouncementKindSelector) is real and visible now, without pretending
 * this flow works before it does.
 */
export default function CreateEolAnnouncementPlaceholder(): JSX.Element {
  const theme = useTheme();
  return (
    <Card
      variant="outlined"
      sx={{
        p: 4,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        textAlign: "center",
        gap: 1.5,
      }}
    >
      <Info size={28} color={theme.palette.text.secondary} aria-hidden />
      <Typography variant="h6">Not available yet</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ maxWidth: 480 }}>
        Product version / EOL announcements need a new capability in the entity service first —
        resolving which customers are running a given product version — which hasn&apos;t been
        built yet. This option will be enabled once that&apos;s ready.
      </Typography>
    </Card>
  );
}
