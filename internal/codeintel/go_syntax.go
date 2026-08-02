package codeintel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type goSyntaxRecord struct {
	Package       string
	PackagePath   string
	ImportAliases map[string]string
	Imports       []string
	Symbols       []Symbol
	References    []Reference
}

func parseGoSyntax(root, full, rel string, source []byte) (goSyntaxRecord, error) {
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, full, source, parser.AllErrors)
	if file == nil {
		return goSyntaxRecord{}, parseErr
	}

	moduleRoot, modulePath := nearestGoModule(root, filepath.Dir(full))
	packageName := ""
	if file.Name != nil {
		packageName = file.Name.Name
	}
	packagePath := packageName
	if modulePath != "" {
		dir := filepath.Dir(full)
		if relDir, err := filepath.Rel(moduleRoot, dir); err == nil && !pathEscapesRoot(relDir, nil) {
			packagePath = modulePath
			if relDir != "." {
				packagePath += "/" + filepath.ToSlash(relDir)
			}
		}
	}

	out := goSyntaxRecord{
		Package:       packageName,
		PackagePath:   packagePath,
		ImportAliases: map[string]string{},
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || strings.TrimSpace(path) == "" {
			continue
		}
		out.Imports = append(out.Imports, path)
		alias := filepath.Base(path)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != "" && alias != "_" && alias != "." {
			out.ImportAliases[alias] = path
		}
	}

	declarationPositions := map[token.Pos]bool{}
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if typed.Name == nil {
				continue
			}
			declarationPositions[typed.Name.Pos()] = true
			receiver := receiverTypeName(typed.Recv)
			kind := "function"
			qualified := qualifyGoName(packagePath, typed.Name.Name)
			container := ""
			if receiver != "" {
				kind = "method"
				container = qualifyGoName(packagePath, receiver)
				qualified = container + "." + typed.Name.Name
			}
			out.Symbols = append(out.Symbols, goSymbol(fset, rel, packageName, qualified, container, typed.Name.Name, kind, typed))
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch item := spec.(type) {
				case *ast.TypeSpec:
					declarationPositions[item.Name.Pos()] = true
					kind := "type"
					switch item.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					out.Symbols = append(out.Symbols, goSymbol(fset, rel, packageName, qualifyGoName(packagePath, item.Name.Name), "", item.Name.Name, kind, item))
				case *ast.ValueSpec:
					kind := "variable"
					if typed.Tok == token.CONST {
						kind = "constant"
					}
					for _, name := range item.Names {
						if name == nil || name.Name == "_" {
							continue
						}
						declarationPositions[name.Pos()] = true
						out.Symbols = append(out.Symbols, goSymbol(fset, rel, packageName, qualifyGoName(packagePath, name.Name), "", name.Name, kind, item))
					}
				}
			}
		}
	}

	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			container := ""
			if typed.Name != nil {
				container = qualifyGoName(packagePath, typed.Name.Name)
				if receiver := receiverTypeName(typed.Recv); receiver != "" {
					container = qualifyGoName(packagePath, receiver) + "." + typed.Name.Name
				}
			}
			if typed.Type != nil {
				collectGoReferences(fset, typed.Type, rel, packageName, packagePath, container, out.ImportAliases, declarationPositions, &out.References)
			}
			if typed.Body != nil {
				collectGoReferences(fset, typed.Body, rel, packageName, packagePath, container, out.ImportAliases, declarationPositions, &out.References)
			}
		case *ast.GenDecl:
			if typed.Tok == token.IMPORT {
				continue
			}
			collectGoReferences(fset, typed, rel, packageName, packagePath, packagePath, out.ImportAliases, declarationPositions, &out.References)
		}
	}
	return out, parseErr
}

func goSymbol(fset *token.FileSet, rel, pkg, qualified, container, name, kind string, node ast.Node) Symbol {
	start, end, startByte, endByte := goNodeRange(fset, node)
	return Symbol{
		Name: name, QualifiedName: qualified, Package: pkg, Container: container,
		Kind: kind, Language: "go", Path: rel, StartLine: start, EndLine: end,
		StartByte: startByte, EndByte: endByte, NodeType: "go/ast",
	}
}

