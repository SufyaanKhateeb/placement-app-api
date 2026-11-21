BEGIN;

CREATE TABLE forms (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    draft JSONB,
    draft_revision BIGINT NOT NULL DEFAULT 1 CHECK (draft_revision > 0),
    published_version INTEGER,
    CHECK (draft IS NOT NULL OR published_version IS NOT NULL)
);

CREATE TABLE form_versions (
    form_id BIGINT NOT NULL REFERENCES forms(id),
    version INTEGER NOT NULL CHECK (version > 0),
    definition JSONB NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (form_id, version)
);

ALTER TABLE forms
    ADD CONSTRAINT forms_published_version_fkey
    FOREIGN KEY (id, published_version) REFERENCES form_versions(form_id, version);

COMMIT;