package user

import (
	"context"
	"fmt"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{
		db: db,
	}
}

func (s *Store) GetUserByEmail(email string) (*types.User, error) {
	rows, err := s.db.Query(context.Background(), "select * from users where email = $1", email)
	if err != nil {
		return nil, err
	}

	u := new(types.User)
	for rows.Next() {
		u, err = scanRowIntoUser(rows)
		if err != nil {
			return nil, err
		}
	}

	if u.Id == 0 {
		return nil, fmt.Errorf("user not found")
	}

	return u, nil
}

func scanRowIntoUser(rows pgx.Rows) (*types.User, error) {
	u := new(types.User)

	err := rows.Scan(
		&u.Id,
		&u.FirstName,
		&u.LastName,
		&u.Email,
		&u.Password,
		&u.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return u, nil
}

func (s *Store) GetUserById(id int) (*types.User, error) {
	rows, err := s.db.Query(context.Background(), "select * from users where id = $1", id)
	if err != nil {
		return nil, err
	}

	u := new(types.User)
	for rows.Next() {
		u, err = scanRowIntoUser(rows)
		if err != nil {
			return nil, err
		}
	}

	if u.Id == 0 {
		return nil, fmt.Errorf("user not found")
	}

	return u, nil
}

func (s *Store) CreateUser(u types.User) (int, error) {
	var lastInserId int
	rows, err := s.db.Query(context.Background(), "insert into users (firstName, lastName, email, password) values ($1,$2,$3,$4) returning id", u.FirstName, u.LastName, u.Email, u.Password)
	if err != nil {
		return 0, err
	}

	// get the id of the created user
	rows.Next()
	err = rows.Scan(&lastInserId)
	if err != nil {
		return lastInserId, err
	}

	return lastInserId, nil
}
