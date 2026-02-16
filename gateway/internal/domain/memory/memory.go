package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryEntry 记忆条目
type MemoryEntry struct {
	ID        string                 // 唯一标识
	Content   string                 // 内容
	Embedding []float32              // 向量嵌入
	Metadata  map[string]interface{} // 元数据
	Score     float32                // 相似度分数 (检索时填充)
	CreatedAt time.Time              // 创建时间
	UpdatedAt time.Time              // 更新时间
	SessionID string                 // 关联会话 ID
	UserID    string                 // 关联用户 ID
}

// VectorStore 向量存储接口
type VectorStore interface {
	// Insert 插入记忆
	Insert(ctx context.Context, entry *MemoryEntry) error
	// Search 语义搜索
	Search(ctx context.Context, query []float32, topK int, filter *SearchFilter) ([]*MemoryEntry, error)
	// Delete 删除记忆
	Delete(ctx context.Context, id string) error
	// Update 更新记忆
	Update(ctx context.Context, entry *MemoryEntry) error
	// GetBySession 获取会话相关记忆
	GetBySession(ctx context.Context, sessionID string) ([]*MemoryEntry, error)
}

// SearchFilter 搜索过滤器
type SearchFilter struct {
	UserID    string
	SessionID string
	MinScore  float32
	TimeRange *TimeRange
}

// TimeRange 时间范围
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// EmbeddingProvider 嵌入向量提供者接口
type EmbeddingProvider interface {
	// Embed 生成文本的嵌入向量
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedBatch 批量生成嵌入向量
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	// Dimension 返回向量维度
	Dimension() int
}

// MemoryManager 记忆管理器
type MemoryManager struct {
	store     VectorStore
	embedder  EmbeddingProvider
	mu        sync.RWMutex
}

// NewMemoryManager 创建记忆管理器
func NewMemoryManager(store VectorStore, embedder EmbeddingProvider) *MemoryManager {
	return &MemoryManager{
		store:    store,
		embedder: embedder,
	}
}

// Remember 存储新记忆
func (m *MemoryManager) Remember(ctx context.Context, content string, metadata map[string]interface{}) (*MemoryEntry, error) {
	// 生成嵌入向量
	embedding, err := m.embedder.Embed(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	// 生成 ID
	id := generateID(content)

	entry := &MemoryEntry{
		ID:        id,
		Content:   content,
		Embedding: embedding,
		Metadata:  metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 提取用户和会话信息
	if userID, ok := metadata["user_id"].(string); ok {
		entry.UserID = userID
	}
	if sessionID, ok := metadata["session_id"].(string); ok {
		entry.SessionID = sessionID
	}

	// 存储
	if err := m.store.Insert(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	return entry, nil
}

// Recall 检索相关记忆
func (m *MemoryManager) Recall(ctx context.Context, query string, topK int, filter *SearchFilter) ([]*MemoryEntry, error) {
	// 生成查询向量
	queryEmbed, err := m.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// 搜索
	results, err := m.store.Search(ctx, queryEmbed, topK, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to search memories: %w", err)
	}

	return results, nil
}

// Forget 删除记忆
func (m *MemoryManager) Forget(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}

// generateID 生成基于内容的唯一 ID
func generateID(content string) string {
	hash := sha256.Sum256([]byte(content + time.Now().String()))
	return hex.EncodeToString(hash[:16])
}

// InMemoryVectorStore 内存向量存储 (用于测试和小规模使用)
type InMemoryVectorStore struct {
	mu      sync.RWMutex
	entries map[string]*MemoryEntry
}

// NewInMemoryVectorStore 创建内存向量存储
func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		entries: make(map[string]*MemoryEntry),
	}
}

// Insert 插入记忆
func (s *InMemoryVectorStore) Insert(ctx context.Context, entry *MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[entry.ID] = entry
	return nil
}

// Search 语义搜索 (余弦相似度)
func (s *InMemoryVectorStore) Search(ctx context.Context, query []float32, topK int, filter *SearchFilter) ([]*MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		entry *MemoryEntry
		score float32
	}

	var candidates []scored

	for _, entry := range s.entries {
		// 应用过滤器
		if filter != nil {
			if filter.UserID != "" && entry.UserID != filter.UserID {
				continue
			}
			if filter.SessionID != "" && entry.SessionID != filter.SessionID {
				continue
			}
			if filter.TimeRange != nil {
				if entry.CreatedAt.Before(filter.TimeRange.Start) || entry.CreatedAt.After(filter.TimeRange.End) {
					continue
				}
			}
		}

		// 计算余弦相似度
		score := cosineSimilarity(query, entry.Embedding)

		if filter != nil && score < filter.MinScore {
			continue
		}

		candidates = append(candidates, scored{entry: entry, score: score})
	}

	// 按分数排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// 取 topK
	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	results := make([]*MemoryEntry, len(candidates))
	for i, c := range candidates {
		entryCopy := *c.entry
		entryCopy.Score = c.score
		results[i] = &entryCopy
	}

	return results, nil
}

// Delete 删除记忆
func (s *InMemoryVectorStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entries, id)
	return nil
}

// Update 更新记忆
func (s *InMemoryVectorStore) Update(ctx context.Context, entry *MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.entries[entry.ID]; !exists {
		return fmt.Errorf("memory not found: %s", entry.ID)
	}

	entry.UpdatedAt = time.Now()
	s.entries[entry.ID] = entry
	return nil
}

// GetBySession 获取会话相关记忆
func (s *InMemoryVectorStore) GetBySession(ctx context.Context, sessionID string) ([]*MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*MemoryEntry
	for _, entry := range s.entries {
		if entry.SessionID == sessionID {
			results = append(results, entry)
		}
	}
	return results, nil
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

// sqrt 简单平方根
func sqrt(x float32) float32 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// SimpleEmbedder 简单嵌入器 (用于测试，基于 TF-IDF 简化)
type SimpleEmbedder struct {
	dimension int
}

// NewSimpleEmbedder 创建简单嵌入器
func NewSimpleEmbedder(dimension int) *SimpleEmbedder {
	return &SimpleEmbedder{dimension: dimension}
}

// Embed 生成简单嵌入 (基于字符哈希)
func (e *SimpleEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	embedding := make([]float32, e.dimension)
	
	// 简单的字符级哈希
	words := strings.Fields(text)
	for _, word := range words {
		for i, char := range word {
			idx := (int(char) + i) % e.dimension
			embedding[idx] += 1.0
		}
	}

	// 归一化
	var norm float32
	for _, v := range embedding {
		norm += v * v
	}
	if norm > 0 {
		norm = sqrt(norm)
		for i := range embedding {
			embedding[i] /= norm
		}
	}

	return embedding, nil
}

// EmbedBatch 批量嵌入
func (e *SimpleEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

// Dimension 返回向量维度
func (e *SimpleEmbedder) Dimension() int {
	return e.dimension
}
