package identity

import "errors"

var (
	ErrUserNotFound  = errors.New("identity: user not found")
	ErrUsernameTaken = errors.New("identity: username taken")
)
