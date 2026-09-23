-- Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
--
-- WSO2 LLC. licenses this file to you under the Apache License,
-- Version 2.0 (the "License"); you may not use this file except
-- in compliance with the License.
-- You may obtain a copy of the License at
--
-- http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing,
-- software distributed under the License is distributed on an
-- "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
-- KIND, either express or implied.  See the License for the
-- specific language governing permissions and limitations
-- under the License.

-- The CRE and SRE teams that own an account, each a single reference into
-- team (000028). Both nullable: not every account has either assigned, and
-- the values are set by CS leadership rather than synced from a source
-- system. Follows the existing single-entity-ref columns on account
-- (customer_success_manager_id, technical_owner_id) in both FK style and
-- index naming.
ALTER TABLE account
    ADD COLUMN IF NOT EXISTS cre_team_id UUID REFERENCES team(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS sre_team_id UUID REFERENCES team(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_account_cre_team ON account (cre_team_id);
CREATE INDEX IF NOT EXISTS idx_account_sre_team ON account (sre_team_id);
