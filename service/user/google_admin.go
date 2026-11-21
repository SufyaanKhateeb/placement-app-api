package user

import (
	"context"
	"errors"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CheckGoogleAdminPassword(ctx context.Context, adminID int, password string) error {
	var hash string
	err := s.db.QueryRow(ctx, "SELECT password FROM admin_user WHERE id = $1", adminID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrGoogleAdminPassword
	}
	if err != nil {
		return err
	}
	if auth.CompareHashAndPassword(password, hash) != nil {
		return auth.ErrGoogleAdminPassword
	}
	return nil
}

func (s *Store) FindGoogleAdmin(ctx context.Context, identity auth.GoogleIdentity) (types.TokenInput, error) {
	var session types.TokenInput
	err := s.db.QueryRow(ctx, `
		SELECT admin_id FROM oidc_identity WHERE issuer = $1 AND subject = $2 AND admin_id IS NOT NULL
	`, identity.Issuer, identity.Subject).Scan(&session.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.TokenInput{}, auth.ErrGoogleAdminUnlinked
	}
	session.Type = types.RoleAdmin
	return session, err
}

func (s *Store) LinkGoogleAdmin(ctx context.Context, adminID int, identity auth.GoogleIdentity) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Serialize Google linking for this admin across API instances.
	var email string
	if err := tx.QueryRow(ctx, "SELECT email FROM admin_user WHERE id = $1 FOR UPDATE", adminID).Scan(&email); err != nil {
		return err
	}
	if !strings.EqualFold(email, identity.Email) {
		return auth.ErrGoogleAdminEmailMismatch
	}

	var linkedSubject string
	err = tx.QueryRow(ctx, `
		SELECT subject FROM oidc_identity WHERE issuer = $1 AND admin_id = $2
	`, identity.Issuer, adminID).Scan(&linkedSubject)
	if err == nil {
		if linkedSubject != identity.Subject {
			return auth.ErrGoogleAdminLinkConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	result, err := tx.Exec(ctx, `
		INSERT INTO oidc_identity (issuer, subject, admin_id) VALUES ($1, $2, $3)
		ON CONFLICT (issuer, subject) DO NOTHING
	`, identity.Issuer, identity.Subject, adminID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return auth.ErrGoogleAdminLinkConflict
	}
	return tx.Commit(ctx)
}

func (s *Store) GoogleAdminLinked(ctx context.Context, adminID int, issuer string) (bool, error) {
	var linked bool
	err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM oidc_identity WHERE admin_id = $1 AND issuer = $2)", adminID, issuer).Scan(&linked)
	return linked, err
}
