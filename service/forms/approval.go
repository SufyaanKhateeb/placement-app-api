package forms

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/jackc/pgx/v5"
)

type ApprovalAction struct {
	Kind   string `json:"kind"`
	FormID int64  `json:"formId,omitempty"`
	Role   string `json:"role,omitempty"`
	RoleID int64  `json:"roleId,omitempty"`
}

var roleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var errInvalidFollowUp = errors.New("select another published form that requires an approval grant")
var errInvalidApprovalRole = errors.New("select a role eligible for form approval")

func (a *ApprovalAction) validate() error {
	if a == nil {
		return nil
	}
	a.Role = strings.ToLower(strings.TrimSpace(a.Role))
	switch a.Kind {
	case "follow_up_form":
		if a.FormID > 0 && a.Role == "" && a.RoleID == 0 {
			return nil
		}
		return errInvalidFollowUp
	case "assign_role":
		if a.RoleID > 0 && a.FormID == 0 {
			return nil
		}
		if a.FormID != 0 || !roleKeyPattern.MatchString(a.Role) {
			return errors.New("enter a role key starting with a letter and using only letters, numbers, hyphens or underscores")
		}
		switch a.Role {
		case "admin", "administrator", "superadmin", "owner", "user", "student":
			return errors.New("choose a role key distinct from built-in account roles")
		}
		return nil
	default:
		return errors.New("choose a supported approval action")
	}
}

func checkApprovalAction(ctx context.Context, db formRowReader, formID int64, action *ApprovalAction) error {
	if err := checkFollowUp(ctx, db, formID, action); err != nil {
		return err
	}
	if action == nil || action.Kind != "assign_role" {
		return nil
	}
	if action.RoleID <= 0 {
		return errInvalidApprovalRole
	}
	role, err := access.ApprovalRole(ctx, db, action.RoleID, "")
	if errors.Is(err, pgx.ErrNoRows) {
		return errInvalidApprovalRole
	}
	if err == nil {
		action.Role = role.Key
	}
	return err
}

type formRowReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func checkFollowUp(ctx context.Context, db formRowReader, formID int64, action *ApprovalAction) error {
	if action == nil || action.Kind != "follow_up_form" {
		return nil
	}
	if action.FormID == formID {
		return errInvalidFollowUp
	}
	var eligible bool
	err := db.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM forms f
            JOIN form_versions v ON v.form_id = f.id AND v.version = f.published_version
            WHERE f.id = $1 AND v.definition->'requiresApprovalGrant' = 'true'::jsonb
        )
    `, action.FormID).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return errInvalidFollowUp
	}
	return nil
}

// This expression uses aliases f (form) and v (published version).
// A previously submitted response stays visible after future visibility changes.
const publishedFormAccess = `(
    $3::boolean
    OR v.definition->'requiresApprovalGrant' IS DISTINCT FROM 'true'::jsonb
    OR EXISTS (
        SELECT 1 FROM form_submissions own WHERE own.form_id = f.id AND own.user_id = $2
    )
    OR EXISTS (
        SELECT 1 FROM form_verifications grant_record
        JOIN form_submissions source ON source.id = grant_record.submission_id
        WHERE grant_record.follow_up_form_id = f.id AND source.user_id = $2
    )
)`

func recordVerification(ctx context.Context, tx pgx.Tx, submissionID int64, action *ApprovalAction) (*int64, error) {
	var followUp *int64
	var role *string
	var roleID *int64
	if action != nil {
		switch action.Kind {
		case "follow_up_form":
			followUp = &action.FormID
		case "assign_role":
			eligible, err := access.ApprovalRole(ctx, tx, action.RoleID, action.Role)
			if err == nil {
				role, roleID = &eligible.Key, &eligible.ID
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
	}
	_, err := tx.Exec(ctx, `
        INSERT INTO form_verifications (submission_id, follow_up_form_id, role_key, access_role_id)
        VALUES ($1, $2, $3, $4)
    `, submissionID, followUp, role, roleID)
	return roleID, err
}
