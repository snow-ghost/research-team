CREATE TABLE research_skill_revisions (
    id text PRIMARY KEY,
    position integer NOT NULL,
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object')
);
