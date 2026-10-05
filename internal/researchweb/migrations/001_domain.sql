CREATE TABLE workspace (
    id integer PRIMARY KEY CHECK (id = 1),
    revision bigint NOT NULL CHECK (revision > 0),
    metadata jsonb NOT NULL
);
INSERT INTO workspace VALUES (1, 1, '{"schema":1,"revision":1,"paused":false,"scenarioStep":0}');
CREATE TABLE events (
    revision bigint PRIMARY KEY,
    metadata jsonb NOT NULL,
    state_metadata jsonb NOT NULL
);
CREATE TABLE commands (id text PRIMARY KEY, fingerprint text NOT NULL);
CREATE TABLE imports (id integer PRIMARY KEY CHECK(id=1), source_digest text NOT NULL, source_revision bigint NOT NULL, imported_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE studies (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    goal_id text GENERATED ALWAYS AS (data->>'goal') STORED NOT NULL,
    title text GENERATED ALWAYS AS (data->>'title') STORED NOT NULL
);
CREATE TABLE entities (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    study_id text GENERATED ALWAYS AS (data->>'study') STORED NOT NULL REFERENCES studies(id) DEFERRABLE INITIALLY DEFERRED,
    revision integer GENERATED ALWAYS AS ((data->>'revision')::integer) STORED CHECK (revision > 0),
    kind text GENERATED ALWAYS AS (data->>'kind') STORED NOT NULL,
    status text GENERATED ALWAYS AS (data->>'status') STORED NOT NULL
);
ALTER TABLE studies ADD CONSTRAINT studies_goal_fk FOREIGN KEY(goal_id) REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX entities_study_status ON entities(study_id, status);
CREATE TABLE dependencies (
    source_id text REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    target_id text REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    required_revision integer CHECK (required_revision > 0),
    position integer NOT NULL,
    PRIMARY KEY(source_id, target_id), CHECK(source_id <> target_id)
);
CREATE INDEX dependencies_target ON dependencies(target_id);
CREATE TABLE work_links (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL,
    source_id text GENERATED ALWAYS AS (data->>0) STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    target_id text GENERATED ALWAYS AS (data->>1) STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE tasks (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    kind text GENERATED ALWAYS AS (data->>'kind') STORED NOT NULL,
    status text GENERATED ALWAYS AS (data->>'state') STORED NOT NULL,
    attempt_id text GENERATED ALWAYS AS (NULLIF(data->>'attempt','')) STORED
);
CREATE INDEX tasks_target_status ON tasks(target_id, status);
CREATE TABLE attempts (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    task_id text GENERATED ALWAYS AS (data->>'task_id') STORED NOT NULL UNIQUE REFERENCES tasks(id) DEFERRABLE INITIALLY DEFERRED,
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    status text GENERATED ALWAYS AS (data->>'status') STORED NOT NULL,
    input_revision integer GENERATED ALWAYS AS ((data->>'input_snapshot')::integer) STORED NOT NULL
);
ALTER TABLE tasks ADD CONSTRAINT tasks_attempt_fk FOREIGN KEY(attempt_id) REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX attempts_status ON attempts(status);
CREATE TABLE questions (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE findings (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    status text GENERATED ALWAYS AS (data->>'state') STORED NOT NULL,
    severity text GENERATED ALWAYS AS (data->>'severity') STORED NOT NULL
);
CREATE INDEX findings_target_status ON findings(target_id,status);
CREATE TABLE applications (
    id text PRIMARY KEY REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    lemma_id text GENERATED ALWAYS AS (data->>'lemma') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    lemma_revision integer GENERATED ALWAYS AS ((data->>'lemmaRevision')::integer) STORED CHECK(lemma_revision > 0),
    UNIQUE(lemma_id,target_id)
);
CREATE TABLE delegations (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK (data->>'id' = id),
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE object_versions (
    collection text NOT NULL,
    object_id text NOT NULL,
    revision bigint REFERENCES events(revision) DEFERRABLE INITIALLY DEFERRED,
    position integer NOT NULL,
    data jsonb,
    PRIMARY KEY(collection, object_id, revision)
);
CREATE INDEX object_versions_revision ON object_versions(revision);
CREATE VIEW entity_versions AS SELECT object_id AS entity_id, revision AS state_revision,
    (data->>'revision')::integer AS statement_revision, data
    FROM object_versions WHERE collection='entities' AND data IS NOT NULL;
