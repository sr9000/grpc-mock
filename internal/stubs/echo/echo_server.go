package echo

import (
	"context"
	echopb "grpc-mock/internal/genproto/echo"
	"grpc-mock/pkg/observability"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

var _ echopb.EchoServiceServer = (*EchoServer)(nil)

type EchoServer struct {
	echopb.UnimplementedEchoServiceServer
	EnableLogging bool
}

func NewEchoServer(server grpc.ServiceRegistrar, enableLogging bool) *EchoServer {
	s := &EchoServer{EnableLogging: enableLogging}
	echopb.RegisterEchoServiceServer(server, s)
	return s
}

func (s *EchoServer) Echo(ctx context.Context, myargRequest *echopb.EchoRequest) (*echopb.EchoResponse, error) {
	if s.EnableLogging {
		logger := observability.Logger(ctx, zerolog.Nop())
		logger.Info().
			Str("stub", "EchoServer").
			Str("method", "Echo").
			Interface("request", myargRequest).
			Msg("stub called")
	}

	return &echopb.EchoResponse{
		Message: myargRequest.Message,
	}, nil
}
