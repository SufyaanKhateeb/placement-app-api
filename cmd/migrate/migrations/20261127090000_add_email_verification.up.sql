BEGIN;

CREATE TABLE verified_user_emails (
    user_id INTEGER PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    email TEXT NOT NULL CHECK (email = lower(email))
);

CREATE TABLE email_verifications (
    email TEXT PRIMARY KEY CHECK (email = lower(email)),
    sent_at TIMESTAMPTZ[] NOT NULL DEFAULT '{}',
    failed_at TIMESTAMPTZ[] NOT NULL DEFAULT '{}'
);

CREATE TABLE email_verification_challenges (
    email TEXT PRIMARY KEY REFERENCES email_verifications(email) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    code_digest BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    deadline TIMESTAMPTZ NOT NULL,
    recipient TEXT NOT NULL,
    user_id INTEGER REFERENCES app_user(id) ON DELETE CASCADE,
    firstname TEXT NOT NULL,
    lastname TEXT NOT NULL,
    password_hash TEXT NOT NULL
);

CREATE INDEX email_verification_challenges_deadline_idx ON email_verification_challenges(deadline);

COMMIT;
