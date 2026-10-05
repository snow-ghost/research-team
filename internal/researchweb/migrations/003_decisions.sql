CREATE TABLE review_decisions (
    state_revision bigint PRIMARY KEY REFERENCES events(revision) DEFERRABLE INITIALLY DEFERRED,
    entity_id text NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    statement_revision integer NOT NULL CHECK(statement_revision > 0),
    attempt_id text REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED,
    decision text NOT NULL CHECK(decision IN ('accept','reject')),
    actor text NOT NULL,
    reason text NOT NULL
);
CREATE INDEX review_decisions_entity ON review_decisions(entity_id,state_revision);
