// Package users owns user profiles and account lifecycle.
package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/isyll/go-grpc-starter/gen/db"
	apperrors "github.com/isyll/go-grpc-starter/internal/errors"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/internal/store"
)

type Repository interface {
	Create(ctx context.Context, user *models.User) error
	FindByID(ctx context.Context, id int64) (*models.User, error)
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	ExistsByEmail(ctx context.Context, email string) bool
	UpdateLastLogin(ctx context.Context, id int64) error
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	MarkEmailVerified(ctx context.Context, id int64) error
	UpdateProfile(ctx context.Context, id int64, upd ProfileUpdate) (*models.User, error)
	UpdateStatus(ctx context.Context, id int64, status models.UserStatus) error
	UpdateRole(ctx context.Context, id int64, role models.UserRole) error
	SoftDeleteByID(ctx context.Context, id int64) error
	List(ctx context.Context, offset, limit int) ([]models.User, int64, error)
}

type repository struct {
	store *store.Store
}

func NewRepository(s *store.Store) Repository {
	return &repository{store: s}
}

func toUser(r db.AuthUser) *models.User {
	return &models.User{
		ID:              r.ID,
		Email:           r.Email,
		PasswordHash:    r.PasswordHash,
		FirstName:       r.FirstName,
		LastName:        r.LastName,
		Avatar:          r.Avatar,
		Bio:             r.Bio,
		Status:          models.UserStatus(r.Status),
		Role:            models.UserRole(r.Role),
		EmailVerifiedAt: store.TimePtr(r.EmailVerifiedAt),
		LastLoginAt:     store.TimePtr(r.LastLoginAt),
		CreatedAt:       store.Time(r.CreatedAt),
		UpdatedAt:       store.Time(r.UpdatedAt),
		DeletedAt:       store.TimePtr(r.DeletedAt),
	}
}

func (r *repository) Create(ctx context.Context, user *models.User) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.CreateUser(ctx, db.CreateUserParams{
			Email:        user.Email,
			PasswordHash: user.PasswordHash,
			FirstName:    user.FirstName,
			LastName:     user.LastName,
		})
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		*user = *toUser(row)
		return nil
	})
}

func (r *repository) FindByID(ctx context.Context, id int64) (*models.User, error) {
	var out *models.User
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetUserByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperrors.ErrUserNotFound
			}
			return fmt.Errorf("find user %d: %w", id, err)
		}
		out = toUser(row)
		return nil
	})
	return out, err
}

func (r *repository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var out *models.User
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetUserByEmail(ctx, email)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperrors.ErrUserNotFound
			}
			return fmt.Errorf("find user by email: %w", err)
		}
		out = toUser(row)
		return nil
	})
	return out, err
}

func (r *repository) ExistsByEmail(ctx context.Context, email string) bool {
	var exists bool
	_ = r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		var err error
		exists, err = q.ExistsUserByEmail(ctx, email)
		return err
	})
	return exists
}

func (r *repository) UpdateLastLogin(ctx context.Context, id int64) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.UpdateUserLastLogin(ctx, id)
	})
}

func (r *repository) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.UpdateUserPasswordHash(ctx, db.UpdateUserPasswordHashParams{ID: id, PasswordHash: hash})
	})
}

func (r *repository) MarkEmailVerified(ctx context.Context, id int64) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.MarkUserEmailVerified(ctx, id)
	})
}

func (r *repository) UpdateProfile(
	ctx context.Context, id int64, upd ProfileUpdate,
) (*models.User, error) {
	var out *models.User
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
			FirstName: upd.FirstName,
			LastName:  upd.LastName,
			Bio:       upd.Bio,
			Avatar:    upd.Avatar,
			ID:        id,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperrors.ErrUserNotFound
			}
			return fmt.Errorf("update profile: %w", err)
		}
		out = toUser(row)
		return nil
	})
	return out, err
}

func (r *repository) UpdateStatus(ctx context.Context, id int64, status models.UserStatus) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.UpdateUserStatus(ctx, db.UpdateUserStatusParams{ID: id, Status: db.AuthUserStatus(status)})
	})
}

func (r *repository) UpdateRole(ctx context.Context, id int64, role models.UserRole) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.UpdateUserRole(ctx, db.UpdateUserRoleParams{ID: id, Role: db.AuthUserRole(role)})
	})
}

func (r *repository) SoftDeleteByID(ctx context.Context, id int64) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.SoftDeleteUser(ctx, id)
	})
}

func (r *repository) List(ctx context.Context, offset, limit int) ([]models.User, int64, error) {
	var users []models.User
	var total int64
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		var err error
		total, err = q.CountUsers(ctx)
		if err != nil {
			return fmt.Errorf("count users: %w", err)
		}
		rows, err := q.ListUsers(ctx, db.ListUsersParams{Limit: int32(limit), Offset: int32(offset)})
		if err != nil {
			return fmt.Errorf("list users: %w", err)
		}
		users = make([]models.User, len(rows))
		for i, row := range rows {
			users[i] = *toUser(row)
		}
		return nil
	})
	return users, total, err
}
