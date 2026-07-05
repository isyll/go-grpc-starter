package grpcsvc

import (
	"context"

	authv1 "github.com/isyll/go-grpc-starter/gen/auth/v1"
	commonv1 "github.com/isyll/go-grpc-starter/gen/common/v1"
	"github.com/isyll/go-grpc-starter/internal/auth"
	"github.com/isyll/go-grpc-starter/internal/interceptor"
	"github.com/isyll/go-grpc-starter/internal/reqctx"
	"github.com/isyll/go-grpc-starter/pkg/idenc"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/emptypb"
)

type AuthServer struct {
	authv1.UnimplementedAuthServiceServer
	svc *auth.Service
	enc idenc.IDEncoder
}

func NewAuthServer(svc *auth.Service, enc idenc.IDEncoder) *AuthServer {
	return &AuthServer{svc: svc, enc: enc}
}

func (s *AuthServer) Register(ctx context.Context, req *authv1.RegisterRequest) (*commonv1.TokenPair, error) {
	tokens, err := s.svc.Register(ctx, auth.RegisterInput{
		Email:     req.GetEmail(),
		Password:  req.GetPassword(),
		FirstName: req.GetFirstName(),
		LastName:  req.GetLastName(),
		Device:    deviceInfo(ctx, req.GetDevice()),
	})
	if err != nil {
		return nil, err
	}
	return toProtoTokenPair(tokens, s.enc), nil
}

func (s *AuthServer) Login(ctx context.Context, req *authv1.LoginRequest) (*commonv1.TokenPair, error) {
	tokens, err := s.svc.Login(ctx, auth.LoginInput{
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
		Device:   deviceInfo(ctx, req.GetDevice()),
	})
	if err != nil {
		return nil, err
	}
	return toProtoTokenPair(tokens, s.enc), nil
}

func (s *AuthServer) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*commonv1.TokenPair, error) {
	tokens, err := s.svc.RefreshTokens(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	return toProtoTokenPair(tokens, s.enc), nil
}

func (s *AuthServer) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	token, _ := interceptor.BearerToken(ctx)
	s.svc.Logout(ctx, reqctx.SubjectFrom(ctx).SessionID, token)
	return &emptypb.Empty{}, nil
}

func (s *AuthServer) VerifyEmail(ctx context.Context, req *authv1.VerifyEmailRequest) (*emptypb.Empty, error) {
	if err := s.svc.VerifyEmail(ctx, req.GetToken()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *AuthServer) ResendVerification(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.svc.ResendVerification(ctx, reqctx.SubjectFrom(ctx).UserID); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *AuthServer) RequestPasswordReset(ctx context.Context, req *authv1.RequestPasswordResetRequest) (*emptypb.Empty, error) {
	if err := s.svc.RequestPasswordReset(ctx, req.GetEmail()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *AuthServer) ResetPassword(ctx context.Context, req *authv1.ResetPasswordRequest) (*emptypb.Empty, error) {
	if err := s.svc.ResetPassword(ctx, req.GetToken(), req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *AuthServer) ChangePassword(ctx context.Context, req *authv1.ChangePasswordRequest) (*emptypb.Empty, error) {
	if err := s.svc.ChangePassword(ctx, reqctx.SubjectFrom(ctx).UserID, req.GetCurrentPassword(), req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *AuthServer) ListDevices(ctx context.Context, _ *emptypb.Empty) (*authv1.ListDevicesResponse, error) {
	devices, err := s.svc.ListDevices(ctx, reqctx.SubjectFrom(ctx).UserID, reqctx.SubjectFrom(ctx).SessionID)
	if err != nil {
		return nil, err
	}
	return &authv1.ListDevicesResponse{Devices: toProtoDevices(devices)}, nil
}

func (s *AuthServer) RevokeDevice(ctx context.Context, req *authv1.RevokeDeviceRequest) (*emptypb.Empty, error) {
	if err := s.svc.RemoveDevice(ctx, reqctx.SubjectFrom(ctx).UserID, req.GetDeviceId(), reqctx.SubjectFrom(ctx).SessionID); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func deviceInfo(ctx context.Context, d *commonv1.DeviceInfo) auth.DeviceInfo {
	info := auth.DeviceInfo{
		IPAddress: clientIP(ctx),
		UserAgent: userAgent(ctx),
	}
	if d != nil {
		info.DeviceID = d.GetDeviceId()
		info.Name = d.GetName()
		info.Platform = d.GetPlatform()
		info.Model = d.GetModel()
		info.Manufacturer = d.GetManufacturer()
	}
	return info
}

func clientIP(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}

func userAgent(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("user-agent"); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
