ALTER TABLE proof_verifications ALTER COLUMN attempt_id DROP NOT NULL;
ALTER TABLE proof_verifications ADD CONSTRAINT proof_source_origin CHECK (
    (attempt_id IS NOT NULL AND attempt_id <> '' AND COALESCE(data->>'origin', '') = '')
    OR (attempt_id IS NULL AND COALESCE(data->>'origin', '') = 'submitted'
        AND COALESCE(length(data->>'author'), 0) > 0
        AND COALESCE(length(data->>'basis'), 0) > 0)
);
ALTER TABLE entities ADD COLUMN proof_verification_id text
    GENERATED ALWAYS AS (data->>'proofVerification') STORED
    REFERENCES proof_verifications(id) DEFERRABLE INITIALLY DEFERRED;
