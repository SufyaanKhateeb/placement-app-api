BEGIN;

CREATE TABLE admin_memberships (
    user_id INTEGER PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE admin_membership_roles (
    user_id INTEGER REFERENCES admin_memberships(user_id) ON DELETE CASCADE,
    role_id BIGINT REFERENCES access_roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE admin_invitations (
    email TEXT PRIMARY KEY CHECK (email = lower(email)),
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    inviter_type TEXT NOT NULL CHECK (inviter_type IN ('admin', 'user')),
    inviter_id INTEGER NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    user_id INTEGER REFERENCES app_user(id) ON DELETE SET NULL
);

COMMIT;
