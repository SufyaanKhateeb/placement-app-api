package user

import (
	"context"
	"errors"
	"strings"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CheckGoogleAdminPassword(ctx context.Context, admin types.TokenInput, password string) error {
	var hash string
	err := s.db.QueryRow(ctx, "SELECT password "+adminAccountSource(admin), admin.Id).Scan(&hash)
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
		SELECT COALESCE(o.admin_id, o.user_id), CASE WHEN o.admin_id IS NULL THEN 'user' ELSE 'admin' END
		FROM oidc_identity o WHERE issuer = $1 AND subject = $2
		  AND (o.admin_id IS NOT NULL OR EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = o.user_id))
	`, identity.Issuer, identity.Subject).Scan(&session.Id, &session.Type)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.TokenInput{}, auth.ErrGoogleAdminUnlinked
	}
	session.AdminMode = session.Type == types.RoleUser
	return session, err
}

func (s *Store) LinkGoogleAdmin(ctx context.Context, admin types.TokenInput, identity auth.GoogleIdentity) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Serialize Google linking for this admin across API instances.
	var email string
	if err := tx.QueryRow(ctx, "SELECT email "+adminAccountSource(admin)+" FOR UPDATE", admin.Id).Scan(&email); err != nil {
		return err
	}
	if !strings.EqualFold(email, identity.Email) {
		return auth.ErrGoogleAdminEmailMismatch
	}

	var linkedSubject string
	column := adminIdentityColumn(admin)
	err = tx.QueryRow(ctx, "SELECT subject FROM oidc_identity WHERE issuer = $1 AND "+column+" = $2",
		identity.Issuer, admin.Id).Scan(&linkedSubject)
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
		INSERT INTO oidc_identity (issuer, subject, `+column+`) VALUES ($1, $2, $3)
		ON CONFLICT (issuer, subject) DO NOTHING
	`, identity.Issuer, identity.Subject, admin.Id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return auth.ErrGoogleAdminLinkConflict
	}
	return tx.Commit(ctx)
}

func (s *Store) GoogleAdminLinked(ctx context.Context, admin types.TokenInput, issuer string) (bool, error) {
	var linked bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM oidc_identity WHERE `+adminIdentityColumn(admin)+` = a.id AND issuer = $2)
		`+adminAccountSource(admin), admin.Id, issuer).Scan(&linked)
	return linked, err
}

func adminAccountSource(admin types.TokenInput) string {
	if admin.Type == types.RoleUser {
		return "FROM app_user a WHERE a.id = $1 AND EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = a.id)"
	}
	return "FROM admin_user a WHERE a.id = $1"
}

func adminIdentityColumn(admin types.TokenInput) string {
	if admin.Type == types.RoleUser {
		return "user_id"
	}
	return "admin_id"
}
