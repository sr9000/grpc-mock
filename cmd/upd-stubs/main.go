package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
	goimports "golang.org/x/tools/imports"
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

func main() {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:  ".",
	}
	pkgs, err := packages.Load(cfg, genprotoPath+"/...")
	if err != nil {
		log.Fatalf("failed to load packages: %v", err)
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

	// Group by output package (derived from genproto subpath)
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

	// Generate stubs
	for _, sp := range pkgMap {
		if err := generateStubPackage(sp.OutDir, sp.PkgName, sp.Services); err != nil {
			log.Fatalf("failed to generate stubs for %s: %v", sp.OutDir, err)
		}
	}

	// Generate wire.go
	if err := generateWireFile(pkgMap); err != nil {
		log.Fatalf("failed to generate wire.go: %v", err)
	}

	log.Println("stubs generated successfully")
}

func generateStubPackage(outDir, pkgName string, services []serviceInfo) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, svc := range services {
		if err := generateStubFile(outDir, pkgName, svc); err != nil {
			return err
		}
	}
	return nil
}

func generateStubFile(outDir, pkgName string, svc serviceInfo) error {
	var buf bytes.Buffer

	// Derive struct name from interface (e.g. EchoServiceServer -> EchoServer)
	structName := strings.TrimSuffix(svc.InterfaceName, "Server") + "Server"
	if strings.HasSuffix(svc.InterfaceName, "ServiceServer") {
		structName = strings.TrimSuffix(svc.InterfaceName, "ServiceServer") + "Server"
	}

	// Derive filename
	fileName := toSnakeCase(structName) + ".go"

	outPath := filepath.Join(outDir, fileName)
	if _, err := os.Stat(outPath); err == nil {
		return updateStubFile(outPath, structName, svc)
	}

	// Create shared imports map for the entire file
	// This ensures consistent aliasing across all methods
	imports := newFileImports(svc.PkgPath)

	// Collect method implementations using shared imports
	var methods []string
	iface := svc.Iface
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if !m.Exported() {
			continue
		}
		sig := m.Type().(*types.Signature)

		methodCode := generateMethod(structName, m.Name(), sig, imports)
		methods = append(methods, methodCode)
	}

	// Get the pb alias for use in struct/constructor
	pbAlias := imports[svc.PkgPath]

	// Write file header
	fmt.Fprintf(&buf, "package %s\n\n", pkgName)

	// Write imports
	fmt.Fprintf(&buf, "import (\n")
	sortedImports := make([]string, 0, len(imports))
	for imp := range imports {
		sortedImports = append(sortedImports, imp)
	}
	sort.Strings(sortedImports)
	for _, imp := range sortedImports {
		alias := imports[imp]
		if alias != "" {
			fmt.Fprintf(&buf, "\t%s %q\n", alias, imp)
		} else {
			fmt.Fprintf(&buf, "\t%q\n", imp)
		}
	}
	fmt.Fprintf(&buf, ")\n\n")

	// Write interface compliance check
	fmt.Fprintf(&buf, "var _ %s.%s = (*%s)(nil)\n\n", pbAlias, svc.InterfaceName, structName)

	// Write struct
	fmt.Fprintf(&buf, "type %s struct {\n", structName)
	fmt.Fprintf(&buf, "\t%s.%s\n", pbAlias, svc.Unimplemented)
	fmt.Fprintf(&buf, "\tEnableLogging bool\n")
	fmt.Fprintf(&buf, "}\n\n")

	// Write constructor
	fmt.Fprintf(&buf, "func New%s(server grpc.ServiceRegistrar, enableLogging bool) *%s {\n", structName, structName)
	fmt.Fprintf(&buf, "\ts := &%s{EnableLogging: enableLogging}\n", structName)
	fmt.Fprintf(&buf, "\t%s.%s(server, s)\n", pbAlias, svc.RegisterFunc)
	fmt.Fprintf(&buf, "\treturn s\n")
	fmt.Fprintf(&buf, "}\n")

	// Write methods
	for _, method := range methods {
		fmt.Fprintf(&buf, "\n%s\n", method)
	}

	// Format
	src, err := format.Source(buf.Bytes())
	if err != nil {
		log.Printf("failed to format %s: %v\nSource:\n%s", fileName, err, buf.String())
		return err
	}

	outPath = filepath.Join(outDir, fileName)
	return os.WriteFile(outPath, src, 0o644)
}

