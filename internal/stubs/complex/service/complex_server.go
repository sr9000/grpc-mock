package service

import (
	"context"
	"errors"
	deprecatedmodelspb "grpc-mock/internal/genproto/complex/deprecatedmodels"
	importmepb "grpc-mock/internal/genproto/complex/importme"
	modelspb "grpc-mock/internal/genproto/complex/models"
	servicepb "grpc-mock/internal/genproto/complex/service"
	"grpc-mock/pkg/ctxkeys"
	"grpc-mock/pkg/ptrtools"
	"log"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/type/date"
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

	if req.Data == nil {
		return &modelspb.ServiceModel{CreatedAt: ptrtools.From(time2date(time.Now()))}, nil
	} else if strings.HasPrefix(req.Data.Value, "error: ") {
		return nil, errors.New(strings.TrimPrefix(req.Data.Value, "error: "))
	} else if strings.HasPrefix(req.Data.Value, "panic: ") {
		panic(strings.TrimPrefix(req.Data.Value, "panic: "))
	} else {
		return &modelspb.ServiceModel{
			CreatedAt: ptrtools.From(time2date(time.Now())),
			Data: &importmepb.ImportedData{
				Value: "echo: " + req.Data.Value,
			},
		}, nil
	}
}

func (s *ComplexServer) GetOldModel(ctx context.Context, req *deprecatedmodelspb.ServiceModel) (*deprecatedmodelspb.ServiceModel, error) {
	if s.EnableLogging {
		reqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)
		log.Printf("[req_id=%s] [ComplexServer] stub GetOldModel called with: %+v", reqID, req)
	}

	if req.Data == nil {
		return &deprecatedmodelspb.ServiceModel{CreatedAt: ptrtools.From(time2date(time.Now()))}, nil
	} else if strings.HasPrefix(req.Data.Value, "error: ") {
		return nil, errors.New(strings.TrimPrefix(req.Data.Value, "error: "))
	} else if strings.HasPrefix(req.Data.Value, "panic: ") {
		panic(strings.TrimPrefix(req.Data.Value, "panic: "))
	} else {
		return &deprecatedmodelspb.ServiceModel{
			CreatedAt: ptrtools.From(time2date(time.Now())),
			Data: &importmepb.ImportedData{
				Value: "echo: " + req.Data.Value,
			},
		}, nil
	}
}

func time2date(t time.Time) date.Date {
	return date.Date{
		Year:  int32(t.Year()),
		Month: int32(t.Month()),
		Day:   int32(t.Day()),
	}
}

func (s *ComplexServer) DoNothing(ctx context.Context, req *importmepb.NothingIn) (*importmepb.NothingOut, error) {
	if s.EnableLogging {
		reqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)
		log.Printf("[req_id=%s] [ComplexServer] stub DoNothing called with: %+v", reqID, req)
	}
	return &importmepb.NothingOut{}, nil
}
