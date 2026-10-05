CREATE TABLE research_branches (
 id text PRIMARY KEY,
 position integer NOT NULL UNIQUE,
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object'),
 study_id text GENERATED ALWAYS AS (data->>'study') STORED NOT NULL REFERENCES studies(id) DEFERRABLE INITIALLY DEFERRED,
 target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
 team_id text GENERATED ALWAYS AS (NULLIF(data->>'team','')) STORED REFERENCES research_teams(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX research_branches_scope ON research_branches(study_id,(data->>'status'));
