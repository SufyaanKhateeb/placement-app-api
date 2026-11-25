package user

import (
	"context"
	"errors"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ResolveGoogleAccount(ctx context.Context, identity auth.GoogleIdentity) (types.TokenInput, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return types.TokenInput{}, err
	}
	defer tx.Rollback(ctx)

	// Coordinate simultaneous callbacks for the same verified email.
	email, err := access.LockAccountEmail(ctx, tx, identity.Email)
	if err != nil {
		return types.TokenInput{}, err
	}

	var account types.TokenInput
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(user_id, admin_id),
		       CASE WHEN admin_id IS NULL THEN 'user' ELSE 'admin' END
		FROM oidc_identity WHERE issuer = $1 AND subject = $2
	`, identity.Issuer, identity.Subject).Scan(&account.Id, &account.Type)
	if err == nil {
		return account, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.TokenInput{}, err
	}
	if err := access.CheckSignupAddress(ctx, tx, email); err != nil {
		return types.TokenInput{}, err
	}

	// Google can create regular accounts. Existing accounts require explicit linking.
	account.Type = types.RoleUser
	err = tx.QueryRow(ctx, `
		INSERT INTO app_user (firstname, lastname, email, password)
		SELECT $1, $2, $3::text, ''
		WHERE NOT EXISTS (SELECT 1 FROM app_user WHERE lower(email) = $3)
		  AND NOT EXISTS (SELECT 1 FROM admin_user WHERE lower(email) = $3)
		ON CONFLICT (email) DO NOTHING
		RETURNING id
	`, identity.FirstName, identity.LastName, email).Scan(&account.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.TokenInput{}, auth.ErrGoogleAccountExists
	}
	if err != nil {
		return types.TokenInput{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO oidc_identity (issuer, subject, user_id) VALUES ($1, $2, $3)
	`, identity.Issuer, identity.Subject, account.Id); err != nil {
		return types.TokenInput{}, err
	}
	return account, tx.Commit(ctx)
}

func (s *Store) ResolveInvitedGoogleAccount(ctx context.Context, identity auth.GoogleIdentity, hash []byte) (types.TokenInput, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return types.TokenInput{}, err
	}
	defer tx.Rollback(ctx)
	invite, err := access.LockInvitationForEnrollment(ctx, tx, hash)
	if err != nil {
		return types.TokenInput{}, err
	}
	if !strings.EqualFold(invite.Email, identity.Email) {
		return types.TokenInput{}, access.ErrInvitationAccount
	}

	account := types.TokenInput{Type: types.RoleUser}
	if invite.UserID != nil {
		err := tx.QueryRow(ctx, `
			SELECT user_id FROM oidc_identity WHERE issuer = $1 AND subject = $2 AND user_id = $3
		`, identity.Issuer, identity.Subject, *invite.UserID).Scan(&account.Id)
		if errors.Is(err, pgx.ErrNoRows) {
			return types.TokenInput{}, access.ErrInvitationAccount
		}
		if err != nil {
			return types.TokenInput{}, err
		}
		return account, tx.Commit(ctx)
	}

	account.Id, err = createInvitedAccount(ctx, tx, invite.Email, identity.FirstName, identity.LastName, "")
	if err != nil {
		return types.TokenInput{}, err
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO oidc_identity (issuer, subject, user_id) VALUES ($1, $2, $3)
		ON CONFLICT (issuer, subject) DO NOTHING
	`, identity.Issuer, identity.Subject, account.Id)
	if err != nil {
		return types.TokenInput{}, err
	}
	if result.RowsAffected() == 0 {
		return types.TokenInput{}, access.ErrInvitationAccount
	}
	if err := access.BindInvitationAccount(ctx, tx, hash, account.Id); err != nil {
		return types.TokenInput{}, err
	}
	return account, tx.Commit(ctx)
}
