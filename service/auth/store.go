package auth

import "github.com/jackc/pgx/v5/pgxpool"

type AuthStore struct {
	db *pgxpool.Pool
}

func NewAuthStore(db *pgxpool.Pool) *AuthStore {
	return &AuthStore{
		db: db,
	}
}
