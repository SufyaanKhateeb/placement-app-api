package access

import (
	"context"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	BrowseForms       = "forms.browse"
	BrowseRestricted  = "forms.browse_restricted"
	OwnSubmissions    = "submissions.own"
	ManageForms       = "forms.manage"
	ReviewSubmissions = "submissions.review"
	ManageRoles       = "roles.manage"
	InviteAdmins      = "admins.invite"
	PromoteAdmins     = "admins.promote"
)

type Store struct {
	DB *pgxpool.Pool
}

// A cast to an approval role never bypasses the catalog's approval-safe policy.
const safeApprovalRole = `r.approval_eligible AND NOT r.builtin AND NOT EXISTS (
	SELECT 1 FROM access_role_permissions rp
	JOIN access_permissions p ON p.key = rp.permission_key
	WHERE rp.role_id = r.id AND (NOT p.approval_safe OR p.reserved)
)`

func (s Store) Resolve(ctx context.Context, account types.TokenInput) ([]string, error) {
	return resolvePermissions(ctx, s.DB, account)
}

func resolvePermissions(ctx context.Context, db rowReader, account types.TokenInput) ([]string, error) {
	var permissions []string
	err := db.QueryRow(ctx, `
		SELECT ARRAY(
			SELECT DISTINCT rp.permission_key
			FROM (
				SELECT role_id FROM access_assignments
				WHERE account_type = a.account_type AND account_id = a.account_id
				UNION ALL
				SELECT r.id
				FROM form_submissions sub
				JOIN form_verifications v ON v.submission_id = sub.id
				LEFT JOIN access_legacy_approval_roles m ON m.legacy_key = v.role_key AND v.access_role_id IS NULL
				JOIN access_roles r ON r.id = COALESCE(v.access_role_id, m.role_id)
				WHERE a.account_type = 'user' AND sub.user_id = a.account_id
				  AND v.access_role_revoked_at IS NULL AND `+safeApprovalRole+`
			) granted
			JOIN access_role_permissions rp ON rp.role_id = granted.role_id
			ORDER BY rp.permission_key
		) FROM access_accounts a WHERE a.account_type = $1 AND a.account_id = $2
	`, account.Type, account.Id).Scan(&permissions)
	return permissions, err
}

type rowReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ApprovalRole(ctx context.Context, db rowReader, id int64, legacyKey string) (RoleChoice, error) {
	var role RoleChoice
	err := db.QueryRow(ctx, `
		SELECT r.id FROM access_roles r
		WHERE r.id = CASE WHEN $1::bigint > 0 THEN $1 ELSE (
			SELECT role_id FROM access_legacy_approval_roles WHERE legacy_key = $2
		) END
		FOR SHARE OF r
	`, id, legacyKey).Scan(&role.ID)
	if err != nil {
		return role, err
	}
	// With a transaction, read the policy after acquiring the same role lock used by editors.
	err = db.QueryRow(ctx, `SELECT r.key, r.name FROM access_roles r WHERE r.id = $1 AND `+safeApprovalRole, role.ID).Scan(&role.Key, &role.Name)
	return role, err
}

func (s Store) SubmissionRole(ctx context.Context, submissionID int64) (RoleChoice, error) {
	var role RoleChoice
	err := s.DB.QueryRow(ctx, `
		SELECT r.id, r.key, r.name
		FROM form_verifications v
		LEFT JOIN access_legacy_approval_roles m ON m.legacy_key = v.role_key AND v.access_role_id IS NULL
		JOIN access_roles r ON r.id = COALESCE(v.access_role_id, m.role_id)
		WHERE v.submission_id = $1 AND v.access_role_revoked_at IS NULL AND `+safeApprovalRole,
		submissionID).Scan(&role.ID, &role.Key, &role.Name)
	return role, err
}

type RoleChoice struct {
	ID   int64  `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (s Store) ApprovalRoles(ctx context.Context) ([]RoleChoice, error) {
	rows, err := s.DB.Query(ctx, `SELECT r.id, r.key, r.name FROM access_roles r WHERE `+safeApprovalRole+` ORDER BY lower(r.name), r.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RoleChoice])
}
