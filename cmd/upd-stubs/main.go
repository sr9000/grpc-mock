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
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
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

	// Derive alias for import
	importAlias := svc.PkgName + "pb"
	if strings.HasSuffix(svc.PkgName, "pb") {
		importAlias = svc.PkgName
	}

	// Collect imports needed
	imports := map[string]string{
		"google.golang.org/grpc": "",
		svc.PkgPath:              importAlias,
		"log":                    "",
	}

	// Check if we need context (we always do for methods)
	needsContext := false

	// Collect method implementations
	var methods []string
	iface := svc.Iface
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if !m.Exported() {
			continue
		}
		sig := m.Type().(*types.Signature)

		// Build method signature
		methodCode, methodImports := generateMethod(structName, m.Name(), sig, importAlias, imports)
		methods = append(methods, methodCode)
		for imp, alias := range methodImports {
			imports[imp] = alias
			if imp == "context" {
				needsContext = true
			}
		}
	}

	_ = needsContext

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
	fmt.Fprintf(&buf, "var _ %s.%s = (*%s)(nil)\n\n", importAlias, svc.InterfaceName, structName)

	// Write struct
	fmt.Fprintf(&buf, "type %s struct {\n", structName)
	fmt.Fprintf(&buf, "\t%s.%s\n", importAlias, svc.Unimplemented)
	fmt.Fprintf(&buf, "\tEnableLogging bool\n")
	fmt.Fprintf(&buf, "}\n\n")

	// Write constructor
	fmt.Fprintf(&buf, "func New%s(server grpc.ServiceRegistrar, enableLogging bool) *%s {\n", structName, structName)
	fmt.Fprintf(&buf, "\ts := &%s{EnableLogging: enableLogging}\n", structName)
	fmt.Fprintf(&buf, "\t%s.%s(server, s)\n", importAlias, svc.RegisterFunc)
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

func generateMethod(structName, methodName string, sig *types.Signature, pbAlias string, initImports map[string]string) (string, map[string]string) {
	stubImports := make(map[string]string)
	stubImports["context"] = ""
	stubImports["log"] = ""
	stubImports["grpc-mock/pkg/ctxkeys"] = ""

	maps.Copy(stubImports, initImports)

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

		pType := formatType(p.Type(), stubImports)
		paramList = append(paramList, fmt.Sprintf("%s %s", pName, pType))
	}
	fmt.Fprintf(&buf, "%s)", strings.Join(paramList, ", "))

	// Results
	if results.Len() > 0 {
		var resultList []string
		for i := 0; i < results.Len(); i++ {
			r := results.At(i)
			rType := formatType(r.Type(), stubImports)
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
			zeros = append(zeros, zeroValue(r.Type(), pbAlias, stubImports))
		}
		fmt.Fprintf(&buf, "\treturn %s\n", strings.Join(zeros, ", "))
	}

	fmt.Fprintf(&buf, "}")

	return buf.String(), stubImports
}

func formatType(t types.Type, imports map[string]string) string {
	switch tt := t.(type) {
	case *types.Named:
		obj := tt.Obj()
		pkg := obj.Pkg()
		if pkg == nil {
			return obj.Name()
		}
		// Check if it's from our pb package
		pkgPath := pkg.Path()
		if alias, ok := imports[pkgPath]; ok && alias != "" {
			return alias + "." + obj.Name()
		}
		// External package
		alias := pkg.Name()
		imports[pkgPath] = ""
		return alias + "." + obj.Name()
	case *types.Pointer:
		return "*" + formatType(tt.Elem(), imports)
	case *types.Slice:
		return "[]" + formatType(tt.Elem(), imports)
	case *types.Map:
		return fmt.Sprintf("map[%s]%s", formatType(tt.Key(), imports), formatType(tt.Elem(), imports))
	case *types.Interface:
		if tt.Empty() {
			return "interface{}"
		}
		return t.String()
	case *types.Basic:
		return tt.Name()
	default:
		return t.String()
	}
}

func zeroValue(t types.Type, pbAlias string, imports map[string]string) string {
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
		importPath := "grpc-mock/" + sp.OutDir

		rel := strings.TrimPrefix(sp.OutDir, stubsOutDir+string(os.PathSeparator))
		parts := strings.Split(rel, string(os.PathSeparator))
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
		pbAlias = svc.PkgName + "pb"
		if strings.HasSuffix(svc.PkgName, "pb") {
			pbAlias = svc.PkgName
		}
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
			if signaturesMatch(existingFn, sig, pbAlias, fset) {
				continue
			}
			log.Printf("Method %s signature mismatch, fixing...", m.Name())
			newMethod := generateMethodWithExistingBody(structName, m.Name(), sig, pbAlias, existingFn, src, fset)
			replacements = append(replacements, replacement{
				start:   fset.Position(existingFn.Pos()).Offset,
				end:     fset.Position(existingFn.End()).Offset,
				content: newMethod,
			})
			continue
		}

		code, _ := generateMethod(structName, m.Name(), sig, pbAlias, nil)
		newMethods = append(newMethods, code)
	}

	if len(newMethods) == 0 && len(replacements) == 0 && !structUpdated {
		return nil
	}

	// Apply replacements
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

	res, err := imports.Process(path, buf.Bytes(), nil)
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

func signaturesMatch(fn *ast.FuncDecl, sig *types.Signature, pbAlias string, fset *token.FileSet) bool {
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

	dummyImports := make(map[string]string)
	for i := 0; i < expectedParams.Len(); i++ {
		expectedType := formatType(expectedParams.At(i).Type(), dummyImports)
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
		expectedType := formatType(expectedResults.At(i).Type(), dummyImports)
		if removeWhitespace(expectedType) != removeWhitespace(astResultTypes[i]) {
			return false
		}
	}

	return true
}

func generateMethodWithExistingBody(structName, methodName string, sig *types.Signature, pbAlias string, existingFn *ast.FuncDecl, src []byte, fset *token.FileSet) string {
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

	// Determine new parameter names
	paramNames := make([]string, sig.Params().Len())
	dummyImports := make(map[string]string)

	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		newParamType := params.At(i).Type()
		newParamTypeStr := removeWhitespace(formatType(newParamType, dummyImports))

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
	imports := make(map[string]string)

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
