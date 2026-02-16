package tool

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// RepoMapTool generates a structural map of a codebase (functions, classes, interfaces).
// Uses Go's built-in AST parser for .go files, regex-based grep for Python/JS/TS.
type RepoMapTool struct {
	logger *zap.Logger
}

func NewRepoMapTool(logger *zap.Logger) *RepoMapTool {
	return &RepoMapTool{logger: logger}
}

func (t *RepoMapTool) Name() string        { return "repo_map" }
func (t *RepoMapTool) Kind() domaintool.Kind { return domaintool.KindRead }

func (t *RepoMapTool) Description() string {
	return "Generate a structural map of a codebase showing functions, classes, interfaces, and method signatures. " +
		"Use this to understand a project's architecture before editing code. " +
		"For Go files it uses full AST parsing; for Python/JS/TS it uses pattern matching."
}

func (t *RepoMapTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Root directory to scan",
			},
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Filter by language: go, python, js, ts, all (default: all)",
			},
			"max_depth": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum directory depth to scan (default: 4, max: 8)",
			},
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g. '*_test.go')",
			},
		},
		"required": []string{"path"},
	}
}

func (t *RepoMapTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	rootPath, ok := args["path"].(string)
	if !ok || rootPath == "" {
		return &Result{Success: false, Error: "path is required"}, nil
	}

	info, err := os.Stat(rootPath)
	if err != nil || !info.IsDir() {
		return &Result{Success: false, Error: fmt.Sprintf("path '%s' is not a valid directory", rootPath)}, nil
	}

	lang := "all"
	if l, ok := args["language"].(string); ok && l != "" {
		lang = strings.ToLower(l)
	}

	maxDepth := 4
	if d, ok := args["max_depth"].(float64); ok && d > 0 {
		maxDepth = int(d)
		if maxDepth > 8 {
			maxDepth = 8
		}
	}

	filterPattern := ""
	if p, ok := args["pattern"].(string); ok {
		filterPattern = p
	}

	t.logger.Info("Generating repo map",
		zap.String("path", rootPath),
		zap.String("language", lang),
		zap.Int("max_depth", maxDepth),
	)

	// Collect files
	var files []string
	baseDepth := strings.Count(filepath.Clean(rootPath), string(os.PathSeparator))

	if err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			t.logger.Warn("Error accessing path during walk",
				zap.String("path", path),
				zap.Error(err),
			)
			return nil
		}
		// Skip hidden dirs and common noise
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" {
				return filepath.SkipDir
			}
			depth := strings.Count(filepath.Clean(path), string(os.PathSeparator)) - baseDepth
			if depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if !matchLanguage(ext, lang) {
			return nil
		}
		if filterPattern != "" {
			matched, _ := filepath.Match(filterPattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}
		files = append(files, path)
		return nil
	}); err != nil {
		t.logger.Error("filepath.Walk failed", zap.String("root", rootPath), zap.Error(err))
		return &Result{Success: false, Error: fmt.Sprintf("walk error: %v", err)}, nil
	}

	if len(files) == 0 {
		return &Result{Output: "No matching source files found.", Success: true}, nil
	}

	sort.Strings(files)

	// Cap file count
	if len(files) > 100 {
		files = files[:100]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Repo Map: %s (%d files)\n\n", rootPath, len(files)))

	for _, file := range files {
		relPath, _ := filepath.Rel(rootPath, file)
		ext := filepath.Ext(file)

		var symbols []string
		switch ext {
		case ".go":
			symbols = parseGoFile(file)
		case ".py":
			symbols = parsePythonFile(file)
		case ".js", ".ts", ".jsx", ".tsx":
			symbols = parseJSFile(file)
		default:
			symbols = parseGenericFile(file)
		}

		if len(symbols) > 0 {
			sb.WriteString(fmt.Sprintf("%s:\n", relPath))
			for _, sym := range symbols {
				sb.WriteString(fmt.Sprintf("  %s\n", sym))
			}
			sb.WriteString("\n")
		}
	}

	output := sb.String()
	if len(output) > 32000 {
		output = output[:32000] + "\n... (truncated)"
	}

	return &Result{
		Output:  output,
		Success: true,
		Metadata: map[string]interface{}{
			"files_scanned": len(files),
		},
	}, nil
}

// matchLanguage checks if a file extension matches the requested language filter.
func matchLanguage(ext, lang string) bool {
	switch lang {
	case "go":
		return ext == ".go"
	case "python", "py":
		return ext == ".py"
	case "js", "javascript":
		return ext == ".js" || ext == ".jsx"
	case "ts", "typescript":
		return ext == ".ts" || ext == ".tsx"
	case "all", "":
		return ext == ".go" || ext == ".py" || ext == ".js" || ext == ".ts" ||
			ext == ".jsx" || ext == ".tsx" || ext == ".java" || ext == ".rs" ||
			ext == ".rb" || ext == ".c" || ext == ".cpp" || ext == ".h"
	default:
		return ext == "."+lang
	}
}

