-- The only issue-body-derived columns persisted: title, ABT team, and
-- opened-by, the last restricted to a @wso2.com address by the CHECK below.
-- Nothing else from the issue body is ever persisted.
ALTER TABLE issues
  ADD COLUMN title     text,
  ADD COLUMN abt_team  text,
  ADD COLUMN opened_by text,
  ADD CONSTRAINT issues_opened_by_wso2_chk
    CHECK (opened_by IS NULL OR opened_by ~ '^[a-z0-9._%+-]+@wso2\.com$');

CREATE INDEX issues_abt_team_idx ON issues (abt_team);
