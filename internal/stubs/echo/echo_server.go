package echo

import (
	"context"
	echopb "grpc-mock/internal/genproto/echo"
	"grpc-mock/pkg/ctxkeys"
	"log"

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
		reqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)
		log.Printf("[req_id=%s] [EchoServer] stub Echo called with: %+v", reqID, myargRequest)
	}

	return &echopb.EchoResponse{
		Message: myargRequest.Message,
	}, nil
}
