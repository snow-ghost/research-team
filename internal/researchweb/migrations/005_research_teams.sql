CREATE TABLE research_teams (
    id text PRIMARY KEY, position integer NOT NULL,
    data jsonb NOT NULL CHECK(data->>'id'=id),
    study_id text GENERATED ALWAYS AS (data->>'study') STORED NOT NULL
        REFERENCES studies(id) DEFERRABLE INITIALLY DEFERRED,
    goal_id text GENERATED ALWAYS AS (data->>'goal') STORED NOT NULL
        REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    status text GENERATED ALWAYS AS (data->>'status') STORED NOT NULL
);
CREATE UNIQUE INDEX one_active_team_per_study ON research_teams(study_id)
    WHERE status IN ('running','awaiting_review','paused','blocked');
ALTER TABLE attempts ADD COLUMN team_id text GENERATED ALWAYS AS (NULLIF(data->>'team_id','')) STORED
    REFERENCES research_teams(id) DEFERRABLE INITIALLY DEFERRED;
