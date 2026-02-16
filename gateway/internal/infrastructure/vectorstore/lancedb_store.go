package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/memory"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	arrowmem "github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
	"go.uber.org/zap"
)

const tableName = "memories"

// LanceDBVectorStore implements memory.VectorStore using LanceDB.
type LanceDBVectorStore struct {
	conn      contracts.IConnection
	table     contracts.ITable
	schema    *arrow.Schema
	dimension int
	logger    *zap.Logger
}

// NewLanceDBVectorStore creates a new LanceDB-backed vector store.
// storePath: directory to persist LanceDB data (e.g. ~/.ngoclaw/memory/lancedb).
// dimension: embedding vector dimension (e.g. 4096).
func NewLanceDBVectorStore(storePath string, dimension int, logger *zap.Logger) (*LanceDBVectorStore, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	absPath, err := expandPath(storePath)
	if err != nil {
		return nil, fmt.Errorf("failed to expand store path: %w", err)
	}
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	ctx := context.Background()
	conn, err := lancedb.Connect(ctx, absPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LanceDB at %s: %w", absPath, err)
	}

	// Build Arrow schema
	fields := []arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "content", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "vector", Type: arrow.FixedSizeListOf(int32(dimension), arrow.PrimitiveTypes.Float32), Nullable: false},
		{Name: "metadata", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "session_id", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "user_id", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "created_at", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "updated_at", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}
	arrowSchema := arrow.NewSchema(fields, nil)

	table, err := openOrCreateTable(ctx, conn, arrowSchema, logger)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open/create table: %w", err)
	}

	logger.Info("LanceDB vector store initialized",
		zap.String("path", absPath),
		zap.Int("dimension", dimension),
	)

	return &LanceDBVectorStore{
		conn:      conn,
		table:     table,
		schema:    arrowSchema,
		dimension: dimension,
		logger:    logger,
	}, nil
}

func openOrCreateTable(ctx context.Context, conn contracts.IConnection, arrowSchema *arrow.Schema, logger *zap.Logger) (contracts.ITable, error) {
	table, err := conn.OpenTable(ctx, tableName)
	if err == nil {
		logger.Info("Opened existing LanceDB table", zap.String("table", tableName))
		return table, nil
	}

	logger.Info("Creating new LanceDB table", zap.String("table", tableName))
	schema, err := lancedb.NewSchema(arrowSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to create LanceDB schema: %w", err)
	}
	return conn.CreateTable(ctx, tableName, schema)
}

// Insert stores a memory entry.
func (s *LanceDBVectorStore) Insert(ctx context.Context, entry *memory.MemoryEntry) error {
	record, err := s.entryToRecord(entry)
	if err != nil {
		return fmt.Errorf("failed to build Arrow record: %w", err)
	}
	defer record.Release()

	if err := s.table.Add(ctx, record, nil); err != nil {
		return fmt.Errorf("LanceDB insert failed: %w", err)
	}
	s.logger.Debug("Memory entry inserted", zap.String("id", entry.ID))
	return nil
}

