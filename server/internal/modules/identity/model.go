package identity

import "time"

type UserStatus string
type UserRole string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
	UserRoleAdmin      UserRole   = "admin"
	UserRoleEditor     UserRole   = "editor"
)

type User struct {
	ID           uint64
	Username     string
	PasswordHash string
	Nickname     string
	Avatar       string
	Role         UserRole
	Status       UserStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateUserParams struct {
	Username     string
	PasswordHash string
	Nickname     string
	Avatar       string
	Role         UserRole
	Status       UserStatus
}

type UpdateProfileParams struct {
	ID       uint64
	Nickname string
	Avatar   string
}

type SetUserStatusParams struct {
	ID     uint64
	Status UserStatus
}
