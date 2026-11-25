package user

import (
	"context"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
)

func (s *Store) IsPromotedAdmin(ctx context.Context, id int) (bool, error) {
	var admin bool
	err := s.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM admin_memberships WHERE user_id = $1)", id).Scan(&admin)
	return admin, err
}

func (s *Store) CreateInvitedUser(ctx context.Context, hash []byte, payload types.RegisterUserPayload) (int, error) {
	password, err := auth.HashPassword(payload.Password)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	invite, err := access.LockInvitationForEnrollment(ctx, tx, hash)
	if err != nil {
		return 0, err
	}
	if invite.UserID != nil {
		return 0, access.ErrInvitationAccount
	}
	id, err := createInvitedAccount(ctx, tx, invite.Email, payload.FirstName, payload.LastName, password)
	if err != nil {
		return 0, err
	}
	if err := access.BindInvitationAccount(ctx, tx, hash, id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func createInvitedAccount(ctx context.Context, tx pgx.Tx, email, first, last, password string) (int, error) {
	var id int
	err := tx.QueryRow(ctx, `
		INSERT INTO app_user (firstname, lastname, email, password)
		SELECT $1, $2, $3::text, $4
		WHERE NOT EXISTS (SELECT 1 FROM app_user WHERE lower(email) = $3)
		  AND NOT EXISTS (SELECT 1 FROM admin_user WHERE lower(email) = $3)
		ON CONFLICT (email) DO NOTHING
		RETURNING id
	`, first, last, email, password).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, access.ErrInvitationAccount
	}
	return id, err
}
