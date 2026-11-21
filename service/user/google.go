package user

import (
	"context"
	"errors"
	"strings"

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
	email := strings.ToLower(identity.Email)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", email); err != nil {
		return types.TokenInput{}, err
	}

	var account types.TokenInput
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(student_id, admin_id),
		       CASE WHEN admin_id IS NULL THEN 'student' ELSE 'admin' END
		FROM oidc_identity WHERE issuer = $1 AND subject = $2
	`, identity.Issuer, identity.Subject).Scan(&account.Id, &account.Type)
	if err == nil {
		return account, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.TokenInput{}, err
	}

	// Google can create regular accounts. Existing accounts require explicit linking.
	account.Type = "student"
	err = tx.QueryRow(ctx, `
		INSERT INTO student_user (firstname, lastname, email, password)
		SELECT $1, $2, $3::text, ''
		WHERE NOT EXISTS (SELECT 1 FROM student_user WHERE lower(email) = $3)
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
		INSERT INTO oidc_identity (issuer, subject, student_id) VALUES ($1, $2, $3)
	`, identity.Issuer, identity.Subject, account.Id); err != nil {
		return types.TokenInput{}, err
	}
	return account, tx.Commit(ctx)
}
