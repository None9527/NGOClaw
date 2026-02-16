package codeintel

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"go.uber.org/zap"
)

// RepoMap generates an Aider-style repository map using PageRank.
// The map shows the most important symbols and their relationships,
// giving the LLM an overview of the codebase structure.
type RepoMap struct {
	indexer  *Indexer
	logger   *zap.Logger
}

// Edge represents a directed reference between two symbols
type Edge struct {
	From string // "file:SymbolName"
	To   string // "file:SymbolName"
}

// RankedSymbol is a symbol with its computed importance score
type RankedSymbol struct {
	Symbol
	Score float64 `json:"score"`
}

// NewRepoMap creates a repo map generator
func NewRepoMap(indexer *Indexer, logger *zap.Logger) *RepoMap {
	return &RepoMap{
		indexer: indexer,
		logger:  logger.With(zap.String("component", "repomap")),
	}
}

// Generate creates a text representation of the most important symbols
// in the codebase, suitable for including in an LLM context window.
func (rm *RepoMap) Generate(maxTokens int) string {
	symbols := rm.indexer.GetSymbols()
	if len(symbols) == 0 {
		return "# Repository Map\n\n(no symbols indexed)\n"
	}

	// Build reference graph
	edges := rm.buildReferenceGraph(symbols)

	// Run PageRank
	ranked := rm.pageRank(symbols, edges)

	// Sort by score descending
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	// Build output, respecting token budget
	return rm.formatMap(ranked, maxTokens)
}

// GenerateForFiles creates a focused map for specific files
func (rm *RepoMap) GenerateForFiles(files []string, maxTokens int) string {
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}

	allSymbols := rm.indexer.GetSymbols()
	var relevant []Symbol
	for _, s := range allSymbols {
		if fileSet[s.File] {
			relevant = append(relevant, s)
		}
	}

	if len(relevant) == 0 {
		return "# Repository Map (focused)\n\n(no symbols in specified files)\n"
	}

	edges := rm.buildReferenceGraph(relevant)
	ranked := rm.pageRank(relevant, edges)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	return rm.formatMap(ranked, maxTokens)
}

// buildReferenceGraph creates edges between symbols based on name references.
// A symbol references another if its name appears in the file of the other.
func (rm *RepoMap) buildReferenceGraph(symbols []Symbol) []Edge {
	// Build a name → symbol key map
	nameToKey := make(map[string][]string) // name → ["file:Name", ...]
	for _, s := range symbols {
		key := fmt.Sprintf("%s:%s", s.File, s.Name)
		nameToKey[s.Name] = append(nameToKey[s.Name], key)
	}

	// Group symbols by file
	fileSymbols := make(map[string][]Symbol)
	for _, s := range symbols {
		fileSymbols[s.File] = append(fileSymbols[s.File], s)
	}

	var edges []Edge
	seen := make(map[string]bool)

	for _, s := range symbols {
		sKey := fmt.Sprintf("%s:%s", s.File, s.Name)

		// Check if this symbol's name is referenced by symbols in other files
		for file, syms := range fileSymbols {
			if file == s.File {
				continue
			}
			for _, other := range syms {
				oKey := fmt.Sprintf("%s:%s", other.File, other.Name)

				// Does symbol 's' reference 'other' by name?
				edgeKey := sKey + "->" + oKey
				if !seen[edgeKey] && s.Name != other.Name {
					// Simple heuristic: exported symbols with matching names
					if other.Exported && len(other.Name) > 2 {
						edges = append(edges, Edge{From: sKey, To: oKey})
						seen[edgeKey] = true
					}
				}
			}
		}
	}

	return edges
}

