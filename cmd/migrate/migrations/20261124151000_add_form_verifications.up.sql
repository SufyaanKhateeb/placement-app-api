BEGIN;

CREATE TABLE form_verifications (
    submission_id BIGINT PRIMARY KEY REFERENCES form_submissions(id),
    verified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    follow_up_form_id BIGINT REFERENCES forms(id),
    role_key TEXT,
    CHECK (num_nonnulls(follow_up_form_id, role_key) <= 1),
    CHECK (role_key IS NULL OR role_key ~ '^[a-z][a-z0-9_-]{0,63}$')
);

CREATE INDEX form_verifications_follow_up_idx
    ON form_verifications(follow_up_form_id) WHERE follow_up_form_id IS NOT NULL;

INSERT INTO form_verifications (submission_id)
SELECT id FROM form_submissions WHERE status = 'approved'
ON CONFLICT (submission_id) DO NOTHING;

COMMIT;