// newFileImports creates a shared imports map for generating a stub file.
// It pre-populates common imports and the genproto package with proper alias.
func newFileImports(genprotoPkgPath string) map[string]string {
	imports := map[string]string{
		"context":                "",
		"log":                    "",
		"google.golang.org/grpc": "",
		"grpc-mock/pkg/ctxkeys":  "",
	}

	// Add genproto package with proper alias
	alias := genprotoPackageAlias(genprotoPkgPath)
	if alias == "" {
		panic("non-genproto packages are not supported yet")
	}
	imports[genprotoPkgPath] = alias

	return imports
}

func generateMethod(structName, methodName string, sig *types.Signature, imports map[string]string) string {
	var buf bytes.Buffer

	// Signature: func (s *StructName) MethodName(ctx context.Context, req *pb.Request) (*pb.Response, error)
	params := sig.Params()
	results := sig.Results()

	fmt.Fprintf(&buf, "func (s *%s) %s(", structName, methodName)

	// Parameters
	var paramList []string
	var logParamNames []string // all argument names (exclude ctx)

	for i := 0; i < params.Len(); i++ {
		p := params.At(i)
		pName := p.Name()
		if pName == "" {
			switch i {
			default:
				pName = fmt.Sprintf("arg%d", i)
			case 0:
				pName = "ctx"
			case 1:
				pName = "req"
			}
		}

		// save argument name (exclude the first one)
		if i > 0 {
			logParamNames = append(logParamNames, pName)
		}

		pType := formatType(p.Type(), imports)
		paramList = append(paramList, fmt.Sprintf("%s %s", pName, pType))
	}
	fmt.Fprintf(&buf, "%s)", strings.Join(paramList, ", "))

	// Results
	if results.Len() > 0 {
		var resultList []string
		for i := 0; i < results.Len(); i++ {
			r := results.At(i)
			rType := formatType(r.Type(), imports)
			resultList = append(resultList, rType)
		}
		if results.Len() == 1 {
			fmt.Fprintf(&buf, " %s", resultList[0])
		} else {
			fmt.Fprintf(&buf, " (%s)", strings.Join(resultList, ", "))
		}
	}

	fmt.Fprintf(&buf, " {\n")

	fmt.Fprintf(&buf, "\tif s.EnableLogging {\n")
	fmt.Fprintf(&buf, "\t\treqID, _ := ctx.Value(ctxkeys.RequestID{}).(string)\n")

	if len(logParamNames) > 0 {
		// making format string like: "%+v, %+v, %+v"
		formatParts := make([]string, len(logParamNames))
		for i := range formatParts {
			formatParts[i] = "%+v"
		}
		formatStr := strings.Join(formatParts, ", ")

		// making argument names list: req, opts, etc
		argsStr := strings.Join(logParamNames, ", ")

		fmt.Fprintf(&buf, "\t\tlog.Printf(\"[req_id=%%s] [%s] stub %s called with: %s\", reqID, %s)\n",
			structName, methodName, formatStr, argsStr)
	} else {
		// if no arguments
		fmt.Fprintf(&buf, "\t\tlog.Printf(\"[req_id=%%s] [%s] stub %s called\", reqID)\n",
			structName, methodName)
	}
	fmt.Fprintf(&buf, "\t}\n")

	// Generate return statement with zero values
	if results.Len() > 0 {
		var zeros []string
		for i := 0; i < results.Len(); i++ {
			r := results.At(i)
			zeros = append(zeros, zeroValue(r.Type(), imports))
		}
		fmt.Fprintf(&buf, "\treturn %s\n", strings.Join(zeros, ", "))
	}

	fmt.Fprintf(&buf, "}")

	return buf.String()
}

