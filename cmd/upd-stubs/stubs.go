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

	goimports "golang.org/x/tools/imports"
)

// generateStubPackage creates or updates all stub files for a package.
func generateStubPackage(outDir, pkgName string, services []serviceInfo, dryRun, verbose bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, svc := range services {
		if err := generateStubFile(outDir, pkgName, svc, dryRun, verbose); err != nil {
			return err
		}
	}
	return nil
}

// generateStubFile creates a new stub file or delegates to updateStubFile if it already exists.
func generateStubFile(outDir, pkgName string, svc serviceInfo, dryRun, verbose bool) error {
	var buf bytes.Buffer

	// Derive struct name from interface (e.g. EchoServiceServer -> EchoServer)
	structName := deriveStructName(svc.InterfaceName)

	// Derive filename
	fileName := toSnakeCase(structName) + ".go"

	outPath := filepath.Join(outDir, fileName)
	if _, err := os.Stat(outPath); err == nil {
		return updateStubFile(outPath, structName, svc, dryRun)
	}

	if verbose {
		log.Printf("creating new stub file: %s", outPath)
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

	if dryRun {
		log.Printf("[dry-run] would write %s (%d bytes)", outPath, len(src))
		return nil
	}
	return os.WriteFile(outPath, src, 0o644)
}

// updateStubFile updates an existing stub file: adds missing methods, fixes signatures, repairs imports.
func updateStubFile(path, structName string, svc serviceInfo, dryRun bool) error {
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

	if dryRun {
		log.Printf("[dry-run] would update %s (%d bytes)", path, len(res))
		return nil
	}
	return os.WriteFile(path, res, 0o644)
}

// updateStructAndConstructor patches the struct and constructor to include EnableLogging if missing.
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
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if ue, ok := n.(*ast.UnaryExpr); ok && ue.Op == token.AND {
						if cl, ok := ue.X.(*ast.CompositeLit); ok {
							if ident, ok := cl.Type.(*ast.Ident); ok && ident.Name == structName {
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

// generateMethod creates a new stub method implementation.
func generateMethod(structName, methodName string, sig *types.Signature, imports map[string]string) string {
	var buf bytes.Buffer

	params := sig.Params()
	results := sig.Results()

	fmt.Fprintf(&buf, "func (s *%s) %s(", structName, methodName)

	var paramList []string
	var logParamNames []string

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

		if i > 0 {
			logParamNames = append(logParamNames, pName)
		}

		pType := formatType(p.Type(), imports)
		paramList = append(paramList, fmt.Sprintf("%s %s", pName, pType))
	}
	fmt.Fprintf(&buf, "%s)", strings.Join(paramList, ", "))

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
		formatParts := make([]string, len(logParamNames))
		for i := range formatParts {
			formatParts[i] = "%+v"
		}
		formatStr := strings.Join(formatParts, ", ")
		argsStr := strings.Join(logParamNames, ", ")

		fmt.Fprintf(&buf, "\t\tlog.Printf(\"[req_id=%%s] [%s] stub %s called with: %s\", reqID, %s)\n",
			structName, methodName, formatStr, argsStr)
	} else {
		fmt.Fprintf(&buf, "\t\tlog.Printf(\"[req_id=%%s] [%s] stub %s called\", reqID)\n",
			structName, methodName)
	}
	fmt.Fprintf(&buf, "\t}\n")

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

// generateMethodWithExistingBody generates a method with a new signature but preserves the existing body.
func generateMethodWithExistingBody(structName, methodName string, sig *types.Signature, imports map[string]string, existingFn *ast.FuncDecl, src []byte, fset *token.FileSet) string {
	startOffset := fset.Position(existingFn.Pos()).Offset
	bodyStartOffset := fset.Position(existingFn.Body.Pos()).Offset
	prevSig := string(src[startOffset:bodyStartOffset])
	prevSig = strings.TrimSpace(prevSig)

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

	paramNames := make([]string, sig.Params().Len())

	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		newParamType := params.At(i).Type()
		newParamTypeStr := removeWhitespace(formatType(newParamType, imports))

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

// deriveStructName derives the stub struct name from the gRPC interface name.
func deriveStructName(interfaceName string) string {
	structName := strings.TrimSuffix(interfaceName, "Server") + "Server"
	if strings.HasSuffix(interfaceName, "ServiceServer") {
		structName = strings.TrimSuffix(interfaceName, "ServiceServer") + "Server"
	}
	return structName
}
