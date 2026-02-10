package service

import (
	"context"
	deprecatedmodelspb "grpc-mock/internal/genproto/complex/deprecatedmodels"
	importmepb "grpc-mock/internal/genproto/complex/importme"
	modelspb "grpc-mock/internal/genproto/complex/models"
	servicepb "grpc-mock/internal/genproto/complex/service"
	"grpc-mock/pkg/ctxkeys"
	"log"

	"google.golang.org/grpc"
)

var _ servicepb.ComplexServiceServer = (*ComplexServer)(nil)

type ComplexServer struct {
	servicepb.UnimplementedComplexServiceServer
	EnableLogging bool
}

func NewComplexServer(server grpc.ServiceRegistrar, enableLogging bool) *ComplexServer {
	s := &ComplexServer{EnableLogging: enableLogging}
	servicepb.RegisterComplexServiceServer(server, s)
	return s
}

func (s *ComplexServer) GetModel(ctx context.Context, req *modelspb.ServiceModel) (*modelspb.ServiceModel, error) {
	if s.EnableLogging {
		reqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)
		log.Printf("[req_id=%s] [ComplexServer] stub GetModel called with: %+v", reqID, req)
	}
	return &modelspb.ServiceModel{}, nil
}

func (s *ComplexServer) DoNothing(ctx context.Context, req *importmepb.NothingIn) (*importmepb.NothingOut, error) {
	if s.EnableLogging {
		reqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)
		log.Printf("[req_id=%s] [ComplexServer] stub DoNothing called with: %+v", reqID, req)
	}
	return &importmepb.NothingOut{}, nil
}

func (s *ComplexServer) GetOldModel(ctx context.Context, req *deprecatedmodelspb.ServiceModel) (*deprecatedmodelspb.ServiceModel, error) {
	if s.EnableLogging {
		reqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)
		log.Printf("[req_id=%s] [ComplexServer] stub GetOldModel called with: %+v", reqID, req)
	}
	return &deprecatedmodelspb.ServiceModel{}, nil
}