func formatType(t types.Type, imports map[string]string) string {
	switch tt := t.(type) {
	default:
		return t.String()
	case *types.Basic:
		return tt.Name()
	case *types.Interface:
		if tt.Empty() {
			return "interface{}"
		}
		return t.String()
	case *types.Pointer:
		return "*" + formatType(tt.Elem(), imports)
	case *types.Slice:
		return "[]" + formatType(tt.Elem(), imports)
	case *types.Map:
		return fmt.Sprintf("map[%s]%s", formatType(tt.Key(), imports), formatType(tt.Elem(), imports))
	case *types.Named:
		obj := tt.Obj()
		pkg := obj.Pkg()
		if pkg == nil {
			return obj.Name()
		}
		pkgPath := pkg.Path()

		// Check if already in imports
		if alias, ok := imports[pkgPath]; ok {
			if alias != "" {
				return alias + "." + obj.Name()
			}

			// Empty alias means use package name
			return pkg.Name() + "." + obj.Name()
		}

		// Check if it's a genproto package - force "pb" suffix
		if alias := genprotoPackageAlias(pkgPath); alias != "" {
			imports[pkgPath] = alias
			return alias + "." + obj.Name()
		}

		// External package - check if we need an alias to avoid conflicts
		pkgName := pkg.Name()
		if alias := resolveConflictingAlias(pkgPath, pkgName, imports); alias != "" {
			imports[pkgPath] = alias
			return alias + "." + obj.Name()
		}

		// add new pkgPath with empty alias (use package name)
		imports[pkgPath] = ""
		return pkgName + "." + obj.Name()
	}
}

// genprotoPackageAlias returns an alias for genproto packages with "pb" suffix.
// Returns empty string if the package is not a genproto package.
func genprotoPackageAlias(pkgPath string) string {
	if !strings.HasPrefix(pkgPath, genprotoPath+"/") && pkgPath != genprotoPath {
		return ""
	}
	// Extract package name from path
	parts := strings.Split(pkgPath, "/")
	pkgName := parts[len(parts)-1]

	// Add "pb" suffix if not already present
	if strings.HasSuffix(pkgName, "pb") {
		return pkgName
	}
	return pkgName + "pb"
}

// resolveConflictingAlias checks if the package name conflicts with existing imports
// and returns a unique alias if needed. Returns empty string if no conflict.
func resolveConflictingAlias(pkgPath, pkgName string, imports map[string]string) string {
	for existingPath, existingAlias := range imports {
		if existingPath == pkgPath {
			continue
		}
		// Determine the effective name used by the existing import
		usedName := existingAlias
		if usedName == "" {
			// Extract package name from path
			parts := strings.Split(existingPath, "/")
			usedName = parts[len(parts)-1]
		}
		if usedName == pkgName {
			// Conflict found - generate unique alias
			return generateUniqueAlias(pkgPath, imports)
		}
	}
	return ""
}

func zeroValue(t types.Type, imports map[string]string) string {
	switch tt := t.(type) {
	case *types.Named:
		// Check if it's error type
		if tt.Obj().Name() == "error" && tt.Obj().Pkg() == nil {
			return "nil"
		}
		return "&" + formatType(t, imports) + "{}"
	case *types.Pointer:
		return "&" + formatType(tt.Elem(), imports) + "{}"
	case *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil"
	case *types.Basic:
		switch tt.Kind() {
		case types.Bool:
			return "false"
		case types.String:
			return `""`
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
			types.Float32, types.Float64, types.Complex64, types.Complex128:
			return "0"
		default:
			return "nil"
		}
	default:
		return "nil"
	}
}

func toSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

// generateUniqueAlias creates a unique import alias from a package path.
// It combines path segments to avoid conflicts with existing imports.
func generateUniqueAlias(pkgPath string, imports map[string]string) string {
	parts := strings.Split(pkgPath, "/")
	// Start with just the package name and add parent segments if needed
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.Join(parts[i:], "")
		// Clean the candidate - remove dots and dashes
		candidate = strings.ReplaceAll(candidate, ".", "")
		candidate = strings.ReplaceAll(candidate, "-", "")
		// Check if this alias is already used
		isUsed := false
		for _, existingAlias := range imports {
			if existingAlias == candidate {
				isUsed = true
				break
			}
		}
		if !isUsed {
			return candidate
		}
	}
	// Fallback: use entire path with all separators removed
	alias := strings.ReplaceAll(pkgPath, "/", "")
	alias = strings.ReplaceAll(alias, ".", "")
	alias = strings.ReplaceAll(alias, "-", "")
	return alias
}

