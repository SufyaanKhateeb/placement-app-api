package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidInvitation  = errors.New("this invitation is invalid, expired, or already used")
	ErrInvitationRequired = errors.New("open your admin invitation to create an account with this email")
	ErrInvitationAccount  = errors.New("sign in to the account created with this invitation")
	errEmailInUse         = errors.New("this email belongs to an existing account; select an account to promote")
	errAlreadyAdmin       = errors.New("this account already has admin access")
	errAccountChanged     = errors.New("the selected account email changed; select the account again")
)

type Invitation struct {
	Email  string `json:"email"`
	UserID *int   `json:"-"`
}

func InvitationHash(token string) ([]byte, error) {
	value, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(value) != 32 {
		return nil, ErrInvalidInvitation
	}
	hash := sha256.Sum256(value)
	return hash[:], nil
}

// LockAccountEmail serializes sign-up, invitation, and promotion for the same address.
func LockAccountEmail(ctx context.Context, tx pgx.Tx, email string) (string, error) {
	email = strings.ToLower(email)
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", email)
	return email, err
}

func CheckSignupAddress(ctx context.Context, tx pgx.Tx, email string) error {
	var reserved bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM admin_invitations WHERE email = $1 AND accepted_at IS NULL AND expires_at > clock_timestamp()
		)
	`, email).Scan(&reserved)
	if err == nil && reserved {
		return ErrInvitationRequired
	}
	return err
}

func (s Store) createInvitation(ctx context.Context, actor types.TokenInput, email string) (string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256(secret[:])
	err := s.changeWithPermission(ctx, actor, InviteAdmins, func(tx pgx.Tx) error {
		email, err := LockAccountEmail(ctx, tx, email)
		if err != nil {
			return err
		}
		var onboarded *int
		err = tx.QueryRow(ctx, `
			SELECT user_id FROM admin_invitations WHERE email = $1 AND accepted_at IS NULL
		`, email).Scan(&onboarded)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var conflict bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM admin_user WHERE lower(email) = $1)
				OR EXISTS (SELECT 1 FROM app_user WHERE lower(email) = $1 AND id IS DISTINCT FROM $2::integer)
				OR EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = $2)
		`, email, onboarded).Scan(&conflict)
		if err != nil {
			return err
		}
		if conflict {
			return errEmailInUse
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO admin_invitations (email, token_hash, inviter_type, inviter_id, expires_at, user_id)
			VALUES ($1, $2, $3, $4, clock_timestamp() + interval '24 hours', $5)
			ON CONFLICT (email) DO UPDATE SET token_hash = EXCLUDED.token_hash,
				inviter_type = EXCLUDED.inviter_type, inviter_id = EXCLUDED.inviter_id,
				expires_at = EXCLUDED.expires_at, user_id = EXCLUDED.user_id, accepted_at = NULL
		`, email, hash[:], actor.Type, actor.Id, onboarded)
		if err != nil {
			return err
		}
		return Audit(ctx, tx, actor, "admin.invited", map[string]any{"email": email})
	})
	return token, err
}

func (s Store) Invitation(ctx context.Context, hash []byte) (Invitation, error) {
	var invite Invitation
	err := s.DB.QueryRow(ctx, `
		SELECT email, user_id FROM admin_invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > clock_timestamp()
	`, hash).Scan(&invite.Email, &invite.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrInvalidInvitation
	}
	return invite, err
}

func lockInvitation(ctx context.Context, tx pgx.Tx, hash []byte) (Invitation, types.TokenInput, error) {
	var invite Invitation
	var actor types.TokenInput
	err := tx.QueryRow(ctx, "SELECT email FROM admin_invitations WHERE token_hash = $1", hash).Scan(&invite.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return invite, actor, ErrInvalidInvitation
	}
	if err != nil {
		return invite, actor, err
	}
	if _, err := LockAccountEmail(ctx, tx, invite.Email); err != nil {
		return invite, actor, err
	}
	err = tx.QueryRow(ctx, `
		SELECT user_id, inviter_type, inviter_id FROM admin_invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > clock_timestamp() FOR UPDATE
	`, hash).Scan(&invite.UserID, &actor.Type, &actor.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrInvalidInvitation
	}
	actor.AdminMode = actor.Type == types.RoleUser
	return invite, actor, err
}

func LockInvitationForEnrollment(ctx context.Context, tx pgx.Tx, hash []byte) (Invitation, error) {
	invite, _, err := lockInvitation(ctx, tx, hash)
	return invite, err
}

func BindInvitationAccount(ctx context.Context, tx pgx.Tx, hash []byte, userID int) error {
	_, err := tx.Exec(ctx, `UPDATE admin_invitations SET user_id = $2 WHERE token_hash = $1`, hash, userID)
	return err
}

func (s Store) AcceptInvitation(ctx context.Context, hash []byte, recipient types.TokenInput) error {
	if recipient.Type != types.RoleUser {
		return ErrInvitationAccount
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockManagement(ctx, tx); err != nil {
		return err
	}
	invite, inviter, err := lockInvitation(ctx, tx, hash)
	if err != nil {
		return err
	}
	if invite.UserID == nil || *invite.UserID != recipient.Id {
		return ErrInvitationAccount
	}
	var email string
	err = tx.QueryRow(ctx, "SELECT email FROM app_user WHERE id = $1 FOR UPDATE", recipient.Id).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !strings.EqualFold(email, invite.Email)) {
		return ErrInvitationAccount
	}
	if err != nil {
		return err
	}
	permissions, err := resolvePermissions(ctx, tx, inviter)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !slices.Contains(permissions, InviteAdmins)) {
		return ErrInvalidInvitation
	}
	if err != nil {
		return err
	}
	if err := grantAdmin(ctx, tx, recipient.Id); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		UPDATE admin_invitations SET accepted_at = clock_timestamp()
		WHERE token_hash = $1 AND expires_at > clock_timestamp()
	`, hash)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrInvalidInvitation
	}
	if err := Audit(ctx, tx, inviter, "admin.invitation_accepted", adminTarget(recipient.Id, invite.Email)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) promote(ctx context.Context, actor types.TokenInput, id int, email string) error {
	return s.changeWithPermission(ctx, actor, PromoteAdmins, func(tx pgx.Tx) error {
		normalized, err := LockAccountEmail(ctx, tx, email)
		if err != nil {
			return err
		}
		var currentEmail string
		if err := tx.QueryRow(ctx, "SELECT email FROM app_user WHERE id = $1 FOR UPDATE", id).Scan(&currentEmail); err != nil {
			return err
		}
		if currentEmail != email {
			return errAccountChanged
		}
		var conflict bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM admin_user WHERE lower(email) = $1)
				OR EXISTS (SELECT 1 FROM app_user WHERE lower(email) = $1 AND id <> $2)
		`, normalized, id).Scan(&conflict); err != nil {
			return err
		}
		if conflict {
			return errEmailInUse
		}
		if err := grantAdmin(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE admin_invitations SET expires_at = LEAST(expires_at, clock_timestamp()) WHERE user_id = $1 AND accepted_at IS NULL", id); err != nil {
			return err
		}
		return Audit(ctx, tx, actor, "admin.promoted", adminTarget(id, currentEmail))
	})
}

func grantAdmin(ctx context.Context, tx pgx.Tx, id int) error {
	result, err := tx.Exec(ctx, "INSERT INTO admin_memberships (user_id) VALUES ($1) ON CONFLICT DO NOTHING", id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errAlreadyAdmin
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO admin_membership_roles (user_id, role_id)
		SELECT $1, id FROM access_roles WHERE key IN ('form-manager', 'reviewer')
	`, id)
	return err
}

func adminTarget(id int, email string) map[string]any {
	return map[string]any{"accountType": types.RoleUser, "accountId": id, "email": email}
}