// pageRank computes importance scores using the PageRank algorithm
func (rm *RepoMap) pageRank(symbols []Symbol, edges []Edge) []RankedSymbol {
	const (
		dampingFactor = 0.85
		iterations    = 20
		epsilon       = 1e-6
	)

	// Build key → index map
	keyToIdx := make(map[string]int)
	for i, s := range symbols {
		key := fmt.Sprintf("%s:%s", s.File, s.Name)
		keyToIdx[key] = i
	}

	n := len(symbols)
	if n == 0 {
		return nil
	}

	// Build adjacency: outLinks[i] = list of indices that i links to
	outLinks := make([][]int, n)
	inLinks := make([][]int, n)

	for _, e := range edges {
		fromIdx, ok1 := keyToIdx[e.From]
		toIdx, ok2 := keyToIdx[e.To]
		if ok1 && ok2 && fromIdx != toIdx {
			outLinks[fromIdx] = append(outLinks[fromIdx], toIdx)
			inLinks[toIdx] = append(inLinks[toIdx], fromIdx)
		}
	}

	// Initialize scores
	scores := make([]float64, n)
	initial := 1.0 / float64(n)
	for i := range scores {
		scores[i] = initial
	}

	// Iterate
	for iter := 0; iter < iterations; iter++ {
		newScores := make([]float64, n)
		maxDelta := 0.0

		for i := 0; i < n; i++ {
			sum := 0.0
			for _, j := range inLinks[i] {
				if len(outLinks[j]) > 0 {
					sum += scores[j] / float64(len(outLinks[j]))
				}
			}
			newScores[i] = (1-dampingFactor)/float64(n) + dampingFactor*sum

			if delta := math.Abs(newScores[i] - scores[i]); delta > maxDelta {
				maxDelta = delta
			}
		}

		scores = newScores
		if maxDelta < epsilon {
			break
		}
	}

	// Boost exported symbols and those with doc comments
	for i, s := range symbols {
		if s.Exported {
			scores[i] *= 1.5
		}
		if s.DocComment != "" {
			scores[i] *= 1.2
		}
		// Boost interfaces and structs
		if s.Kind == "interface" || s.Kind == "struct" || s.Kind == "class" {
			scores[i] *= 1.3
		}
	}

	// Build ranked list
	ranked := make([]RankedSymbol, n)
	for i, s := range symbols {
		ranked[i] = RankedSymbol{Symbol: s, Score: scores[i]}
	}

	return ranked
}

// formatMap generates the text output, staying within token budget
func (rm *RepoMap) formatMap(ranked []RankedSymbol, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = 4000 // default ~4k tokens
	}

	var b strings.Builder
	b.WriteString("# Repository Map\n\n")

	// Group by file
	type fileEntry struct {
		path    string
		symbols []RankedSymbol
	}

	fileMap := make(map[string]*fileEntry)
	var fileOrder []string

	for _, rs := range ranked {
		if _, ok := fileMap[rs.File]; !ok {
			fileMap[rs.File] = &fileEntry{path: rs.File}
			fileOrder = append(fileOrder, rs.File)
		}
		fileMap[rs.File].symbols = append(fileMap[rs.File].symbols, rs)
	}

	// Sort files by max symbol score
	sort.Slice(fileOrder, func(i, j int) bool {
		si := fileMap[fileOrder[i]].symbols
		sj := fileMap[fileOrder[j]].symbols
		return si[0].Score > sj[0].Score
	})

	// Estimate ~4 chars per token
	charBudget := maxTokens * 4
	currentChars := b.Len()

	for _, filePath := range fileOrder {
		entry := fileMap[filePath]

		section := fmt.Sprintf("## %s\n\n", filePath)

		for _, rs := range entry.symbols {
			line := ""
			switch rs.Kind {
			case "struct", "class":
				line = fmt.Sprintf("- %s `%s` (L%d)\n", rs.Kind, rs.Name, rs.Line)
			case "interface":
				line = fmt.Sprintf("- interface `%s` (L%d)\n", rs.Name, rs.Line)
			case "function", "method":
				sig := rs.Signature
				if sig == "" {
					sig = rs.Name + "()"
				}
				if rs.Parent != "" {
					line = fmt.Sprintf("  - `%s` (L%d)\n", sig, rs.Line)
				} else {
					line = fmt.Sprintf("- func `%s` (L%d)\n", sig, rs.Line)
				}
			default:
				line = fmt.Sprintf("- %s `%s` (L%d)\n", rs.Kind, rs.Name, rs.Line)
			}
			section += line
		}

		section += "\n"

		if currentChars+len(section) > charBudget {
			break
		}
		b.WriteString(section)
		currentChars += len(section)
	}

	return b.String()
}
