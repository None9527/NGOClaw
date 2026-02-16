package codeintel

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// Symbol represents a code symbol extracted from source files
type Symbol struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`       // "function", "class", "method", "variable", "interface", "struct"
	File       string `json:"file"`
	Line       int    `json:"line"`
	EndLine    int    `json:"end_line"`
	Signature  string `json:"signature"`
	Parent     string `json:"parent,omitempty"`     // Parent class/struct for methods
	Language   string `json:"language"`
	Exported   bool   `json:"exported"`
	DocComment string `json:"doc_comment,omitempty"`
}

// FileIndex holds all symbols extracted from a single file
type FileIndex struct {
	Path     string    `json:"path"`
	Language string    `json:"language"`
	Symbols  []Symbol  `json:"symbols"`
	Lines    int       `json:"lines"`
	Size     int64     `json:"size"`
}

// Indexer extracts code symbols from source files.
// Uses Go's native AST parser for Go files and regex-based
// extraction for Python, JavaScript/TypeScript, and Rust.
type Indexer struct {
	logger *zap.Logger
	mu     sync.Mutex
	index  map[string]*FileIndex
}

// NewIndexer creates a new code indexer
func NewIndexer(logger *zap.Logger) *Indexer {
	return &Indexer{
		logger: logger.With(zap.String("component", "indexer")),
		index:  make(map[string]*FileIndex),
	}
}

// IndexFile parses a single file and extracts symbols
func (idx *Indexer) IndexFile(path string) (*FileIndex, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	lang := detectLanguage(path)
	if lang == "" {
		return nil, nil // unsupported file type
	}

	var symbols []Symbol

	switch lang {
	case "go":
		symbols, err = idx.parseGo(path)
	case "python":
		symbols, err = idx.parsePython(path)
	case "javascript", "typescript":
		symbols, err = idx.parseJSTS(path)
	case "rust":
		symbols, err = idx.parseRust(path)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	lines, _ := countLines(path)
	fi := &FileIndex{
		Path:     path,
		Language: lang,
		Symbols:  symbols,
		Lines:    lines,
		Size:     info.Size(),
	}

	idx.mu.Lock()
	idx.index[path] = fi
	idx.mu.Unlock()

	return fi, nil
}

// IndexDirectory recursively indexes all supported files in a directory
func (idx *Indexer) IndexDirectory(root string, excludes []string) (int, error) {
	count := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			name := info.Name()
			for _, ex := range defaultExcludes {
				if name == ex {
					return filepath.SkipDir
				}
			}
			for _, ex := range excludes {
				if name == ex {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if info.Size() > 1024*1024 { // skip files > 1MB
			return nil
		}

		fi, err := idx.IndexFile(path)
		if err == nil && fi != nil {
			count++
		}
		return nil
	})

	idx.logger.Info("Directory indexed",
		zap.String("root", root),
		zap.Int("files", count),
	)
	return count, err
}

// GetSymbols returns all indexed symbols
func (idx *Indexer) GetSymbols() []Symbol {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var all []Symbol
	for _, fi := range idx.index {
		all = append(all, fi.Symbols...)
	}
	return all
}

// GetFileIndex returns the index for a specific file
func (idx *Indexer) GetFileIndex(path string) (*FileIndex, bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	fi, ok := idx.index[path]
	return fi, ok
}

// SearchSymbols finds symbols matching a query
func (idx *Indexer) SearchSymbols(query string) []Symbol {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	query = strings.ToLower(query)
	var results []Symbol
	for _, fi := range idx.index {
		for _, sym := range fi.Symbols {
			if strings.Contains(strings.ToLower(sym.Name), query) {
				results = append(results, sym)
			}
		}
	}
	return results
}

var defaultExcludes = []string{
	".git", "node_modules", "__pycache__", ".venv", "venv",
	"vendor", "dist", "build", ".next", "target",
}

// --- Go Parser (native AST) ---

func (idx *Indexer) parseGo(path string) ([]Symbol, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := Symbol{
				Name:     d.Name.Name,
				Kind:     "function",
				File:     path,
				Line:     fset.Position(d.Pos()).Line,
				EndLine:  fset.Position(d.End()).Line,
				Language: "go",
				Exported: d.Name.IsExported(),
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym.Kind = "method"
				sym.Parent = typeString(d.Recv.List[0].Type)
			}
			if d.Doc != nil {
				sym.DocComment = d.Doc.Text()
			}
			sym.Signature = funcSignature(d)
			symbols = append(symbols, sym)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch s.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					sym := Symbol{
						Name:     s.Name.Name,
						Kind:     kind,
						File:     path,
						Line:     fset.Position(s.Pos()).Line,
						EndLine:  fset.Position(s.End()).Line,
						Language: "go",
						Exported: s.Name.IsExported(),
					}
					if d.Doc != nil {
						sym.DocComment = d.Doc.Text()
					}
					symbols = append(symbols, sym)
				}
			}
		}
	}

	return symbols, nil
}

// --- Python Parser (regex-based) ---

var (
	pyClassRe    = regexp.MustCompile(`^class\s+(\w+)`)
	pyFuncRe     = regexp.MustCompile(`^(\s*)def\s+(\w+)\s*\(`)
	pyAsyncFuncRe = regexp.MustCompile(`^(\s*)async\s+def\s+(\w+)\s*\(`)
)

func (idx *Indexer) parsePython(path string) ([]Symbol, error) {
	return parseWithRegex(path, "python", func(line string, lineNum int) *Symbol {
		if m := pyClassRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "class", Line: lineNum, Exported: !strings.HasPrefix(m[1], "_")}
		}
		if m := pyFuncRe.FindStringSubmatch(line); m != nil {
			kind := "function"
			if len(m[1]) > 0 {
				kind = "method" // indented = method
			}
			return &Symbol{Name: m[2], Kind: kind, Line: lineNum, Exported: !strings.HasPrefix(m[2], "_")}
		}
		if m := pyAsyncFuncRe.FindStringSubmatch(line); m != nil {
			kind := "function"
			if len(m[1]) > 0 {
				kind = "method"
			}
			return &Symbol{Name: m[2], Kind: kind, Line: lineNum, Exported: !strings.HasPrefix(m[2], "_")}
		}
		return nil
	})
}

// --- JS/TS Parser (regex-based) ---

var (
	jsFuncRe   = regexp.MustCompile(`(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	jsClassRe  = regexp.MustCompile(`(?:export\s+)?class\s+(\w+)`)
	jsArrowRe  = regexp.MustCompile(`(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(`)
	jsMethodRe = regexp.MustCompile(`^\s+(?:async\s+)?(\w+)\s*\(`)
)

func (idx *Indexer) parseJSTS(path string) ([]Symbol, error) {
	return parseWithRegex(path, detectLanguage(path), func(line string, lineNum int) *Symbol {
		if m := jsClassRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "class", Line: lineNum, Exported: strings.Contains(line, "export")}
		}
		if m := jsFuncRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "function", Line: lineNum, Exported: strings.Contains(line, "export")}
		}
		if m := jsArrowRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "function", Line: lineNum, Exported: strings.Contains(line, "export")}
		}
		return nil
	})
}