// parseGoFile uses Go's built-in AST parser for precise symbol extraction.
func parseGoFile(path string) []string {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return []string{fmt.Sprintf("// parse error: %v", err)}
	}

	var symbols []string

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sig := formatFuncDecl(d)
			symbols = append(symbols, sig)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					switch st := s.Type.(type) {
					case *ast.StructType:
						symbols = append(symbols, fmt.Sprintf("type %s struct", s.Name.Name))
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								symbols = append(symbols, fmt.Sprintf("  .%s", name.Name))
							}
						}
					case *ast.InterfaceType:
						symbols = append(symbols, fmt.Sprintf("type %s interface", s.Name.Name))
						for _, method := range st.Methods.List {
							for _, name := range method.Names {
								symbols = append(symbols, fmt.Sprintf("  .%s()", name.Name))
							}
						}
					default:
						symbols = append(symbols, fmt.Sprintf("type %s ...", s.Name.Name))
					}
				}
			}
		}
	}

	return symbols
}

// formatFuncDecl formats a Go function declaration with receiver.
func formatFuncDecl(f *ast.FuncDecl) string {
	var sb strings.Builder
	sb.WriteString("func ")

	if f.Recv != nil && len(f.Recv.List) > 0 {
		recv := f.Recv.List[0]
		sb.WriteString("(")
		sb.WriteString(exprString(recv.Type))
		sb.WriteString(") ")
	}

	sb.WriteString(f.Name.Name)
	sb.WriteString("(")

	if f.Type.Params != nil {
		var params []string
		for _, p := range f.Type.Params.List {
			typStr := exprString(p.Type)
			if len(p.Names) > 0 {
				for _, n := range p.Names {
					params = append(params, fmt.Sprintf("%s %s", n.Name, typStr))
				}
			} else {
				params = append(params, typStr)
			}
		}
		sb.WriteString(strings.Join(params, ", "))
	}
	sb.WriteString(")")

	if f.Type.Results != nil && len(f.Type.Results.List) > 0 {
		var results []string
		for _, r := range f.Type.Results.List {
			results = append(results, exprString(r.Type))
		}
		if len(results) == 1 {
			sb.WriteString(" " + results[0])
		} else {
			sb.WriteString(" (" + strings.Join(results, ", ") + ")")
		}
	}

	return sb.String()
}

// exprString returns a simple string representation of a Go AST expression.
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(e.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprString(e.Key), exprString(e.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprString(e.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + exprString(e.Value)
	default:
		return "?"
	}
}

// parsePythonFile uses line-based pattern matching for Python symbols.
func parsePythonFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var symbols []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if strings.HasPrefix(trimmed, "class ") {
			name := strings.TrimPrefix(trimmed, "class ")
			if idx := strings.IndexAny(name, "(:"); idx > 0 {
				name = name[:idx]
			}
			symbols = append(symbols, "class "+strings.TrimSpace(name))
		} else if strings.HasPrefix(trimmed, "def ") {
			name := strings.TrimPrefix(trimmed, "def ")
			if idx := strings.Index(name, "("); idx > 0 {
				name = name[:idx]
			}
			prefix := ""
			if indent > 0 {
				prefix = "  ."
			} else {
				prefix = "def "
			}
			symbols = append(symbols, prefix+strings.TrimSpace(name)+"()")
		} else if strings.HasPrefix(trimmed, "async def ") {
			name := strings.TrimPrefix(trimmed, "async def ")
			if idx := strings.Index(name, "("); idx > 0 {
				name = name[:idx]
			}
			prefix := ""
			if indent > 0 {
				prefix = "  ."
			} else {
				prefix = "async def "
			}
			symbols = append(symbols, prefix+strings.TrimSpace(name)+"()")
		}
	}
	return symbols
}

// parseJSFile uses line-based pattern matching for JavaScript/TypeScript symbols.
func parseJSFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var symbols []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "export class ") || strings.HasPrefix(trimmed, "class ") {
			name := trimmed
			name = strings.TrimPrefix(name, "export ")
			name = strings.TrimPrefix(name, "class ")
			if idx := strings.IndexAny(name, " {<"); idx > 0 {
				name = name[:idx]
			}
			symbols = append(symbols, "class "+name)
		} else if strings.HasPrefix(trimmed, "export function ") || strings.HasPrefix(trimmed, "function ") {
			name := trimmed
			name = strings.TrimPrefix(name, "export ")
			name = strings.TrimPrefix(name, "function ")
			if idx := strings.Index(name, "("); idx > 0 {
				name = name[:idx]
			}
			symbols = append(symbols, "function "+strings.TrimSpace(name)+"()")
		} else if strings.HasPrefix(trimmed, "export interface ") || strings.HasPrefix(trimmed, "interface ") {
			name := trimmed
			name = strings.TrimPrefix(name, "export ")
			name = strings.TrimPrefix(name, "interface ")
			if idx := strings.IndexAny(name, " {<"); idx > 0 {
				name = name[:idx]
			}
			symbols = append(symbols, "interface "+name)
		} else if strings.HasPrefix(trimmed, "export type ") || (strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "=")) {
			name := trimmed
			name = strings.TrimPrefix(name, "export ")
			name = strings.TrimPrefix(name, "type ")
			if idx := strings.IndexAny(name, " =<"); idx > 0 {
				name = name[:idx]
			}
			symbols = append(symbols, "type "+name)
		}
	}
	return symbols
}

// parseGenericFile returns a file-exists marker for unsupported languages.
func parseGenericFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	lineCount := len(lines)
	return []string{fmt.Sprintf("(%d lines)", lineCount)}
}