func toPascalCase(s string) string {
	var result []rune
	capitalizeNext := true
	for _, r := range s {
		if r == '_' || r == '-' {
			capitalizeNext = true
			continue
		}
		if capitalizeNext {
			result = append(result, unicode.ToUpper(r))
			capitalizeNext = false
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

func generateWireFile(pkgMap map[string]*stubPkg) error {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "//go:build wireinject\n")
	fmt.Fprintf(&buf, "// +build wireinject\n\n")
	fmt.Fprintf(&buf, "package app\n\n")

	fmt.Fprintf(&buf, "import (\n")
	fmt.Fprintf(&buf, "\t\"github.com/google/wire\"\n")
	fmt.Fprintf(&buf, "\t\"google.golang.org/grpc\"\n\n")

	// Collect all packages and servers
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
		// Import paths always use forward slashes, regardless of OS
		importPath := "grpc-mock/" + filepath.ToSlash(sp.OutDir)

		rel := strings.TrimPrefix(sp.OutDir, stubsOutDir+string(os.PathSeparator))
		// Use forward slashes for consistent alias generation
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		alias := strings.Join(parts, "") + "stub"

		var servers []string
		for _, svc := range sp.Services {
			structName := strings.TrimSuffix(svc.InterfaceName, "Server") + "Server"
			if strings.HasSuffix(svc.InterfaceName, "ServiceServer") {
				structName = strings.TrimSuffix(svc.InterfaceName, "ServiceServer") + "Server"
			}
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

	// Generate App struct - use package prefix for field names to avoid duplicates
	fmt.Fprintf(&buf, "type App struct {\n")
	for _, stub := range stubs {
		for _, server := range stub.Servers {
			// Use PkgName + ServerName (without Server suffix) as field name
			pkgPrefix := toPascalCase(stub.Alias[:len(stub.Alias)-4])
			serverBase := strings.TrimSuffix(server, "Server")
			// Avoid redundant names like EchoEcho or StoreStore
			fieldName := pkgPrefix + serverBase
			if strings.EqualFold(pkgPrefix, serverBase) {
				fieldName = pkgPrefix
			}
			fmt.Fprintf(&buf, "\t%s *%s.%s\n", fieldName, stub.Alias, server)
		}
	}
	fmt.Fprintf(&buf, "}\n\n")

	// Generate ProviderSet
	fmt.Fprintf(&buf, "var ProviderSet = wire.NewSet(\n")
	for _, stub := range stubs {
		for _, server := range stub.Servers {
			fmt.Fprintf(&buf, "\t%s.New%s,\n", stub.Alias, server)
		}
	}
	fmt.Fprintf(&buf, "\twire.Struct(new(App), \"*\"),\n")
	fmt.Fprintf(&buf, ")\n\n")

	// Generate InitializeApp
	fmt.Fprintf(&buf, "func InitializeApp(server grpc.ServiceRegistrar, enableLogging bool) (*App, error) {\n")
	fmt.Fprintf(&buf, "\twire.Build(ProviderSet)\n")
	fmt.Fprintf(&buf, "\treturn nil, nil\n")
	fmt.Fprintf(&buf, "}\n")

	src, err := format.Source(buf.Bytes())
	if err != nil {
		log.Printf("failed to format wire.go: %v\nSource:\n%s", err, buf.String())
		return err
	}

	return os.WriteFile("internal/app/wire.go", src, 0o644)
}

func updateStubFile(path, structName string, svc serviceInfo) error {
	originalSrc, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, originalSrc, parser.ParseComments)
	if err != nil {
		return err
	}

	// Check if struct and constructor need update
	src, err := updateStructAndConstructor(originalSrc, fset, node, structName)
	if err != nil {
		return err
	}

	structUpdated := !bytes.Equal(src, originalSrc)

	// Re-parse after potential updates
	fset = token.NewFileSet()
	node, err = parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return err
	}

	existingMethods := make(map[string]*ast.FuncDecl)
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				recvType := fn.Recv.List[0].Type
				if isReceiver(recvType, structName) {
					existingMethods[fn.Name.Name] = fn
				}
			}
		}
	}

	pbAlias := ""
	for _, imp := range node.Imports {
		impPath := strings.Trim(imp.Path.Value, "\"")
		if impPath == svc.PkgPath {
			if imp.Name != nil {
				pbAlias = imp.Name.Name
			} else {
				pbAlias = svc.PkgName
			}
			break
		}
	}

	if pbAlias == "" {
		// Use genprotoPackageAlias for consistent alias derivation
		pbAlias = genprotoPackageAlias(svc.PkgPath)
		if pbAlias == "" {
			panic("non-genproto packages are not supported yet")
		}
	}

	// Create shared imports map for new methods (ensures consistent aliasing)
	fileImports := map[string]string{
		svc.PkgPath: pbAlias,
	}

	type replacement struct {
		start, end int
		content    string
	}
	var replacements []replacement

	var newMethods []string
	iface := svc.Iface
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if !m.Exported() {
			continue
		}
		sig := m.Type().(*types.Signature)

		if existingFn, ok := existingMethods[m.Name()]; ok {
			if signaturesMatch(existingFn, sig, fileImports, fset) {
				continue
			}
			log.Printf("Method %s signature mismatch, fixing...", m.Name())
			newMethod := generateMethodWithExistingBody(structName, m.Name(), sig, fileImports, existingFn, src, fset)
			replacements = append(replacements, replacement{
				start:   fset.Position(existingFn.Pos()).Offset,
				end:     fset.Position(existingFn.End()).Offset,
				content: newMethod,
			})
			continue
		}

		code := generateMethod(structName, m.Name(), sig, fileImports)
		newMethods = append(newMethods, code)
	}

	// Check if imports need updating (missing imports, wrong aliases, or duplicates)
	importsNeedUpdate := false
	seenImports := make(map[string]bool)
	for _, imp := range node.Imports {
		impPath := strings.Trim(imp.Path.Value, "\"")
		if seenImports[impPath] {
			// Duplicate import found
			importsNeedUpdate = true
			break
		}
		seenImports[impPath] = true
	}
	if !importsNeedUpdate {
		for path, requiredAlias := range fileImports {
			found := false
			for _, imp := range node.Imports {
				impPath := strings.Trim(imp.Path.Value, "\"")
				if impPath == path {
					impAlias := ""
					if imp.Name != nil {
						impAlias = imp.Name.Name
					}
					if impAlias == requiredAlias {
						found = true
					}
					break
				}
			}
			if !found {
				importsNeedUpdate = true
				break
			}
		}
	}

	if len(newMethods) == 0 && len(replacements) == 0 && !structUpdated && !importsNeedUpdate {
		return nil
	}

	// Apply replacements (method updates)
	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].start > replacements[j].start
	})

	newSrc := src
	for _, r := range replacements {
		newSrc = append(newSrc[:r.start], append([]byte(r.content), newSrc[r.end:]...)...)
	}

	var buf bytes.Buffer
	buf.Write(newSrc)
	for _, method := range newMethods {
		buf.WriteString("\n\n")
		buf.WriteString(method)
	}

	// Re-parse to patch imports properly
	updatedSrc := buf.Bytes()
	fset = token.NewFileSet()
	node, err = parser.ParseFile(fset, path, updatedSrc, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to re-parse after updates: %w", err)
	}

	// Collect existing imports (path -> alias)
	existingImports := make(map[string]string)
	for _, imp := range node.Imports {
		impPath := strings.Trim(imp.Path.Value, "\"")
		impAlias := ""
		if imp.Name != nil {
			impAlias = imp.Name.Name
		}
		existingImports[impPath] = impAlias
	}

	// Merge required imports (fileImports) into existingImports
	// fileImports takes precedence for aliases (they have the correct "pb" suffix)
	for path, alias := range fileImports {
		existingImports[path] = alias
	}

	// Build new import section
	var importBuf bytes.Buffer
	importBuf.WriteString("import (\n")

	sortedImports := make([]string, 0, len(existingImports))
	for imp := range existingImports {
		sortedImports = append(sortedImports, imp)
	}
	sort.Strings(sortedImports)

	for _, imp := range sortedImports {
		alias := existingImports[imp]
		if alias != "" {
			fmt.Fprintf(&importBuf, "\t%s %q\n", alias, imp)
		} else {
			fmt.Fprintf(&importBuf, "\t%q\n", imp)
		}
	}
	importBuf.WriteString(")\n")

	// Find import section boundaries and replace
	var importStart, importEnd int
	for _, decl := range node.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			if importStart == 0 || fset.Position(gen.Pos()).Offset < importStart {
				importStart = fset.Position(gen.Pos()).Offset
			}
			if fset.Position(gen.End()).Offset > importEnd {
				importEnd = fset.Position(gen.End()).Offset
			}
		}
	}

	var finalSrc []byte
	if importStart > 0 && importEnd > 0 {
		// Replace existing import section(s)
		finalSrc = append(finalSrc, updatedSrc[:importStart]...)
		finalSrc = append(finalSrc, importBuf.Bytes()...)
		finalSrc = append(finalSrc, updatedSrc[importEnd:]...)
	} else {
		// No imports exist - insert after package declaration
		pkgEnd := fset.Position(node.Name.End()).Offset
		finalSrc = append(finalSrc, updatedSrc[:pkgEnd]...)
		finalSrc = append(finalSrc, '\n', '\n')
		finalSrc = append(finalSrc, importBuf.Bytes()...)
		finalSrc = append(finalSrc, updatedSrc[pkgEnd:]...)
	}

	// Format with goimports for final cleanup
	res, err := goimports.Process(path, finalSrc, nil)
	if err != nil {
		return fmt.Errorf("failed to process imports: %w", err)
	}

	return os.WriteFile(path, res, 0o644)
}

