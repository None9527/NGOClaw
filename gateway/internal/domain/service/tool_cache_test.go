package service

import (
	"context"
	"testing"
	"time"
)

// === ToolResultCache Tests ===

func TestToolCache_PutGet(t *testing.T) {
	cache := NewToolResultCache(5*time.Second, 100)

	args := map[string]interface{}{"path": "main.go"}
	cache.Put("read_file", args, "file contents", true)

	output, success, hit := cache.Get("read_file", args)
	if !hit {
		t.Fatal("expected cache hit")
	}
	if output != "file contents" {
		t.Fatalf("expected 'file contents', got %q", output)
	}
	if !success {
		t.Fatal("expected success=true")
	}
}

func TestToolCache_Miss(t *testing.T) {
	cache := NewToolResultCache(5*time.Second, 100)

	_, _, hit := cache.Get("read_file", map[string]interface{}{"path": "missing"})
	if hit {
		t.Fatal("expected cache miss")
	}
}

func TestToolCache_TTLExpiry(t *testing.T) {
	cache := NewToolResultCache(10*time.Millisecond, 100)

	args := map[string]interface{}{"x": 1}
	cache.Put("test_tool", args, "result", true)

	// Should hit immediately
	_, _, hit := cache.Get("test_tool", args)
	if !hit {
		t.Fatal("expected cache hit before expiry")
	}

	// Wait for TTL
	time.Sleep(15 * time.Millisecond)

	_, _, hit = cache.Get("test_tool", args)
	if hit {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestToolCache_MaxSizeEviction(t *testing.T) {
	cache := NewToolResultCache(5*time.Second, 3) // max 3 entries

	cache.Put("tool1", nil, "r1", true)
	time.Sleep(time.Millisecond)
	cache.Put("tool2", nil, "r2", true)
	time.Sleep(time.Millisecond)
	cache.Put("tool3", nil, "r3", true)
	time.Sleep(time.Millisecond)

	if cache.Size() != 3 {
		t.Fatalf("expected 3 entries, got %d", cache.Size())
	}

	// Adding 4th should evict oldest (tool1)
	cache.Put("tool4", nil, "r4", true)
	if cache.Size() != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", cache.Size())
	}

	// tool1 should be evicted
	_, _, hit := cache.Get("tool1", nil)
	if hit {
		t.Fatal("tool1 should have been evicted")
	}

	// tool4 should be present
	output, _, hit := cache.Get("tool4", nil)
	if !hit {
		t.Fatal("tool4 should be present")
	}
	if output != "r4" {
		t.Fatalf("expected 'r4', got %q", output)
	}
}

func TestToolCache_Clear(t *testing.T) {
	cache := NewToolResultCache(5*time.Second, 100)
	cache.Put("tool", nil, "result", true)

	if cache.Size() != 1 {
		t.Fatal("expected 1 entry")
	}

	cache.Clear()
	if cache.Size() != 0 {
		t.Fatal("expected 0 entries after clear")
	}
}

func TestToolCache_DifferentArgs(t *testing.T) {
	cache := NewToolResultCache(5*time.Second, 100)

	args1 := map[string]interface{}{"path": "a.go"}
	args2 := map[string]interface{}{"path": "b.go"}

	cache.Put("read_file", args1, "content_a", true)
	cache.Put("read_file", args2, "content_b", true)

	output1, _, hit1 := cache.Get("read_file", args1)
	if !hit1 || output1 != "content_a" {
		t.Fatalf("expected 'content_a', got %q (hit=%v)", output1, hit1)
	}

	output2, _, hit2 := cache.Get("read_file", args2)
	if !hit2 || output2 != "content_b" {
		t.Fatalf("expected 'content_b', got %q (hit=%v)", output2, hit2)
	}
}

// === TraceID Tests ===

func TestTraceID_WithAndFrom(t *testing.T) {
	ctx := context.Background()

	// No trace ID by default
	if id := TraceIDFromContext(ctx); id != "" {
		t.Fatalf("expected empty trace ID, got %q", id)
	}

	// Inject a trace ID
	ctx = WithTraceID(ctx, "test-trace-123")
	if id := TraceIDFromContext(ctx); id != "test-trace-123" {
		t.Fatalf("expected 'test-trace-123', got %q", id)
	}
}

func TestTraceID_AutoGenerate(t *testing.T) {
	ctx := WithTraceID(context.Background(), "")
	id := TraceIDFromContext(ctx)

	if id == "" {
		t.Fatal("expected auto-generated trace ID")
	}
	if len(id) != 16 {
		t.Fatalf("expected 16-char trace ID, got %d chars: %q", len(id), id)
	}
}

func TestTraceID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		ctx := WithTraceID(context.Background(), "")
		id := TraceIDFromContext(ctx)
		if ids[id] {
			t.Fatalf("duplicate trace ID generated: %q", id)
		}
		ids[id] = true
	}
}
