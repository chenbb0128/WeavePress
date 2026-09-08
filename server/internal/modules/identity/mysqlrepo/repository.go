package mysqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"github.com/chenbb0128/weavepress/server/internal/modules/identity"
	"github.com/chenbb0128/weavepress/server/internal/platform/database"
	"github.com/chenbb0128/weavepress/server/internal/platform/database/sqlc"
)

const duplicateEntryErrorNumber uint16 = 1062

type Repository struct {
	queries *sqlc.Queries
}

func New(exec database.DBTX) *Repository {
	return &Repository{queries: sqlc.New(exec)}
}

func (r *Repository) CreateUser(ctx context.Context, params identity.CreateUserParams) (identity.User, error) {
	result, err := r.queries.CreateUser(ctx, sqlc.CreateUserParams{
		Username:     params.Username,
		PasswordHash: params.PasswordHash,
		Nickname:     params.Nickname,
		Avatar:       params.Avatar,
		Role:         string(params.Role),
		Status:       string(params.Status),
	})
	if err != nil {
		return identity.User{}, translateError(err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return identity.User{}, fmt.Errorf("read created user id: %w", err)
	}
	if id <= 0 {
		return identity.User{}, fmt.Errorf("read created user id: invalid id %d", id)
	}

	return r.FindUserByID(ctx, uint64(id))
}

func (r *Repository) FindUserByID(ctx context.Context, id uint64) (identity.User, error) {
	user, err := r.queries.GetUserByID(ctx, id)
	if err != nil {
		return identity.User{}, translateError(err)
	}
	return mapUser(user), nil
}

func (r *Repository) FindUserByUsername(ctx context.Context, username string) (identity.User, error) {
	user, err := r.queries.GetUserByUsername(ctx, username)
	if err != nil {
		return identity.User{}, translateError(err)
	}
	return mapUser(user), nil
}

func (r *Repository) UpdateProfile(ctx context.Context, params identity.UpdateProfileParams) error {
	result, err := r.queries.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		ID:       params.ID,
		Nickname: params.Nickname,
		Avatar:   params.Avatar,
	})
	if err != nil {
		return translateError(err)
	}
	return ensureAffected(result)
}

func (r *Repository) SetUserStatus(ctx context.Context, params identity.SetUserStatusParams) error {
	result, err := r.queries.SetUserStatus(ctx, sqlc.SetUserStatusParams{
		ID:     params.ID,
		Status: string(params.Status),
	})
	if err != nil {
		return translateError(err)
	}
	return ensureAffected(result)
}

func mapUser(user sqlc.User) identity.User {
	return identity.User{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Nickname:     user.Nickname,
		Avatar:       user.Avatar,
		Role:         identity.UserRole(user.Role),
		Status:       identity.UserStatus(user.Status),
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}
}

func translateError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return identity.ErrUserNotFound
	}

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == duplicateEntryErrorNumber {
		return identity.ErrUsernameTaken
	}

	return err
}

func ensureAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if affected == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}
