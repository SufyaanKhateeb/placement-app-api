BEGIN;

CREATE TABLE form_submissions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    form_id BIGINT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES app_user(id),
    form_version INTEGER NOT NULL,
    answers JSONB NOT NULL CHECK (jsonb_typeof(answers) = 'object'),
    status TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'changes_requested', 'approved')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (form_id, form_version) REFERENCES form_versions(form_id, version),
    UNIQUE (form_id, user_id)
);

COMMIT;