func updateStructAndConstructor(src []byte, fset *token.FileSet, node *ast.File, structName string) ([]byte, error) {
	var replacements []struct {
		start, end int
		content    string
	}

	for _, decl := range node.Decls {
		// Check for struct
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			for _, spec := range gen.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == structName {
					if st, ok := ts.Type.(*ast.StructType); ok {
						hasField := false
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								if name.Name == "EnableLogging" {
									hasField = true
								}
							}
						}
						if !hasField {
							closingBrace := fset.Position(st.Fields.Closing).Offset
							replacements = append(replacements, struct {
								start, end int
								content    string
							}{
								start:   closingBrace,
								end:     closingBrace,
								content: "\tEnableLogging bool\n",
							})
						}
					}
				}
			}
		}

		// Check for constructor
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "New"+structName {
			hasParam := false
			for _, param := range fn.Type.Params.List {
				for _, name := range param.Names {
					if name.Name == "enableLogging" {
						hasParam = true
					}
				}
			}

			if !hasParam {
				// Update signature
				closingParen := fset.Position(fn.Type.Params.Closing).Offset
				replacements = append(replacements, struct {
					start, end int
					content    string
				}{
					start:   closingParen,
					end:     closingParen,
					content: ", enableLogging bool",
				})

				// Update body initialization
				// Look for &StructName{} or &StructName{...}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if ue, ok := n.(*ast.UnaryExpr); ok && ue.Op == token.AND {
						if cl, ok := ue.X.(*ast.CompositeLit); ok {
							if ident, ok := cl.Type.(*ast.Ident); ok && ident.Name == structName {
								// Found &StructName{...}
								// Insert EnableLogging: enableLogging
								closingBrace := fset.Position(cl.Rbrace).Offset
								content := "EnableLogging: enableLogging"
								if len(cl.Elts) > 0 {
									content = ", " + content
								}
								replacements = append(replacements, struct {
									start, end int
									content    string
								}{
									start:   closingBrace,
									end:     closingBrace,
									content: content,
								})
								return false
							}
						}
					}
					return true
				})
			}
		}
	}

	if len(replacements) == 0 {
		return src, nil
	}

	// Apply replacements in reverse order
	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].start > replacements[j].start
	})

	newSrc := src
	for _, r := range replacements {
		newSrc = append(newSrc[:r.start], append([]byte(r.content), newSrc[r.end:]...)...)
	}

	return newSrc, nil
}

