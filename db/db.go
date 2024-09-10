package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewDbPool(url string) (*pgxpool.Pool, error) {
	return pgxpool.New(context.Background(), url)
}
