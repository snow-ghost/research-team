CREATE TABLE lemma_signatures (
    id text PRIMARY KEY,
    position integer NOT NULL,
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object')
);
