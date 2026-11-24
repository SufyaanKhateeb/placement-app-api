ALTER TABLE form_submissions
    ADD COLUMN requested_changes JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT form_submissions_requested_changes_check CHECK (
        jsonb_typeof(requested_changes) = 'object'
    );
