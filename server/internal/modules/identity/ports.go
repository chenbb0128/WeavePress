package identity

import "context"

type UserStore interface {
	CreateUser(context.Context, CreateUserParams) (User, error)
	FindUserByID(context.Context, uint64) (User, error)
	FindUserByUsername(context.Context, string) (User, error)
	UpdateProfile(context.Context, UpdateProfileParams) error
	SetUserStatus(context.Context, SetUserStatusParams) error
}
