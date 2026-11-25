package user

import (
	"context"
	"fmt"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/access"
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

func (s *Store) CheckUserWithEmailExists(email string) (bool, error) {
	rows, err := s.db.Query(context.Background(), "select exists(select id from app_user where email = $1)", email)
	if err != nil {
		return true, err
	}

	exists := true
	for rows.Next() {
		err = rows.Scan(&exists)
		if err != nil {
			return true, err
		}
	}
	return exists, nil
}

func (s *Store) GetUserByEmail(email string) (*types.User, error) {
	rows, err := s.db.Query(context.Background(), "select * from app_user where email = $1", email)
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

func (s *Store) GetAdminUserByEmail(email string) (*types.AdminUser, error) {
	rows, err := s.db.Query(context.Background(), "select * from admin_user where email = $1", email)
	if err != nil {
		return nil, err
	}

	u := new(types.AdminUser)
	for rows.Next() {
		u, err = scanRowIntoAdminUser(rows)
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
		&u.Verified,
		&u.CreatedAt,
	)

	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	return u, nil
}

func scanRowIntoAdminUser(rows pgx.Rows) (*types.AdminUser, error) {
	u := new(types.AdminUser)

	err := rows.Scan(
		&u.Id,
		&u.FirstName,
		&u.LastName,
		&u.Email,
		&u.Password,
		&u.Role,
		&u.CreatedAt,
	)

	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	return u, nil
}

func (s *Store) GetUserById(id int) (*types.User, error) {
	rows, err := s.db.Query(context.Background(), "select * from app_user where id = $1", id)
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

func (s *Store) GetAdminById(id int) (*types.AdminUser, error) {
	rows, err := s.db.Query(context.Background(), "select * from admin_user where id = $1", id)
	if err != nil {
		return nil, err
	}

	u := new(types.AdminUser)
	for rows.Next() {
		u, err = scanRowIntoAdminUser(rows)
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
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	email, err := access.LockAccountEmail(ctx, tx, u.Email)
	if err != nil {
		return 0, err
	}
	if err := access.CheckSignupAddress(ctx, tx, email); err != nil {
		return 0, err
	}
	var id int
	err = tx.QueryRow(ctx, "INSERT INTO app_user (firstname, lastname, email, password) VALUES ($1,$2,$3,$4) RETURNING id",
		u.FirstName, u.LastName, u.Email, u.Password).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}
