package user

import (
	"context"
	"fmt"
	"net/http"

	"github.com/SufyaanKhateeb/college-placement-app-api/service/auth"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
)

type UserService struct {
	Store types.UserStore
}

func NewUserService(store types.UserStore) *UserService {
	return &UserService{
		Store: store,
	}
}

func (us *UserService) GetStudentUserById(ctx context.Context, uid int) (*types.User, error) {
	user, err := us.Store.GetUserById(uid)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("invalid user, user not found")
	}
	return user, nil
}

func (us *UserService) GetAdminUserById(ctx context.Context, uid int) (*types.AdminUser, error) {
	user, err := us.Store.GetAdminById(uid)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("invalid user, user not found")
	}
	return user, nil
}

func (us *UserService) GetStudentUserByEmail(ctx context.Context, emailId string) (*types.User, error) {
	user, err := us.Store.GetUserByEmail(emailId)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("invalid user, user not found")
	}
	return user, nil
}

func (us *UserService) GetAdminUserByEmail(ctx context.Context, emailId string) (*types.AdminUser, error) {
	user, err := us.Store.GetAdminUserByEmail(emailId)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("invalid user, user not found")
	}
	return user, nil
}

func (us *UserService) LoginStudentUser(ctx context.Context, payload *types.LoginUserPayload) (*types.User, error) {
	// get the user using the email
	u, err := us.Store.GetUserByEmail(payload.Email)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("not found, invalid email or password")
	}

	// check if password matches hash
	if err = auth.CompareHashAndPassword(payload.Password, u.Password); err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("not found, invalid email or password")
	}

	return u, nil
}

func (us *UserService) LoginAdminUser(ctx context.Context, payload *types.LoginUserPayload) (*types.AdminUser, error) {
	// get the user using the email
	u, err := us.Store.GetAdminUserByEmail(payload.Email)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("not found, invalid email or password")
	}

	// check if password matches hash
	if err = auth.CompareHashAndPassword(payload.Password, u.Password); err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("not found, invalid email or password")
	}

	return u, nil
}

func (us *UserService) RegisterStudentUser(ctx context.Context, payload *types.RegisterUserPayload) (*types.User, error) {
	// check if the user exists
	exists, err := us.Store.CheckUserWithEmailExists(payload.Email)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusInternalServerError)
		return nil, err
	}
	if exists {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("user with email %s already exists", payload.Email)
	}

	// create a new user
	hashedPassword, err := auth.HashPassword(payload.Password)

	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusInternalServerError)
		return nil, err
	}

	u := types.User{
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Email:     payload.Email,
		Password:  hashedPassword,
	}

	id, err := us.Store.CreateUser(u)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusInternalServerError)
		return nil, err
	} else {
		u.Id = id
	}

	return &u, nil
}

func (us *UserService) RegisterAdminUser(ctx context.Context, payload *types.RegisterUserPayload) (*types.AdminUser, error) {
	// check if the user exists
	exists, err := us.Store.CheckAdminUserWithEmailExists(payload.Email)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusInternalServerError)
		return nil, err
	}
	if exists {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusBadRequest)
		return nil, fmt.Errorf("user with email %s already exists", payload.Email)
	}

	// create a new user
	hashedPassword, err := auth.HashPassword(payload.Password)

	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusInternalServerError)
		return nil, err
	}

	u := types.AdminUser{
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Email:     payload.Email,
		Password:  hashedPassword,
	}

	id, err := us.Store.CreateAdminUser(u)
	if err != nil {
		context.WithValue(ctx, types.HttpStatusCodeKey, http.StatusInternalServerError)
		return nil, err
	} else {
		u.Id = id
	}

	return &u, nil
}
