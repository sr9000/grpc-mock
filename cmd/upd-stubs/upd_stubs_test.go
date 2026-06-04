package main

import (
	"testing"
)

func TestDeriveStructName(t *testing.T) {
	tests := []struct {
		iface string
		want  string
	}{
		{"EchoServiceServer", "EchoServer"},
		{"ComplexServiceServer", "ComplexServer"},
		{"FooServer", "FooServer"},
		{"BarServiceServer", "BarServer"},
	}
	for _, tt := range tests {
		got := deriveStructName(tt.iface)
		if got != tt.want {
			t.Errorf("deriveStructName(%q) = %q, want %q", tt.iface, got, tt.want)
		}
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"EchoServer", "echo_server"},
		{"ComplexServer", "complex_server"},
		{"HTTPHandler", "h_t_t_p_handler"},
		{"Simple", "simple"},
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.in)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"echo_stub", "EchoStub"},
		{"complex_service_stub", "ComplexServiceStub"},
		{"simple", "Simple"},
		{"kebab-case-test", "KebabCaseTest"},
	}
	for _, tt := range tests {
		got := toPascalCase(tt.in)
		if got != tt.want {
			t.Errorf("toPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGenprotoPackageAlias(t *testing.T) {
	tests := []struct {
		pkgPath string
		want    string
	}{
		{"grpc-mock/internal/genproto/echo", "echopb"},
		{"grpc-mock/internal/genproto/complex/service", "servicepb"},
		{"grpc-mock/internal/genproto", "genprotopb"},
		{"other/module/something", ""},
	}
	for _, tt := range tests {
		got := genprotoPackageAlias(tt.pkgPath)
		if got != tt.want {
			t.Errorf("genprotoPackageAlias(%q) = %q, want %q", tt.pkgPath, got, tt.want)
		}
	}
}

func TestRemoveWhitespace(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello world", "helloworld"},
		{"  foo  bar  ", "foobar"},
		{"nochange", "nochange"},
	}
	for _, tt := range tests {
		got := removeWhitespace(tt.in)
		if got != tt.want {
			t.Errorf("removeWhitespace(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveConflictingAlias_NoConflict(t *testing.T) {
	imports := map[string]string{
		"context":                "",
		"google.golang.org/grpc": "",
	}
	alias := resolveConflictingAlias("grpc-mock/internal/genproto/echo", "echo", imports)
	if alias != "" {
		t.Logf("resolveConflictingAlias returned %q for non-conflicting case", alias)
	}
}

func TestResolveConflictingAlias_WithConflict(t *testing.T) {
	imports := map[string]string{
		"context":                "",
		"google.golang.org/grpc": "",
		"some/other/echo":        "echo",
	}
	alias := resolveConflictingAlias("grpc-mock/internal/genproto/echo", "echo", imports)
	if alias == "" {
		t.Fatalf("expected a non-empty alias when there is a conflict")
	}
	if imports["some/other/echo"] == alias {
		t.Fatalf("generated alias conflicts with existing import")
	}
}

func TestGenerateUniqueAlias(t *testing.T) {
	imports := map[string]string{
		"context": "",
		"log":     "",
	}
	alias := generateUniqueAlias("grpc-mock/pkg/ctxkeys", imports)
	if alias == "" {
		t.Fatalf("expected non-empty alias")
	}
	for _, existing := range imports {
		if existing == alias {
			t.Fatalf("generated alias conflicts with existing")
		}
	}
}

func TestSignaturesMatch_SameSignature(t *testing.T) {
	a := removeWhitespace("context.Context, *echopb.EchoRequest")
	b := removeWhitespace("context.Context, *echopb.EchoRequest")
	if a != b {
		t.Fatalf("identical signatures should match after whitespace removal")
	}
}

func TestSignaturesMatch_DifferentSignature(t *testing.T) {
	a := removeWhitespace("context.Context, *echopb.EchoRequest")
	b := removeWhitespace("context.Context, *echopb.NewRequest")
	if a == b {
		t.Fatalf("different signatures should not match after whitespace removal")
	}
}

func TestGroupServicesByPkg(t *testing.T) {
	services := []serviceInfo{
		{PkgPath: "grpc-mock/internal/genproto/echo", PkgName: "echo", InterfaceName: "EchoServiceServer"},
		{PkgPath: "grpc-mock/internal/genproto/complex/service", PkgName: "service", InterfaceName: "ComplexServiceServer"},
	}
	pkgMap := groupServicesByPkg(services)
	if len(pkgMap) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgMap))
	}
	echoPkg, exists := pkgMap["internal/stubs/echo"]
	if !exists {
		t.Fatal("expected internal/stubs/echo to exist")
	}
	if echoPkg.PkgName != "echo" {
		t.Fatalf("expected PkgName echo, got %q", echoPkg.PkgName)
	}
	complexPkg, exists := pkgMap["internal/stubs/complex/service"]
	if !exists {
		t.Fatal("expected internal/stubs/complex/service to exist")
	}
	if complexPkg.PkgName != "service" {
		t.Fatalf("expected PkgName service, got %q", complexPkg.PkgName)
	}
}

func TestNewFileImports(t *testing.T) {
	imports := newFileImports("grpc-mock/internal/genproto/echo")
	if _, exists := imports["context"]; !exists {
		t.Fatal("expected context import")
	}
	if _, exists := imports["grpc-mock/pkg/observability"]; !exists {
		t.Fatal("expected observability import")
	}
	if _, exists := imports["github.com/rs/zerolog"]; !exists {
		t.Fatal("expected zerolog import")
	}
	if _, exists := imports["google.golang.org/grpc"]; !exists {
		t.Fatal("expected grpc import")
	}
	if alias, exists := imports["grpc-mock/internal/genproto/echo"]; !exists {
		t.Fatal("expected genproto/echo import")
	} else if alias != "echopb" {
		t.Fatalf("expected alias echopb, got %q", alias)
	}
}
