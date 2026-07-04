package grpcsvc

import (
	"context"
	"time"

	apiv1 "github.com/isyll/go-grpc-starter/gen/api/v1"
	"github.com/isyll/go-grpc-starter/internal/event"
	"github.com/isyll/go-grpc-starter/internal/reqctx"
	"github.com/isyll/go-grpc-starter/internal/suspension"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/idenc"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type AdminServer struct {
	apiv1.UnimplementedAdminServiceServer
	users      *users.Service
	suspension *suspension.Service
	bus        *event.Bus
	enc        idenc.IDEncoder
}

func NewAdminServer(
	u *users.Service,
	s *suspension.Service,
	bus *event.Bus,
	enc idenc.IDEncoder,
) *AdminServer {
	return &AdminServer{users: u, suspension: s, bus: bus, enc: enc}
}

func (s *AdminServer) ListUsers(ctx context.Context, req *apiv1.ListUsersRequest) (*apiv1.ListUsersResponse, error) {
	page, size := pageParams(req.GetPage())
	list, total, err := s.users.List(ctx, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	out := make([]*apiv1.User, len(list))
	for i := range list {
		out[i] = toProtoUser(&list[i], s.enc)
	}
	return &apiv1.ListUsersResponse{Users: out, Total: total}, nil
}

func (s *AdminServer) GetUser(ctx context.Context, req *apiv1.AdminGetUserRequest) (*apiv1.User, error) {
	id, err := s.enc.Decode(req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user.invalid_id")
	}
	u, err := s.users.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toProtoUser(u, s.enc), nil
}

func (s *AdminServer) SuspendUser(ctx context.Context, req *apiv1.SuspendUserRequest) (*emptypb.Empty, error) {
	id, err := s.enc.Decode(req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user.invalid_id")
	}
	var until *time.Time
	if !req.GetPermanent() {
		t := time.Now().UTC().AddDate(0, 0, int(req.GetDays()))
		until = &t
	}
	if _, err := s.suspension.Suspend(ctx, suspension.SuspendInput{
		UserID:    id,
		Reason:    suspension.SuspensionReason(req.GetReason()),
		Details:   req.GetDetails(),
		Until:     until,
		Permanent: req.GetPermanent(),
	}); err != nil {
		return nil, err
	}
	if err := s.users.SetStatus(ctx, id, users.UserStatusSuspended); err != nil {
		return nil, err
	}
	s.audit(ctx, "user.suspend", req.GetId())
	return &emptypb.Empty{}, nil
}

func (s *AdminServer) UnsuspendUser(ctx context.Context, req *apiv1.UnsuspendUserRequest) (*emptypb.Empty, error) {
	id, err := s.enc.Decode(req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user.invalid_id")
	}
	if err := s.suspension.Unsuspend(ctx, id); err != nil {
		return nil, err
	}
	if err := s.users.SetStatus(ctx, id, users.UserStatusActive); err != nil {
		return nil, err
	}
	s.audit(ctx, "user.unsuspend", req.GetId())
	return &emptypb.Empty{}, nil
}

func (s *AdminServer) SetUserRole(ctx context.Context, req *apiv1.SetUserRoleRequest) (*emptypb.Empty, error) {
	id, err := s.enc.Decode(req.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user.invalid_id")
	}
	if err := s.users.SetRole(ctx, id, users.UserRole(req.GetRole())); err != nil {
		return nil, err
	}
	s.audit(ctx, "user.set_role", req.GetId())
	return &emptypb.Empty{}, nil
}

func (s *AdminServer) audit(ctx context.Context, action, resourceID string) {
	_ = s.bus.Publish(ctx, &event.AuditLogWritten{
		AdminID:    reqctx.SubjectFrom(ctx).UserID,
		Action:     action,
		Resource:   "user",
		ResourceID: resourceID,
		Status:     "success",
		RequestID:  reqctx.RequestIDFromContext(ctx),
		OccurredAt: time.Now().UTC(),
	})
}

func pageParams(p *apiv1.Page) (page, size int) {
	page, size = 1, 20
	if p != nil {
		if p.GetPage() > 0 {
			page = int(p.GetPage())
		}
		if p.GetPageSize() > 0 && p.GetPageSize() <= 100 {
			size = int(p.GetPageSize())
		}
	}
	return page, size
}
