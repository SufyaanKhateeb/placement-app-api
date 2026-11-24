BEGIN;

CREATE TABLE access_permissions (
    key TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    approval_safe BOOLEAN NOT NULL DEFAULT FALSE,
    reserved BOOLEAN NOT NULL DEFAULT FALSE
);

INSERT INTO access_permissions (key, label, approval_safe, reserved) VALUES
    ('forms.browse', 'Browse published forms', TRUE, FALSE),
    ('submissions.own', 'Manage own submissions', TRUE, FALSE),
    ('forms.browse_restricted', 'Browse published forms with restricted access', FALSE, FALSE),
    ('forms.manage', 'Manage and publish forms', FALSE, FALSE),
    ('submissions.review', 'Review submissions', FALSE, FALSE),
    ('roles.manage', 'Manage roles and view access history', FALSE, TRUE),
    ('admins.invite', 'Invite or create admin accounts', FALSE, TRUE),
    ('admins.promote', 'Promote existing accounts to admin', FALSE, TRUE);

CREATE TABLE access_roles (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key TEXT NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_-]{0,63}$'),
    name TEXT NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    builtin BOOLEAN NOT NULL DEFAULT FALSE,
    approval_eligible BOOLEAN NOT NULL DEFAULT FALSE
);

INSERT INTO access_roles (key, name, builtin) VALUES
    ('member', 'Member', TRUE),
    ('form-manager', 'Form Manager', TRUE),
    ('reviewer', 'Reviewer', TRUE),
    ('role-manager', 'Role Manager', TRUE);

CREATE TABLE access_role_permissions (
    role_id BIGINT REFERENCES access_roles(id) ON DELETE CASCADE,
    permission_key TEXT REFERENCES access_permissions(key),
    PRIMARY KEY (role_id, permission_key)
);

INSERT INTO access_role_permissions
SELECT r.id, p.permission_key
FROM (VALUES
    ('member', 'forms.browse'), ('member', 'submissions.own'),
    ('form-manager', 'forms.browse_restricted'), ('form-manager', 'forms.manage'),
    ('reviewer', 'submissions.review'),
    ('role-manager', 'roles.manage'), ('role-manager', 'admins.invite'), ('role-manager', 'admins.promote')
) p(role_key, permission_key)
JOIN access_roles r ON r.key = p.role_key;

-- A signed session identifies one of two existing account tables.
CREATE TABLE access_accounts (
    account_type TEXT NOT NULL CHECK (account_type IN ('user', 'admin')),
    account_id INTEGER NOT NULL,
    user_id INTEGER GENERATED ALWAYS AS (CASE WHEN account_type = 'user' THEN account_id END) STORED
        REFERENCES app_user(id) ON DELETE CASCADE,
    admin_id INTEGER GENERATED ALWAYS AS (CASE WHEN account_type = 'admin' THEN account_id END) STORED
        REFERENCES admin_user(id) ON DELETE CASCADE,
    PRIMARY KEY (account_type, account_id)
);

CREATE TABLE access_assignments (
    account_type TEXT NOT NULL,
    account_id INTEGER NOT NULL,
    role_id BIGINT REFERENCES access_roles(id) ON DELETE CASCADE,
    PRIMARY KEY (account_type, account_id, role_id),
    FOREIGN KEY (account_type, account_id) REFERENCES access_accounts ON DELETE CASCADE
);

CREATE TABLE access_audit (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    actor_type TEXT,
    actor_id INTEGER,
    action TEXT NOT NULL,
    detail JSONB NOT NULL
);

-- Persist the bootstrap decision even if that individual later leaves.
CREATE TABLE access_bootstrap (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton)
);

CREATE FUNCTION provision_access_account(kind TEXT, account INTEGER) RETURNS VOID
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO access_accounts VALUES (kind, account) ON CONFLICT DO NOTHING;
    WITH added AS (
        INSERT INTO access_assignments
        SELECT kind, account, id FROM access_roles
        WHERE key = 'member' OR (kind = 'admin' AND key IN ('form-manager', 'reviewer'))
        ON CONFLICT DO NOTHING
        RETURNING role_id
    )
    INSERT INTO access_audit (action, detail)
    SELECT 'role.assigned', jsonb_build_object('accountType', kind, 'accountId', account, 'roleId', role_id)
    FROM added;
END;
$$;

CREATE FUNCTION bootstrap_role_manager(account INTEGER) RETURNS VOID
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO access_bootstrap DEFAULT VALUES ON CONFLICT DO NOTHING;
    IF FOUND THEN
        INSERT INTO access_assignments
        SELECT 'admin', account, id FROM access_roles WHERE key = 'role-manager';
        INSERT INTO access_audit (action, detail)
        SELECT 'role.bootstrapped', jsonb_build_object('accountType', 'admin', 'accountId', account, 'roleId', id)
        FROM access_roles WHERE key = 'role-manager';
    END IF;
END;
$$;

CREATE FUNCTION provision_new_access_account() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM provision_access_account(TG_ARGV[0], NEW.id);
    IF TG_ARGV[0] = 'admin' THEN
        PERFORM bootstrap_role_manager(NEW.id);
    END IF;
    RETURN NEW;
END;
$$;

LOCK TABLE app_user, admin_user IN SHARE ROW EXCLUSIVE MODE;

SELECT provision_access_account('user', id) FROM app_user;
SELECT provision_access_account('admin', id) FROM admin_user;
SELECT bootstrap_role_manager(id) FROM (
    SELECT id FROM admin_user ORDER BY createdat, id LIMIT 1
) first_admin;

CREATE TRIGGER app_user_access_insert AFTER INSERT ON app_user
    FOR EACH ROW EXECUTE FUNCTION provision_new_access_account('user');
CREATE TRIGGER admin_user_access_insert AFTER INSERT ON admin_user
    FOR EACH ROW EXECUTE FUNCTION provision_new_access_account('admin');

CREATE TABLE access_legacy_approval_roles (
    legacy_key TEXT PRIMARY KEY,
    role_id BIGINT NOT NULL REFERENCES access_roles(id)
);

ALTER TABLE form_verifications
    ADD COLUMN access_role_id BIGINT REFERENCES access_roles(id),
    ADD COLUMN access_role_revoked_at TIMESTAMPTZ;

CREATE INDEX form_verifications_role_idx ON form_verifications(access_role_id)
    WHERE access_role_id IS NOT NULL;
CREATE INDEX form_submissions_user_access_idx ON form_submissions(user_id);

COMMIT;
