//go:build wireinject
// +build wireinject

package app

import (
	"github.com/google/wire"
	"google.golang.org/grpc"

	complexservicestub "grpc-mock/internal/stubs/complex/service"
	echostub "grpc-mock/internal/stubs/echo"
)

type App struct {
	ComplexserviceComplex *complexservicestub.ComplexServer
	Echo                  *echostub.EchoServer
}

var ProviderSet = wire.NewSet(
	complexservicestub.NewComplexServer,
	echostub.NewEchoServer,
	wire.Struct(new(App), "*"),
)

func InitializeApp(server grpc.ServiceRegistrar, enableLogging bool) (*App, error) {
	wire.Build(ProviderSet)
	return nil, nil
}
