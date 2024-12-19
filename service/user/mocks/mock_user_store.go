package mocks

import (
	"fmt"

	"github.com/SufyaanKhateeb/college-placement-app-api/types"
)

type MockAuthStore struct{}

type MockUserStore struct {
	UserExists bool
	IsAdmin    bool
}

func (s *MockUserStore) CheckUserWithEmailExists(email string) (bool, error) {
	if s.UserExists {
		return true, nil
	}
	return false, nil
}

func (s *MockUserStore) CheckAdminUserWithEmailExists(email string) (bool, error) {
	if s.IsAdmin && s.UserExists {
		return true, nil
	}
	return false, nil
}

func (s *MockUserStore) GetUserByEmail(email string) (*types.User, error) {
	if s.UserExists {
		return &types.User{
			Id: 1,
		}, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (s *MockUserStore) GetAdminUserByEmail(email string) (*types.AdminUser, error) {
	if s.IsAdmin && s.UserExists {
		return &types.AdminUser{
			Id: 2,
		}, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (s *MockUserStore) GetUserById(id int) (*types.User, error) {
	if s.UserExists {
		return &types.User{
			Id: 1,
		}, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (s *MockUserStore) GetAdminById(id int) (*types.AdminUser, error) {
	if s.IsAdmin && s.UserExists {
		return &types.AdminUser{
			Id: 2,
		}, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (s *MockUserStore) CreateUser(u types.User) (int, error) {
	return 1, nil
}

func (s *MockUserStore) CreateAdminUser(u types.AdminUser) (int, error) {
	return 2, nil
}
