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

-- The product-consumption provisioning flow's final step generates two
-- subscription secret keys and stores them on the project alongside the
-- Choreo application's OAuth2 credentials. The project table mirrors the
-- ServiceNow record, which carries both, but had no columns for them — so
-- the dual-write silently discarded step 5's artefacts and every read
-- reported the project as having no secret keys.
--
-- These columns already exist on the sync-built databases, as VARCHAR(128),
-- so IF NOT EXISTS makes this a no-op there and the definition below only
-- takes effect on a database built from migrations/. TEXT rather than a
-- length-capped VARCHAR because nothing here depends on the width and a key
-- longer than expected should not fail the write.
--
-- Stored as supplied, matching the sync, which writes them in the clear --
-- the observed values are 64-character keys, not ciphertext. This service
-- deliberately does not encrypt its own writes: doing so alone would put two
-- indistinguishable formats in one column and make a row it writes stop
-- matching the ServiceNow record the project row mirrors. Encrypting these at
-- rest is worth doing across every writer, which is a platform change.
ALTER TABLE project
    ADD COLUMN IF NOT EXISTS primary_secret_key TEXT,
    ADD COLUMN IF NOT EXISTS secondary_secret_key TEXT;

COMMENT ON COLUMN project.primary_secret_key IS
    'Primary subscription secret key, mirrored from the ServiceNow record. Stored as supplied; never returned to a caller -- the read response reports only whether it is present.';
COMMENT ON COLUMN project.secondary_secret_key IS
    'Secondary subscription secret key, mirrored from the ServiceNow record. Stored as supplied; never returned to a caller -- the read response reports only whether it is present.';
