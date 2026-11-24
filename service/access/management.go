package access

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

var (
	errInvalidRole    = errors.New("choose a valid role name, key, and permissions")
	errReservedRole   = errors.New("built-in roles cannot be edited")
	errUnsafeApproval = errors.New("an approval role can contain only member permissions")
	errAdminRequired  = errors.New("Role Manager requires an admin account")
	errLastManager    = errors.New("assign another Role Manager before revoking this assignment")
	errMappingExists  = errors.New("this legacy approval key is already mapped")
	errDenied         = errors.New("insufficient permissions")
	validKey          = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type role struct {
	RoleChoice
	Builtin          bool     `json:"builtin"`
	ApprovalEligible bool     `json:"approvalEligible"`
	Permissions      []string `json:"permissions"`
}

type roleInput struct {
	Key              string   `json:"key"`
	Name             string   `json:"name"`
	ApprovalEligible bool     `json:"approvalEligible"`
	Permissions      []string `json:"permissions"`
}

type permission struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	ApprovalSafe bool   `json:"approvalSafe"`
	Reserved     bool   `json:"reserved"`
}

type account struct {
	Type  string `json:"type"`
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type accountRole struct {
	RoleID       int64  `json:"roleId"`
	Name         string `json:"name"`
	SubmissionID *int64 `json:"submissionId,omitempty"`
	Active       bool   `json:"active"`
	Revoked      bool   `json:"revoked"`
}

type auditEntry struct {
	ID         int64           `json:"id"`
	OccurredAt time.Time       `json:"occurredAt"`
	ActorType  *string         `json:"actorType"`
	ActorID    *int            `json:"actorId"`
	ActorName  *string         `json:"actorName"`
	Action     string          `json:"action"`
	Detail     json.RawMessage `json:"detail"`
}

type legacyKey struct {
	Key    string `json:"key"`
	RoleID *int64 `json:"roleId"`
}

type catalog struct {
	Roles       []role       `json:"roles"`
	Permissions []permission `json:"permissions"`
	LegacyKeys  []legacyKey  `json:"legacyKeys"`
}

func (s Store) catalog(ctx context.Context) (catalog, error) {
	roles, err := s.DB.Query(ctx, `
		SELECT id, key, name, builtin, approval_eligible,
			ARRAY(SELECT permission_key FROM access_role_permissions WHERE role_id = r.id ORDER BY permission_key)
		FROM access_roles r ORDER BY builtin DESC, lower(name), id
	`)
	if err != nil {
		return catalog{}, err
	}
	var result catalog
	result.Roles, err = pgx.CollectRows(roles, func(row pgx.CollectableRow) (role, error) {
		var r role
		err := row.Scan(&r.ID, &r.Key, &r.Name, &r.Builtin, &r.ApprovalEligible, &r.Permissions)
		return r, err
	})
	if err != nil {
		return result, err
	}
	permissions, err := s.DB.Query(ctx, "SELECT key, label, approval_safe, reserved FROM access_permissions ORDER BY label")
	if err != nil {
		return result, err
	}
	result.Permissions, err = pgx.CollectRows(permissions, pgx.RowToStructByPos[permission])
	if err != nil {
		return result, err
	}
	keys, err := s.DB.Query(ctx, `
		SELECT k.key, m.role_id
		FROM (
			SELECT role_key AS key FROM form_verifications WHERE access_role_id IS NULL AND role_key IS NOT NULL
			UNION
			SELECT definition #>> '{approvalAction,role}' AS key FROM form_versions
			WHERE definition #>> '{approvalAction,kind}' = 'assign_role'
			  AND definition #> '{approvalAction,roleId}' IS NULL
		) k LEFT JOIN access_legacy_approval_roles m ON m.legacy_key = k.key
		WHERE k.key IS NOT NULL AND k.key <> '' ORDER BY k.key
	`)
	if err != nil {
		return result, err
	}
	result.LegacyKeys, err = pgx.CollectRows(keys, pgx.RowToStructByPos[legacyKey])
	return result, err
}

func (s Store) accounts(ctx context.Context, search string) ([]account, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT kind, id, name, email
		FROM (
			SELECT 'user' AS kind, id, concat_ws(' ', firstname, lastname) AS name, email FROM app_user
			UNION ALL
			SELECT 'admin', id, concat_ws(' ', firstname, lastname), email FROM admin_user
		) a
		WHERE strpos(lower(email), lower($1)) > 0 OR strpos(lower(name), lower($1)) > 0
		ORDER BY lower(name), kind, id LIMIT 40
	`, search)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[account])
}

func (s Store) accountRoles(ctx context.Context, kind string, id int) ([]accountRole, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, r.name, NULL::bigint, TRUE, FALSE
		FROM access_assignments a JOIN access_roles r ON r.id = a.role_id
		WHERE a.account_type = $1 AND a.account_id = $2
		UNION ALL
		SELECT r.id, r.name, sub.id, v.access_role_revoked_at IS NULL AND `+safeApprovalRole+`, v.access_role_revoked_at IS NOT NULL
		FROM form_submissions sub JOIN form_verifications v ON v.submission_id = sub.id
		LEFT JOIN access_legacy_approval_roles m ON m.legacy_key = v.role_key AND v.access_role_id IS NULL
		JOIN access_roles r ON r.id = COALESCE(v.access_role_id, m.role_id)
		WHERE $1 = 'user' AND sub.user_id = $2
		ORDER BY 2, 3 NULLS FIRST
	`, kind, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[accountRole])
}