func isReceiver(expr ast.Expr, structName string) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return isReceiver(t.X, structName)
	case *ast.Ident:
		return t.Name == structName
	default:
		return false
	}
}

func signaturesMatch(fn *ast.FuncDecl, sig *types.Signature, imports map[string]string, fset *token.FileSet) bool {
	params := fn.Type.Params.List
	expectedParams := sig.Params()

	var astParamTypes []string
	for _, field := range params {
		typeStr := typeToString(field.Type, fset)
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for k := 0; k < count; k++ {
			astParamTypes = append(astParamTypes, typeStr)
		}
	}

	if len(astParamTypes) != expectedParams.Len() {
		return false
	}

	// Use provided imports map for consistent type formatting
	for i := 0; i < expectedParams.Len(); i++ {
		expectedType := formatType(expectedParams.At(i).Type(), imports)
		if removeWhitespace(expectedType) != removeWhitespace(astParamTypes[i]) {
			return false
		}
	}

	results := fn.Type.Results
	expectedResults := sig.Results()
	var astResultTypes []string
	if results != nil {
		for _, field := range results.List {
			typeStr := typeToString(field.Type, fset)
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for k := 0; k < count; k++ {
				astResultTypes = append(astResultTypes, typeStr)
			}
		}
	}

	if len(astResultTypes) != expectedResults.Len() {
		return false
	}

	for i := 0; i < expectedResults.Len(); i++ {
		expectedType := formatType(expectedResults.At(i).Type(), imports)
		if removeWhitespace(expectedType) != removeWhitespace(astResultTypes[i]) {
			return false
		}
	}

	return true
}

