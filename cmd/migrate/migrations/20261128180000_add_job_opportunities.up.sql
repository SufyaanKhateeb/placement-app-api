BEGIN;

INSERT INTO access_permissions (key, label) VALUES ('jobs.publish', 'Create and publish job opportunities');

CREATE TABLE job_opportunities (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    company TEXT NOT NULL CHECK (char_length(company) <= 160),
    title TEXT NOT NULL CHECK (char_length(title) <= 160),
    description TEXT NOT NULL CHECK (char_length(description) <= 10000),
    requirements TEXT NOT NULL CHECK (char_length(requirements) <= 5000),
    deadline TIMESTAMPTZ,
    contact TEXT NOT NULL CHECK (char_length(contact) <= 320),
    application_instructions TEXT NOT NULL CHECK (char_length(application_instructions) <= 5000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    published_at TIMESTAMPTZ,
    publisher_type TEXT CHECK (publisher_type IN ('admin', 'user')),
    publisher_id INTEGER CHECK (publisher_id > 0),
    verified_audience BOOLEAN NOT NULL DEFAULT FALSE,
    audience_role_ids BIGINT[] NOT NULL DEFAULT '{}',
    recipient_count BIGINT NOT NULL DEFAULT 0 CHECK (recipient_count >= 0),
    CHECK (
        (published_at IS NULL AND publisher_type IS NULL AND publisher_id IS NULL
            AND NOT verified_audience AND cardinality(audience_role_ids) = 0 AND recipient_count = 0)
        OR
        (published_at IS NOT NULL AND publisher_type IS NOT NULL AND publisher_id IS NOT NULL
            AND (verified_audience OR cardinality(audience_role_ids) > 0)
            AND btrim(company) <> '' AND btrim(title) <> '' AND btrim(description) <> '')
    )
);

CREATE TABLE job_deliveries (
    user_id INTEGER NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    job_id BIGINT NOT NULL REFERENCES job_opportunities(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, job_id)
);

CREATE INDEX job_deliveries_job_idx ON job_deliveries(job_id);

COMMIT;