// Search performs vector similarity search with optional filters.
func (s *LanceDBVectorStore) Search(ctx context.Context, query []float32, topK int, filter *memory.SearchFilter) ([]*memory.MemoryEntry, error) {
	// Build filter expression if needed
	filterExpr := buildFilterExpr(filter)

	var results []map[string]interface{}
	var err error

	if filterExpr != "" {
		results, err = s.table.VectorSearchWithFilter(ctx, "vector", query, topK, filterExpr)
	} else {
		results, err = s.table.VectorSearch(ctx, "vector", query, topK)
	}
	if err != nil {
		return nil, fmt.Errorf("LanceDB vector search failed: %w", err)
	}

	entries := make([]*memory.MemoryEntry, 0, len(results))
	for _, row := range results {
		entry := rowToMemoryEntry(row)
		if entry == nil {
			continue
		}
		// Post-filter min score and time range (hard to push into LanceDB SQL)
		if filter != nil {
			if filter.MinScore > 0 && entry.Score < filter.MinScore {
				continue
			}
			if filter.TimeRange != nil {
				if entry.CreatedAt.Before(filter.TimeRange.Start) || entry.CreatedAt.After(filter.TimeRange.End) {
					continue
				}
			}
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// Delete removes a memory entry by ID.
func (s *LanceDBVectorStore) Delete(ctx context.Context, id string) error {
	if err := s.table.Delete(ctx, fmt.Sprintf("id = '%s'", id)); err != nil {
		return fmt.Errorf("LanceDB delete failed: %w", err)
	}
	return nil
}

// Update modifies a memory entry (delete + re-insert).
func (s *LanceDBVectorStore) Update(ctx context.Context, entry *memory.MemoryEntry) error {
	if err := s.Delete(ctx, entry.ID); err != nil {
		s.logger.Debug("Pre-update delete failed (may not exist yet)", zap.String("id", entry.ID), zap.Error(err))
	}
	entry.UpdatedAt = time.Now()
	return s.Insert(ctx, entry)
}

// GetBySession returns all memories for a session.
func (s *LanceDBVectorStore) GetBySession(ctx context.Context, sessionID string) ([]*memory.MemoryEntry, error) {
	results, err := s.table.SelectWithFilter(ctx, fmt.Sprintf("session_id = '%s'", sessionID))
	if err != nil {
		return nil, fmt.Errorf("LanceDB session query failed: %w", err)
	}

	entries := make([]*memory.MemoryEntry, 0, len(results))
	for _, row := range results {
		if e := rowToMemoryEntry(row); e != nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// Close releases LanceDB resources.
func (s *LanceDBVectorStore) Close() error {
	if s.table != nil {
		s.table.Close()
	}
	if s.conn != nil {
		s.conn.Close()
	}
	return nil
}

// ============ internal helpers ============

func (s *LanceDBVectorStore) entryToRecord(entry *memory.MemoryEntry) (arrow.Record, error) {
	pool := arrowmem.NewGoAllocator()

	idB := array.NewStringBuilder(pool)
	idB.Append(entry.ID)
	idArr := idB.NewArray()
	defer idArr.Release()

	contentB := array.NewStringBuilder(pool)
	contentB.Append(entry.Content)
	contentArr := contentB.NewArray()
	defer contentArr.Release()

	vectorArr, err := buildVectorArray(pool, entry.Embedding, s.dimension)
	if err != nil {
		return nil, err
	}
	defer vectorArr.Release()

	metaJSON, _ := json.Marshal(entry.Metadata)
	metaB := array.NewStringBuilder(pool)
	metaB.Append(string(metaJSON))
	metaArr := metaB.NewArray()
	defer metaArr.Release()

	sessionB := array.NewStringBuilder(pool)
	sessionB.Append(entry.SessionID)
	sessionArr := sessionB.NewArray()
	defer sessionArr.Release()

	userB := array.NewStringBuilder(pool)
	userB.Append(entry.UserID)
	userArr := userB.NewArray()
	defer userArr.Release()

	createdB := array.NewInt64Builder(pool)
	createdB.Append(entry.CreatedAt.Unix())
	createdArr := createdB.NewArray()
	defer createdArr.Release()

	updatedB := array.NewInt64Builder(pool)
	updatedB.Append(entry.UpdatedAt.Unix())
	updatedArr := updatedB.NewArray()
	defer updatedArr.Release()

	cols := []arrow.Array{idArr, contentArr, vectorArr, metaArr, sessionArr, userArr, createdArr, updatedArr}
	return array.NewRecord(s.schema, cols, 1), nil
}

func buildVectorArray(pool arrowmem.Allocator, vec []float32, dim int) (arrow.Array, error) {
	if len(vec) != dim {
		return nil, fmt.Errorf("vector dimension mismatch: expected %d, got %d", dim, len(vec))
	}

	floatB := array.NewFloat32Builder(pool)
	floatB.AppendValues(vec, nil)
	floatArr := floatB.NewArray()
	defer floatArr.Release()

	listType := arrow.FixedSizeListOf(int32(dim), arrow.PrimitiveTypes.Float32)
	listData := array.NewData(listType, 1, []*arrowmem.Buffer{nil},
		[]arrow.ArrayData{floatArr.Data()}, 0, 0)
	return array.NewFixedSizeListData(listData), nil
}

func buildFilterExpr(filter *memory.SearchFilter) string {
	if filter == nil {
		return ""
	}
	var parts []string
	if filter.UserID != "" {
		parts = append(parts, fmt.Sprintf("user_id = '%s'", filter.UserID))
	}
	if filter.SessionID != "" {
		parts = append(parts, fmt.Sprintf("session_id = '%s'", filter.SessionID))
	}
	return strings.Join(parts, " AND ")
}

func rowToMemoryEntry(row map[string]interface{}) *memory.MemoryEntry {
	entry := &memory.MemoryEntry{}

	if v, ok := row["id"].(string); ok {
		entry.ID = v
	}
	if v, ok := row["content"].(string); ok {
		entry.Content = v
	}
	if v, ok := row["session_id"].(string); ok {
		entry.SessionID = v
	}
	if v, ok := row["user_id"].(string); ok {
		entry.UserID = v
	}
	if v, ok := row["metadata"].(string); ok && v != "" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(v), &meta); err == nil {
			entry.Metadata = meta
		}
	}
	if v, ok := toInt64(row["created_at"]); ok {
		entry.CreatedAt = time.Unix(v, 0)
	}
	if v, ok := toInt64(row["updated_at"]); ok {
		entry.UpdatedAt = time.Unix(v, 0)
	}
	// LanceDB returns _distance for vector search results
	if v, ok := toFloat32(row["_distance"]); ok {
		entry.Score = 1.0 / (1.0 + v) // L2 distance → [0,1] similarity
	}

	return entry
}

func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	}
	return 0, false
}

func toFloat32(v interface{}) (float32, bool) {
	switch n := v.(type) {
	case float32:
		return n, true
	case float64:
		return float32(n), true
	}
	return 0, false
}

func expandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}
