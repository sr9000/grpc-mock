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
	"strings"
	"unicode"
)

// newFileImports creates a shared imports map for generating a stub file.
func newFileImports(genprotoPkgPath string) map[string]string {
	imports := map[string]string{
		"context":                "",
		"log":                    "",
		"google.golang.org/grpc": "",
		"grpc-mock/pkg/ctxkeys":  "",
	}

	alias := genprotoPackageAlias(genprotoPkgPath)
	if alias == "" {
		panic("non-genproto packages are not supported yet")
	}
	imports[genprotoPkgPath] = alias

	return imports
}

// genprotoPackageAlias returns an alias for genproto packages with "pb" suffix.
func genprotoPackageAlias(pkgPath string) string {
	if !strings.HasPrefix(pkgPath, genprotoPath+"/") && pkgPath != genprotoPath {
		return ""
	}
	parts := strings.Split(pkgPath, "/")
	pkgName := parts[len(parts)-1]

	if strings.HasSuffix(pkgName, "pb") {
		return pkgName
	}
	return pkgName + "pb"
}

// formatType formats a Go type for code generation, managing import aliases.
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

		if alias, ok := imports[pkgPath]; ok {
			if alias != "" {
				return alias + "." + obj.Name()
			}
			return pkg.Name() + "." + obj.Name()
		}

		if alias := genprotoPackageAlias(pkgPath); alias != "" {
			imports[pkgPath] = alias
			return alias + "." + obj.Name()
		}

		pkgName := pkg.Name()
		if alias := resolveConflictingAlias(pkgPath, pkgName, imports); alias != "" {
			imports[pkgPath] = alias
			return alias + "." + obj.Name()
		}

		imports[pkgPath] = ""
		return pkgName + "." + obj.Name()
	}
}

// zeroValue returns the zero value expression for a Go type.
func zeroValue(t types.Type, imports map[string]string) string {
	switch tt := t.(type) {
	case *types.Named:
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

// resolveConflictingAlias checks if the package name conflicts with existing imports
// and returns a unique alias if needed.
func resolveConflictingAlias(pkgPath, pkgName string, imports map[string]string) string {
	for existingPath, existingAlias := range imports {
		if existingPath == pkgPath {
			continue
		}
		usedName := existingAlias
		if usedName == "" {
			parts := strings.Split(existingPath, "/")
			usedName = parts[len(parts)-1]
		}
		if usedName == pkgName {
			return generateUniqueAlias(pkgPath, imports)
		}
	}
	return ""
}

// generateUniqueAlias creates a unique import alias from a package path.
func generateUniqueAlias(pkgPath string, imports map[string]string) string {
	parts := strings.Split(pkgPath, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.Join(parts[i:], "")
		candidate = strings.ReplaceAll(candidate, ".", "")
		candidate = strings.ReplaceAll(candidate, "-", "")
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
	alias := strings.ReplaceAll(pkgPath, "/", "")
	alias = strings.ReplaceAll(alias, ".", "")
	alias = strings.ReplaceAll(alias, "-", "")
	return alias
}

// isReceiver checks if an AST expression matches the given struct name as a receiver.
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

// signaturesMatch checks if an existing AST function declaration matches a types.Signature.
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

// typeToString converts an AST expression to a string representation.
func typeToString(expr ast.Expr, fset *token.FileSet) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

// removeWhitespace removes all whitespace from a string for comparison.
func removeWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// toSnakeCase converts CamelCase to snake_case.
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

// toPascalCase converts snake_case or kebab-case to PascalCase.
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

// annotateOrphanedMethods scans stub files for methods that no longer exist
// in the corresponding gRPC interface and logs them.
func annotateOrphanedMethods(pkgMap map[string]*stubPkg, dryRun, verbose bool) error {
	for _, sp := range pkgMap {
		for _, svc := range sp.Services {
			structName := deriveStructName(svc.InterfaceName)
			fileName := toSnakeCase(structName) + ".go"
			outPath := filepath.Join(sp.OutDir, fileName)

			knownMethods := make(map[string]bool)
			iface := svc.Iface
			for i := 0; i < iface.NumMethods(); i++ {
				m := iface.Method(i)
				if m.Exported() {
					knownMethods[m.Name()] = true
				}
			}

			src, err := os.ReadFile(outPath)
			if err != nil {
				if verbose {
					log.Printf("prune: skipping %s: %v", outPath, err)
				}
				continue
			}

			fset := token.NewFileSet()
			node, err := parser.ParseFile(fset, outPath, src, parser.ParseComments)
			if err != nil {
				continue
			}

			var orphans []string
			for _, decl := range node.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				if !isReceiver(fn.Recv.List[0].Type, structName) {
					continue
				}
				if strings.HasPrefix(fn.Name.Name, "New") {
					continue
				}
				if !knownMethods[fn.Name.Name] {
					orphans = append(orphans, fn.Name.Name)
					if verbose {
						log.Printf("prune: orphaned method %s.%s in %s", structName, fn.Name.Name, outPath)
					}
				}
			}

			if len(orphans) > 0 {
				if dryRun {
					log.Printf("[dry-run] prune: would annotate %d orphaned method(s) in %s: %v", len(orphans), outPath, orphans)
				} else {
					log.Printf("prune: found %d orphaned method(s) in %s: %v (consider removing manually)", len(orphans), outPath, orphans)
				}
			}
		}
	}
	return nil
}
