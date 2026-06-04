package main

import (
	"bytes"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// generateWireFile generates the wire.go file for dependency injection.
func generateWireFile(pkgMap map[string]*stubPkg, dryRun, verbose bool) error {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "//go:build wireinject\n")
	fmt.Fprintf(&buf, "// +build wireinject\n\n")
	fmt.Fprintf(&buf, "package app\n\n")

	fmt.Fprintf(&buf, "import (\n")
	fmt.Fprintf(&buf, "\t\"github.com/google/wire\"\n")
	fmt.Fprintf(&buf, "\t\"google.golang.org/grpc\"\n\n")

	type stubInfo struct {
		ImportPath string
		Alias      string
		Servers    []string
	}
	var stubs []stubInfo

	sortedDirs := make([]string, 0, len(pkgMap))
	for dir := range pkgMap {
		sortedDirs = append(sortedDirs, dir)
	}
	sort.Strings(sortedDirs)

	for _, dir := range sortedDirs {
		sp := pkgMap[dir]
		importPath := "grpc-mock/" + filepath.ToSlash(sp.OutDir)

		rel := strings.TrimPrefix(sp.OutDir, stubsOutDir+string(os.PathSeparator))
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		alias := strings.Join(parts, "") + "stub"

		var servers []string
		for _, svc := range sp.Services {
			structName := deriveStructName(svc.InterfaceName)
			servers = append(servers, structName)
		}

		stubs = append(stubs, stubInfo{
			ImportPath: importPath,
			Alias:      alias,
			Servers:    servers,
		})

		fmt.Fprintf(&buf, "\t%s %q\n", alias, importPath)
	}

	fmt.Fprintf(&buf, ")\n\n")

	fmt.Fprintf(&buf, "type App struct {\n")
	for _, stub := range stubs {
		for _, server := range stub.Servers {
			pkgPrefix := toPascalCase(stub.Alias[:len(stub.Alias)-4])
			serverBase := strings.TrimSuffix(server, "Server")
			fieldName := pkgPrefix + serverBase
			if strings.EqualFold(pkgPrefix, serverBase) {
				fieldName = pkgPrefix
			}
			fmt.Fprintf(&buf, "\t%s *%s.%s\n", fieldName, stub.Alias, server)
		}
	}
	fmt.Fprintf(&buf, "}\n\n")

	fmt.Fprintf(&buf, "var ProviderSet = wire.NewSet(\n")
	for _, stub := range stubs {
		for _, server := range stub.Servers {
			fmt.Fprintf(&buf, "\t%s.New%s,\n", stub.Alias, server)
		}
	}
	fmt.Fprintf(&buf, "\twire.Struct(new(App), \"*\"),\n")
	fmt.Fprintf(&buf, ")\n\n")

	fmt.Fprintf(&buf, "func InitializeApp(server grpc.ServiceRegistrar, enableLogging bool) (*App, error) {\n")
	fmt.Fprintf(&buf, "\twire.Build(ProviderSet)\n")
	fmt.Fprintf(&buf, "\treturn nil, nil\n")
	fmt.Fprintf(&buf, "}\n")

	src, err := format.Source(buf.Bytes())
	if err != nil {
		log.Printf("failed to format wire.go: %v\nSource:\n%s", err, buf.String())
		return err
	}

	if dryRun {
		log.Printf("[dry-run] would write internal/app/wire.go (%d bytes)", len(src))
		return nil
	}
	return os.WriteFile("internal/app/wire.go", src, 0o644)
}