func collectGoReferences(fset *token.FileSet, node ast.Node, rel, pkg, packagePath, container string, aliases map[string]string, declarations map[token.Pos]bool, out *[]Reference) {
	if node == nil {
		return
	}
	var stack []ast.Node
	ast.Inspect(node, func(current ast.Node) bool {
		if current == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, current)

		switch item := current.(type) {
		case *ast.SelectorExpr:
			kind := "reference"
			if call, ok := parent.(*ast.CallExpr); ok && call.Fun == item {
				kind = "call"
			}
			receiver := goExprName(item.X)
			qualified := ""
			if imported, ok := aliases[receiver]; ok {
				qualified = imported + "." + item.Sel.Name
			} else if receiver != "" {
				qualified = receiver + "." + item.Sel.Name
			}
			*out = append(*out, goReference(fset, rel, pkg, qualified, container, receiver, item.Sel.Name, kind, item.Sel))
		case *ast.Ident:
			if item.Name == "_" || declarations[item.Pos()] || goBuiltinNames[item.Name] {
				return true
			}
			if selector, ok := parent.(*ast.SelectorExpr); ok && (selector.X == item || selector.Sel == item) {
				return true
			}
			if _, ok := parent.(*ast.ImportSpec); ok {
				return true
			}
			kind := "identifier"
			if call, ok := parent.(*ast.CallExpr); ok && call.Fun == item {
				kind = "call"
			}
			// Lowercase non-call identifiers are overwhelmingly locals, parameters
			// and fields. Keeping them would inflate the graph and create false
			// package-level references. Calls and exported identifiers remain useful.
			if kind != "call" && !ast.IsExported(item.Name) {
				return true
			}
			qualified := qualifyGoName(packagePath, item.Name)
			*out = append(*out, goReference(fset, rel, pkg, qualified, container, "", item.Name, kind, item))
		}
		return true
	})
}

func goReference(fset *token.FileSet, rel, pkg, qualified, container, receiver, name, kind string, node ast.Node) Reference {
	start, end, startByte, endByte := goNodeRange(fset, node)
	return Reference{
		Name: name, QualifiedName: qualified, Package: pkg, Container: container,
		Receiver: receiver, Kind: kind, Language: "go", Path: rel,
		StartLine: start, EndLine: end, StartByte: startByte, EndByte: endByte,
	}
}

func goNodeRange(fset *token.FileSet, node ast.Node) (startLine, endLine int, startByte, endByte uint32) {
	if node == nil {
		return 1, 1, 0, 0
	}
	start := fset.PositionFor(node.Pos(), false)
	end := fset.PositionFor(node.End(), false)
	startLine, endLine = start.Line, end.Line
	if startLine <= 0 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	if start.Offset >= 0 {
		startByte = uint32(start.Offset)
	}
	if end.Offset >= 0 {
		endByte = uint32(end.Offset)
	}
	return
}

func nearestGoModule(root, start string) (string, string) {
	root = filepath.Clean(root)
	current := filepath.Clean(start)
	for {
		if rel, err := filepath.Rel(root, current); pathEscapesRoot(rel, err) {
			break
		}
		path := filepath.Join(current, "go.mod")
		if data, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(strings.TrimSpace(line))
				if len(fields) == 2 && fields[0] == "module" {
					return current, strings.TrimSpace(fields[1])
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current || current == root {
			break
		}
		current = parent
	}
	return root, ""
}

func qualifyGoName(packagePath, name string) string {
	packagePath = strings.TrimSuffix(strings.TrimSpace(packagePath), ".")
	name = strings.TrimPrefix(strings.TrimSpace(name), ".")
	if packagePath == "" {
		return name
	}
	if name == "" {
		return packagePath
	}
	return packagePath + "." + name
}

func receiverTypeName(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	return goTypeName(fields.List[0].Type)
}

func goTypeName(expr ast.Expr) string {
	switch item := expr.(type) {
	case *ast.Ident:
		return item.Name
	case *ast.StarExpr:
		return goTypeName(item.X)
	case *ast.IndexExpr:
		return goTypeName(item.X)
	case *ast.IndexListExpr:
		return goTypeName(item.X)
	case *ast.SelectorExpr:
		prefix := goExprName(item.X)
		if prefix == "" {
			return item.Sel.Name
		}
		return prefix + "." + item.Sel.Name
	default:
		return ""
	}
}

func goExprName(expr ast.Expr) string {
	switch item := expr.(type) {
	case *ast.Ident:
		return item.Name
	case *ast.SelectorExpr:
		prefix := goExprName(item.X)
		if prefix == "" {
			return item.Sel.Name
		}
		return prefix + "." + item.Sel.Name
	case *ast.ParenExpr:
		return goExprName(item.X)
	case *ast.StarExpr:
		return goExprName(item.X)
	case *ast.IndexExpr:
		return goExprName(item.X)
	case *ast.IndexListExpr:
		return goExprName(item.X)
	default:
		return ""
	}
}

var goBuiltinNames = map[string]bool{
	"nil": true, "true": true, "false": true, "iota": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true,
	"complex128": true, "error": true, "float32": true, "float64": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"rune": true, "string": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "uintptr": true,
}
