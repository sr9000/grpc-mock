package service

import (
	"context"
	"errors"
	deprecatedmodelspb "grpc-mock/internal/genproto/complex/deprecatedmodels"
	importmepb "grpc-mock/internal/genproto/complex/importme"
	modelspb "grpc-mock/internal/genproto/complex/models"
	servicepb "grpc-mock/internal/genproto/complex/service"
	"grpc-mock/pkg/observability"
	"grpc-mock/pkg/ptrtools"
	"strings"
	"time"

	"github.com/rs/zerolog"
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
		logger := observability.Logger(ctx, zerolog.Nop())
		logger.Info().
			Str("stub", "ComplexServer").
			Str("method", "GetModel").
			Interface("request", req).
			Msg("stub called")
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
		logger := observability.Logger(ctx, zerolog.Nop())
		logger.Info().
			Str("stub", "ComplexServer").
			Str("method", "GetOldModel").
			Interface("request", req).
			Msg("stub called")
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
		logger := observability.Logger(ctx, zerolog.Nop())
		logger.Info().
			Str("stub", "ComplexServer").
			Str("method", "DoNothing").
			Interface("request", req).
			Msg("stub called")
	}
	return &importmepb.NothingOut{}, nil
}
