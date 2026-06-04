package main

import (
	"go/types"
	"log"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// discoverServices loads genproto packages and extracts gRPC service interfaces.
func discoverServices(verbose bool) ([]serviceInfo, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:  ".",
	}
	pkgs, err := packages.Load(cfg, genprotoPath+"/...")
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 {
		log.Fatal("errors in genproto packages")
	}

	var services []serviceInfo

	for _, pkg := range pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			if !strings.HasSuffix(name, "Server") {
				continue
			}
			if strings.HasPrefix(name, "Unimplemented") || strings.HasPrefix(name, "Unsafe") {
				continue
			}
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			iface, ok := obj.Type().Underlying().(*types.Interface)
			if !ok {
				continue
			}

			// Find corresponding Register and Unimplemented
			registerFunc := "Register" + name
			unimplemented := "Unimplemented" + name

			// Check they exist
			if scope.Lookup(registerFunc) == nil || scope.Lookup(unimplemented) == nil {
				continue
			}

			if verbose {
				log.Printf("discovered service: %s in package %s", name, pkg.PkgPath)
			}

			services = append(services, serviceInfo{
				PkgPath:       pkg.PkgPath,
				PkgName:       pkg.Name,
				InterfaceName: name,
				Iface:         iface,
				RegisterFunc:  registerFunc,
				Unimplemented: unimplemented,
			})
		}
	}

	return services, nil
}

// groupServicesByPkg groups discovered services by their output stub package.
func groupServicesByPkg(services []serviceInfo) map[string]*stubPkg {
	pkgMap := make(map[string]*stubPkg)

	for _, svc := range services {
		// Derive output dir from genproto path
		// e.g. grpc-mock/internal/genproto/echo -> internal/stubs/echo
		// e.g. grpc-mock/internal/genproto/store/v1 -> internal/stubs/store/v1
		rel := strings.TrimPrefix(svc.PkgPath, genprotoPath+"/")
		outDir := filepath.Join(stubsOutDir, rel)
		outPkgName := svc.PkgName

		if pkgMap[outDir] == nil {
			pkgMap[outDir] = &stubPkg{
				OutDir:  outDir,
				PkgName: outPkgName,
			}
		}
		pkgMap[outDir].Services = append(pkgMap[outDir].Services, svc)
	}

	return pkgMap
}