// --- Rust Parser (regex-based) ---

var (
	rustFnRe     = regexp.MustCompile(`(?:pub\s+)?(?:async\s+)?fn\s+(\w+)`)
	rustStructRe = regexp.MustCompile(`(?:pub\s+)?struct\s+(\w+)`)
	rustEnumRe   = regexp.MustCompile(`(?:pub\s+)?enum\s+(\w+)`)
	rustTraitRe  = regexp.MustCompile(`(?:pub\s+)?trait\s+(\w+)`)
	rustImplRe   = regexp.MustCompile(`impl(?:<[^>]+>)?\s+(\w+)`)
)

func (idx *Indexer) parseRust(path string) ([]Symbol, error) {
	return parseWithRegex(path, "rust", func(line string, lineNum int) *Symbol {
		if m := rustStructRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "struct", Line: lineNum, Exported: strings.HasPrefix(line, "pub")}
		}
		if m := rustEnumRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "enum", Line: lineNum, Exported: strings.HasPrefix(line, "pub")}
		}
		if m := rustTraitRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "interface", Line: lineNum, Exported: strings.HasPrefix(line, "pub")}
		}
		if m := rustFnRe.FindStringSubmatch(line); m != nil {
			return &Symbol{Name: m[1], Kind: "function", Line: lineNum, Exported: strings.HasPrefix(line, "pub")}
		}
		return nil
	})
}

// --- Helpers ---

type lineParser func(line string, lineNum int) *Symbol

func parseWithRegex(path, lang string, parse lineParser) ([]Symbol, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var symbols []Symbol
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if sym := parse(line, lineNum); sym != nil {
			sym.File = path
			sym.Language = lang
			symbols = append(symbols, *sym)
		}
	}
	return symbols, scanner.Err()
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	default:
		return ""
	}
}

func funcSignature(decl *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(typeString(decl.Recv.List[0].Type))
		b.WriteString(") ")
	}
	b.WriteString(decl.Name.Name)
	b.WriteString("(")
	if decl.Type.Params != nil {
		for i, p := range decl.Type.Params.List {
			if i > 0 {
				b.WriteString(", ")
			}
			for j, name := range p.Names {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(name.Name)
			}
			b.WriteString(" ")
			b.WriteString(typeString(p.Type))
		}
	}
	b.WriteString(")")
	return b.String()
}
