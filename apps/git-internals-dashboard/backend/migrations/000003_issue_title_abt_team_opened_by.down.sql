DROP INDEX IF EXISTS issues_abt_team_idx;
ALTER TABLE issues
  DROP CONSTRAINT IF EXISTS issues_opened_by_wso2_chk,
  DROP COLUMN IF EXISTS opened_by,
  DROP COLUMN IF EXISTS abt_team,
  DROP COLUMN IF EXISTS title;
