package main

import (
	"go/types"
)

const (
	genprotoPath = "grpc-mock/internal/genproto"
	stubsOutDir  = "internal/stubs"
)

type serviceInfo struct {
	PkgPath       string
	PkgName       string
	InterfaceName string
	Iface         *types.Interface
	RegisterFunc  string
	Unimplemented string
}

type stubPkg struct {
	OutDir   string
	PkgName  string
	Services []serviceInfo
}
