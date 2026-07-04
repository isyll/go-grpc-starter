package users

import (
	"context"
	"time"

	"github.com/isyll/go-grpc-starter/internal/events"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type ProfileUpdate struct {
	FirstName *string
	LastName  *string
	Bio       *string
	Avatar    *string
}

type Service struct {
	repo     Repository
	sessions SessionRevoker
	bus      *events.Bus
	logger   *logger.Logger
}

func NewService(
	repo Repository,
	sessions SessionRevoker,
	bus *events.Bus,
	logx *logger.Logger,
) *Service {
	return &Service{repo: repo, sessions: sessions, bus: bus, logger: logx}
}

func (s *Service) Get(ctx context.Context, id int64) (*models.User, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) UpdateProfile(
	ctx context.Context, id int64, upd ProfileUpdate,
) (*models.User, error) {
	return s.repo.UpdateProfile(ctx, id, upd)
}

func (s *Service) DeleteAccount(ctx context.Context, id int64) error {
	if err := s.repo.SoftDeleteByID(ctx, id); err != nil {
		return err
	}
	if err := s.sessions.RevokeAllSessions(ctx, id, "account_deleted"); err != nil {
		s.logger.Warn("revoke sessions on delete failed", "error", err, "user_id", id)
	}
	if err := s.bus.Publish(ctx, &events.UserAccountDeleted{
		UserID:     id,
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		s.logger.Warn("publish account deleted failed", "error", err, "user_id", id)
	}
	return nil
}

func (s *Service) List(ctx context.Context, offset, limit int) ([]models.User, int64, error) {
	return s.repo.List(ctx, offset, limit)
}

func (s *Service) SetStatus(ctx context.Context, id int64, status models.UserStatus) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *Service) SetRole(ctx context.Context, id int64, role models.UserRole) error {
	return s.repo.UpdateRole(ctx, id, role)
}