func generateMethodWithExistingBody(structName, methodName string, sig *types.Signature, imports map[string]string, existingFn *ast.FuncDecl, src []byte, fset *token.FileSet) string {
	// Capture previous signature
	startOffset := fset.Position(existingFn.Pos()).Offset
	bodyStartOffset := fset.Position(existingFn.Body.Pos()).Offset
	prevSig := string(src[startOffset:bodyStartOffset])
	prevSig = strings.TrimSpace(prevSig)

	// Extract existing parameters for matching
	type existingParam struct {
		Name    string
		TypeStr string
		Used    bool
	}
	var existingParams []*existingParam

	for _, field := range existingFn.Type.Params.List {
		typeStr := removeWhitespace(typeToString(field.Type, fset))
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				existingParams = append(existingParams, &existingParam{
					Name:    name.Name,
					TypeStr: typeStr,
				})
			}
		} else {
			existingParams = append(existingParams, &existingParam{
				Name:    "",
				TypeStr: typeStr,
			})
		}
	}

	// Determine new parameter names by matching types
	paramNames := make([]string, sig.Params().Len())

	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		newParamType := params.At(i).Type()
		newParamTypeStr := removeWhitespace(formatType(newParamType, imports))

		// Find match
		for _, ep := range existingParams {
			if !ep.Used && ep.TypeStr == newParamTypeStr {
				if ep.Name != "" {
					paramNames[i] = ep.Name
				}
				ep.Used = true
				break
			}
		}
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "// Previous signature: %s\n", prevSig)
	fmt.Fprintf(&buf, "func (s *%s) %s(", structName, methodName)

	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			buf.WriteString(", ")
		}
		pName := paramNames[i]
		if pName == "" {
			switch i {
			default:
				pName = fmt.Sprintf("arg%d", i)
			case 0:
				pName = "ctx"
			case 1:
				pName = "req"
			}
		}
		pType := formatType(params.At(i).Type(), imports)
		fmt.Fprintf(&buf, "%s %s", pName, pType)
	}
	buf.WriteString(")")

	results := sig.Results()
	if results.Len() > 0 {
		var resultList []string
		for i := 0; i < results.Len(); i++ {
			r := results.At(i)
			rType := formatType(r.Type(), imports)
			resultList = append(resultList, rType)
		}
		if results.Len() == 1 {
			fmt.Fprintf(&buf, " %s", resultList[0])
		} else {
			fmt.Fprintf(&buf, " (%s)", strings.Join(resultList, ", "))
		}
	}

	buf.WriteString(" ")

	bodyStart := fset.Position(existingFn.Body.Pos()).Offset
	bodyEnd := fset.Position(existingFn.Body.End()).Offset
	body := string(src[bodyStart:bodyEnd])
	buf.WriteString(body)

	return buf.String()
}

func typeToString(expr ast.Expr, fset *token.FileSet) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

func removeWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
