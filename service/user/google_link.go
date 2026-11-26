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

func autoLinkGoogleAccount(ctx context.Context, tx pgx.Tx, identity auth.GoogleIdentity, email string) (int, error) {
	var id int
	err := tx.QueryRow(ctx, `
		SELECT id FROM app_user
		WHERE lower(email) = $1 AND password <> ''
		  AND EXISTS (SELECT 1 FROM verified_user_emails WHERE user_id = app_user.id AND email = $1)
		  AND (SELECT count(*) FROM app_user WHERE lower(email) = $1) = 1
		  AND NOT EXISTS (SELECT 1 FROM admin_user WHERE lower(email) = $1)
		FOR UPDATE
	`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	eligible, err := regularGoogleLinkEligible(ctx, tx, id, email)
	if err != nil || !eligible {
		return 0, err
	}
	account := types.TokenInput{Id: id, Type: types.RoleUser}
	if err := linkGoogleIdentity(ctx, tx, account, identity); err != nil {
		if errors.Is(err, auth.ErrGoogleAdminLinkConflict) {
			return 0, auth.ErrGoogleAccountExists
		}
		return 0, err
	}
	return id, nil
}

func regularGoogleLinkEligible(ctx context.Context, tx pgx.Tx, id int, email string) (bool, error) {
	var eligible bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM app_user
			WHERE id = $1 AND lower(email) = $2 AND password <> ''
			  AND EXISTS (SELECT 1 FROM verified_user_emails WHERE user_id = $1 AND email = $2)
			  AND NOT EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = $1)
		)
	`, id, email).Scan(&eligible)
	return eligible, err
}

func (s *Store) CheckGoogleUserPassword(ctx context.Context, userID int, password string) error {
	var hash string
	err := s.db.QueryRow(ctx, `
		SELECT password FROM app_user
		WHERE id = $1
		  AND EXISTS (SELECT 1 FROM verified_user_emails WHERE user_id = $1 AND email = lower(app_user.email))
		  AND NOT EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = $1)
	`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrGoogleUserPassword
	}
	if err != nil {
		return err
	}
	if auth.CompareHashAndPassword(password, hash) != nil {
		return auth.ErrGoogleUserPassword
	}
	return nil
}

func (s *Store) LinkGoogleUser(ctx context.Context, id int, identity auth.GoogleIdentity) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	email, err := access.LockAccountEmail(ctx, tx, identity.Email)
	if err != nil {
		return err
	}
	var storedEmail string
	err = tx.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1 FOR UPDATE`, id).Scan(&storedEmail)
	if err != nil {
		return err
	}
	if !strings.EqualFold(storedEmail, identity.Email) {
		return auth.ErrGoogleAdminEmailMismatch
	}
	eligible, err := regularGoogleLinkEligible(ctx, tx, id, email)
	if err != nil {
		return err
	}
	if !eligible {
		return auth.ErrGoogleUserPassword
	}
	if err := linkGoogleIdentity(ctx, tx, types.TokenInput{Id: id, Type: types.RoleUser}, identity); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GoogleUserLinkStatus(ctx context.Context, id int, issuer string) (bool, bool, error) {
	var linked, canLink bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM oidc_identity WHERE user_id = $1 AND issuer = $2),
		    EXISTS (SELECT 1 FROM verified_user_emails WHERE user_id = $1 AND email = lower(app_user.email)) AND password <> ''
		        AND NOT EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = $1)
		FROM app_user WHERE id = $1
	`, id, issuer).Scan(&linked, &canLink)
	return linked, canLink, err
}
