package application

import (
	"strings"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/domain/service"
	toolpkg "github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/tool"
)

// memoryPersisterAdapter bridges service.MemoryPersister → toolpkg.MemoryStore
type memoryPersisterAdapter struct{}

func (m *memoryPersisterAdapter) SaveFact(content, category string, confidence float64, source string) error {
	store, err := toolpkg.LoadMemoryStore()
	if err != nil {
		return err
	}
	store.Facts = append(store.Facts, toolpkg.MemoryFact{
		ID:         time.Now().Format("20060102150405")[6:],
		Content:    content,
		Category:   category,
		Confidence: confidence,
		Source:     source,
		CreatedAt:  time.Now().Format(time.RFC3339),
	})
	return toolpkg.SaveMemoryStore(store)
}

func (m *memoryPersisterAdapter) IsDuplicate(content string) bool {
	store, err := toolpkg.LoadMemoryStore()
	if err != nil {
		return false
	}
	lower := strings.ToLower(content)
	for _, f := range store.Facts {
		if strings.ToLower(f.Content) == lower {
			return true
		}
	}
	return false
}

// Compile-time check
var _ service.MemoryPersister = (*memoryPersisterAdapter)(nil)
