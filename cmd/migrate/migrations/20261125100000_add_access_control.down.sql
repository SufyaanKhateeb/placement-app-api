BEGIN;

ALTER TABLE form_verifications DROP COLUMN access_role_revoked_at, DROP COLUMN access_role_id;
DROP INDEX form_submissions_user_access_idx;
DROP TABLE access_legacy_approval_roles;
DROP TRIGGER admin_user_access_insert ON admin_user;
DROP TRIGGER app_user_access_insert ON app_user;
DROP FUNCTION provision_new_access_account();
DROP FUNCTION bootstrap_role_manager(INTEGER);
DROP FUNCTION provision_access_account(TEXT, INTEGER);
DROP TABLE access_bootstrap, access_audit, access_assignments, access_accounts,
    access_role_permissions, access_roles, access_permissions;

COMMIT;
