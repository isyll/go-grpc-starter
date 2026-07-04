package grpcsvc

import (
	"bytes"
	"context"
	"errors"
	"io"

	apiv1 "github.com/isyll/go-grpc-starter/gen/api/v1"
	"github.com/isyll/go-grpc-starter/internal/errs"
	"github.com/isyll/go-grpc-starter/internal/errs/codes"
	"github.com/isyll/go-grpc-starter/internal/notifications"
	"github.com/isyll/go-grpc-starter/internal/reqctx"
	"github.com/isyll/go-grpc-starter/internal/settings"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/idenc"

	"google.golang.org/protobuf/types/known/emptypb"
)

type UserServer struct {
	apiv1.UnimplementedUserServiceServer
	users    *users.Service
	settings *settings.Service
	notifs   *notifications.Service
	enc      idenc.IDEncoder
}

func NewUserServer(
	u *users.Service,
	s *settings.Service,
	n *notifications.Service,
	enc idenc.IDEncoder,
) *UserServer {
	return &UserServer{users: u, settings: s, notifs: n, enc: enc}
}

func (s *UserServer) GetMe(ctx context.Context, _ *emptypb.Empty) (*apiv1.User, error) {
	u, err := s.users.Get(ctx, reqctx.SubjectFrom(ctx).UserID)
	if err != nil {
		return nil, err
	}
	return toProtoUser(u, s.enc), nil
}

func (s *UserServer) UpdateMe(ctx context.Context, req *apiv1.UpdateMeRequest) (*apiv1.User, error) {
	u, err := s.users.UpdateProfile(ctx, reqctx.SubjectFrom(ctx).UserID, users.ProfileUpdate{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Bio:       req.Bio,
		Avatar:    req.Avatar,
	})
	if err != nil {
		return nil, err
	}
	return toProtoUser(u, s.enc), nil
}

func (s *UserServer) DeleteMe(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.users.DeleteAccount(ctx, reqctx.SubjectFrom(ctx).UserID); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *UserServer) GetUser(ctx context.Context, req *apiv1.GetUserRequest) (*apiv1.PublicUser, error) {
	id, err := s.enc.Decode(req.GetId())
	if err != nil {
		return nil, errs.BadRequest(codes.InvalidUserID, "user.invalid_id")
	}
	u, err := s.users.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toProtoPublicUser(u, s.enc), nil
}

func (s *UserServer) UploadAvatar(stream apiv1.UserService_UploadAvatarServer) error {
	ctx := stream.Context()
	userID := reqctx.SubjectFrom(ctx).UserID

	var (
		contentType string
		gotMeta     bool
		buf         bytes.Buffer
	)
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch data := req.GetData().(type) {
		case *apiv1.UploadAvatarRequest_ContentType:
			contentType = data.ContentType
			gotMeta = true
		case *apiv1.UploadAvatarRequest_Chunk:
			if buf.Len()+len(data.Chunk) > users.MaxAvatarBytes {
				return errs.BadRequest(codes.AvatarTooLarge, "user.avatar_too_large")
			}
			buf.Write(data.Chunk)
		}
	}
	if !gotMeta {
		return errs.BadRequest(codes.InvalidPayload, "user.avatar_missing_content_type")
	}

	url, err := s.users.UploadAvatar(ctx, userID, contentType, buf.Bytes())
	if err != nil {
		return err
	}
	return stream.SendAndClose(&apiv1.UploadAvatarResponse{AvatarUrl: url})
}

func (s *UserServer) GetSettings(ctx context.Context, _ *emptypb.Empty) (*apiv1.Settings, error) {
	set, err := s.settings.Get(ctx, reqctx.SubjectFrom(ctx).UserID)
	if err != nil {
		return nil, err
	}
	return toProtoSettings(set), nil
}

func (s *UserServer) UpdateSettings(ctx context.Context, req *apiv1.Settings) (*apiv1.Settings, error) {
	if err := s.settings.Update(ctx, reqctx.SubjectFrom(ctx).UserID, fromProtoSettings(req)); err != nil {
		return nil, err
	}
	return req, nil
}

func (s *UserServer) RegisterPushToken(ctx context.Context, req *apiv1.RegisterPushTokenRequest) (*emptypb.Empty, error) {
	err := s.notifs.RegisterToken(ctx, reqctx.SubjectFrom(ctx).UserID, notifications.RegisterTokenInput{
		DeviceID:   req.GetDeviceId(),
		Token:      req.GetToken(),
		Platform:   notifications.NotificationPlatform(req.GetPlatform()),
		AppVersion: req.GetAppVersion(),
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *UserServer) GetNotificationPreferences(ctx context.Context, _ *emptypb.Empty) (*apiv1.NotificationPreferences, error) {
	prefs, err := s.notifs.GetPreferences(ctx, reqctx.SubjectFrom(ctx).UserID)
	if err != nil {
		return nil, err
	}
	return toProtoNotifPrefs(prefs), nil
}

func (s *UserServer) UpdateNotificationPreferences(ctx context.Context, req *apiv1.NotificationPreferences) (*apiv1.NotificationPreferences, error) {
	push, email, marketing, qenabled := req.GetPush(), req.GetEmail(), req.GetMarketing(), req.GetQuietHoursEnabled()
	start, end, tz := req.GetQuietHoursStart(), req.GetQuietHoursEnd(), req.GetTimezone()
	prefs, err := s.notifs.UpdatePreferences(ctx, reqctx.SubjectFrom(ctx).UserID, notifications.PreferencesUpdate{
		Push:              &push,
		Email:             &email,
		Marketing:         &marketing,
		QuietHoursEnabled: &qenabled,
		QuietHoursStart:   &start,
		QuietHoursEnd:     &end,
		Timezone:          &tz,
	})
	if err != nil {
		return nil, err
	}
	return toProtoNotifPrefs(prefs), nil
}