func (s Store) auditEntries(ctx context.Context, before *int64) ([]auditEntry, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT event.id, event.occurred_at, event.actor_type, event.actor_id,
			COALESCE(admin.firstname || ' ' || admin.lastname, member.firstname || ' ' || member.lastname),
			event.action, event.detail
		FROM access_audit event
		LEFT JOIN admin_user admin ON event.actor_type = 'admin' AND admin.id = event.actor_id
		LEFT JOIN app_user member ON event.actor_type = 'user' AND member.id = event.actor_id
		WHERE $1::bigint IS NULL OR event.id < $1 ORDER BY event.id DESC LIMIT 50
	`, before)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[auditEntry])
}

func Audit(ctx context.Context, tx pgx.Tx, actor types.TokenInput, action string, detail any) error {
	_, err := tx.Exec(ctx, "INSERT INTO access_audit (actor_type, actor_id, action, detail) VALUES ($1, $2, $3, $4)",
		actor.Type, actor.Id, action, detail)
	return err
}

func (s Store) change(ctx context.Context, actor types.TokenInput, fn func(pgx.Tx) error) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize role and grant administration, including the last-manager check.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(21, 1)"); err != nil {
		return err
	}
	permissions, err := resolvePermissions(ctx, tx, actor)
	if err != nil {
		return err
	}
	if actor.Type != types.RoleAdmin || !slices.Contains(permissions, ManageRoles) {
		return errDenied
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func changeRole(ctx context.Context, tx pgx.Tx, actor types.TokenInput, id int64, input roleInput) (int64, error) {
	input.Name = strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(input.Name) == 0 || utf8.RuneCountInString(input.Name) > 80 {
		return 0, errInvalidRole
	}
	var previous roleInput
	if id == 0 && !validKey.MatchString(input.Key) {
		return 0, errInvalidRole
	}
	if id != 0 {
		var builtin bool
		err := tx.QueryRow(ctx, `
			SELECT key, name, approval_eligible, builtin,
				ARRAY(SELECT permission_key FROM access_role_permissions WHERE role_id = r.id ORDER BY permission_key)
			FROM access_roles r WHERE id = $1 FOR UPDATE OF r
		`, id).Scan(&previous.Key, &previous.Name, &previous.ApprovalEligible, &builtin, &previous.Permissions)
		if err != nil {
			return 0, err
		}
		if builtin {
			return 0, errReservedRole
		}
		input.Key = previous.Key
	}
	if len(input.Permissions) == 0 {
		return 0, errInvalidRole
	}
	slices.Sort(input.Permissions)
	input.Permissions = slices.Compact(input.Permissions)
	var found int
	var safe bool
	err := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(bool_and(approval_safe), FALSE)
		FROM access_permissions WHERE key = ANY($1) AND NOT reserved
	`, input.Permissions).Scan(&found, &safe)
	if err != nil {
		return 0, err
	}
	if found != len(input.Permissions) {
		return 0, errInvalidRole
	}
	if input.ApprovalEligible && !safe {
		return 0, errUnsafeApproval
	}

	if id == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO access_roles (key, name, approval_eligible) VALUES ($1, $2, $3)
			ON CONFLICT (key) DO NOTHING RETURNING id
		`, input.Key, input.Name, input.ApprovalEligible).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errInvalidRole
		}
	} else {
		_, err = tx.Exec(ctx, "UPDATE access_roles SET name = $2, approval_eligible = $3 WHERE id = $1",
			id, input.Name, input.ApprovalEligible)
		if err == nil {
			_, err = tx.Exec(ctx, "DELETE FROM access_role_permissions WHERE role_id = $1", id)
		}
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO access_role_permissions SELECT $1, unnest($2::text[])", id, input.Permissions); err != nil {
		return 0, err
	}
	detail := map[string]any{"roleId": id, "after": input}
	action := "role.created"
	if previous.Key != "" {
		detail["before"] = previous
		action = "role.edited"
	}
	return id, Audit(ctx, tx, actor, action, detail)
}

func changeAssignment(ctx context.Context, tx pgx.Tx, actor types.TokenInput, kind string, id int, roleID int64, assign bool) error {
	var key string
	if err := tx.QueryRow(ctx, `
		SELECT r.key FROM access_roles r CROSS JOIN access_accounts a
		WHERE r.id = $1 AND a.account_type = $2 AND a.account_id = $3
	`, roleID, kind, id).Scan(&key); err != nil {
		return err
	}
	if key == "role-manager" && kind != types.RoleAdmin {
		return errAdminRequired
	}
	action := "role.assigned"
	if assign {
		result, err := tx.Exec(ctx, "INSERT INTO access_assignments VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", kind, id, roleID)
		if err != nil || result.RowsAffected() == 0 {
			return err
		}
	} else {
		result, err := tx.Exec(ctx, "DELETE FROM access_assignments WHERE account_type = $1 AND account_id = $2 AND role_id = $3", kind, id, roleID)
		if err != nil || result.RowsAffected() == 0 {
			return err
		}
		if key == "role-manager" {
			var remaining bool
			err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM access_assignments WHERE role_id = $1 AND account_type = 'admin')", roleID).Scan(&remaining)
			if err != nil {
				return err
			}
			if !remaining {
				return errLastManager
			}
		}
		action = "role.revoked"
	}
	return Audit(ctx, tx, actor, action, map[string]any{"accountType": kind, "accountId": id, "roleId": roleID, "roleKey": key})
}

func revokeApproval(ctx context.Context, tx pgx.Tx, actor types.TokenInput, userID int, submissionID int64) error {
	var roleID int64
	err := tx.QueryRow(ctx, `
		UPDATE form_verifications v SET access_role_revoked_at = clock_timestamp()
		FROM form_submissions sub
		WHERE v.submission_id = sub.id AND sub.id = $1 AND sub.user_id = $2 AND v.access_role_revoked_at IS NULL
		  AND COALESCE(v.access_role_id, (SELECT role_id FROM access_legacy_approval_roles WHERE legacy_key = v.role_key)) IS NOT NULL
		RETURNING COALESCE(v.access_role_id, (SELECT role_id FROM access_legacy_approval_roles WHERE legacy_key = v.role_key))
	`, submissionID, userID).Scan(&roleID)
	if err != nil {
		return err
	}
	return Audit(ctx, tx, actor, "approval.revoked", map[string]any{"accountType": "user", "accountId": userID, "submissionId": submissionID, "roleId": roleID})
}

func mapLegacyRole(ctx context.Context, tx pgx.Tx, actor types.TokenInput, key string, id int64) error {
	if _, err := ApprovalRole(ctx, tx, id, ""); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO access_legacy_approval_roles (legacy_key, role_id)
		SELECT $1, $2 WHERE EXISTS (
			SELECT 1 FROM form_verifications WHERE access_role_id IS NULL AND role_key = $1
			UNION ALL
			SELECT 1 FROM form_versions WHERE definition #>> '{approvalAction,kind}' = 'assign_role'
				AND definition #> '{approvalAction,roleId}' IS NULL AND definition #>> '{approvalAction,role}' = $1
		) ON CONFLICT DO NOTHING
	`, key, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var mapped bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM access_legacy_approval_roles WHERE legacy_key = $1)", key).Scan(&mapped); err != nil {
			return err
		}
		if mapped {
			return errMappingExists
		}
		return pgx.ErrNoRows
	}
	return Audit(ctx, tx, actor, "approval.legacy_mapped", map[string]any{"legacyKey": key, "roleId": id})
}
