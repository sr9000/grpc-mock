//go:build wireinject
// +build wireinject

package app

import (
	"github.com/google/wire"
	"google.golang.org/grpc"

	echostub "grpc-mock/internal/stubs/echo"
)

type App struct {
	Echo *echostub.EchoServer
}

var ProviderSet = wire.NewSet(
	echostub.NewEchoServer,
	wire.Struct(new(App), "*"),
)

func InitializeApp(server grpc.ServiceRegistrar, enableLogging bool) (*App, error) {
	wire.Build(ProviderSet)
	return nil, nil
}
