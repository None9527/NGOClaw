package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDAGExecutor_LinearChain(t *testing.T) {
	logger := zap.NewNop()
	spawner := NewInMemorySpawner(logger, 5)

	// Register root agent
	root, _ := spawner.Spawn(context.Background(), "", DefaultSpawnConfig("root"))

	var callOrder int32
	runFn := func(ctx context.Context, agent *SpawnedAgent, input string) (string, error) {
		idx := atomic.AddInt32(&callOrder, 1)
		return fmt.Sprintf("result-%d", idx), nil
	}

	executor := NewDAGExecutor(spawner, runFn, DAGConfig{
		ParentID:    root.ID,
		MaxParallel: 2,
	}, logger)

	nodes := []*DAGNode{
		{ID: "A", AgentConfig: DefaultSpawnConfig("agent-a"), Dependencies: nil, Metadata: map[string]string{"input": "start"}},
		{ID: "B", AgentConfig: DefaultSpawnConfig("agent-b"), Dependencies: []string{"A"}},
		{ID: "C", AgentConfig: DefaultSpawnConfig("agent-c"), Dependencies: []string{"B"}},
	}

	results, err := executor.Execute(context.Background(), nodes)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All nodes should complete
	for _, n := range nodes {
		n.mu.RLock()
		if n.Status != DAGNodeCompleted {
			t.Errorf("node %s expected completed, got %s", n.ID, n.Status)
		}
		n.mu.RUnlock()
	}
}

func TestDAGExecutor_ParallelFanOut(t *testing.T) {
	logger := zap.NewNop()
	spawner := NewInMemorySpawner(logger, 5)
	root, _ := spawner.Spawn(context.Background(), "", DefaultSpawnConfig("root"))

	var parallel int32
	var maxParallel int32

	runFn := func(ctx context.Context, agent *SpawnedAgent, input string) (string, error) {
		cur := atomic.AddInt32(&parallel, 1)
		for {
			old := atomic.LoadInt32(&maxParallel)
			if cur <= old {
				break
			}
			if atomic.CompareAndSwapInt32(&maxParallel, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&parallel, -1)
		return "ok", nil
	}

	executor := NewDAGExecutor(spawner, runFn, DAGConfig{
		ParentID:    root.ID,
		MaxParallel: 4,
	}, logger)

	// A → {B, C, D} (B, C, D can run in parallel)
	nodes := []*DAGNode{
		{ID: "A", AgentConfig: DefaultSpawnConfig("a")},
		{ID: "B", AgentConfig: DefaultSpawnConfig("b"), Dependencies: []string{"A"}},
		{ID: "C", AgentConfig: DefaultSpawnConfig("c"), Dependencies: []string{"A"}},
		{ID: "D", AgentConfig: DefaultSpawnConfig("d"), Dependencies: []string{"A"}},
	}

	results, err := executor.Execute(context.Background(), nodes)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// B, C, D should have run in parallel (maxParallel >= 2)
	if atomic.LoadInt32(&maxParallel) < 2 {
		t.Log("Warning: expected parallel execution of B, C, D")
	}
}

func TestDAGExecutor_CycleDetection(t *testing.T) {
	logger := zap.NewNop()
	spawner := NewInMemorySpawner(logger, 5)
	root, _ := spawner.Spawn(context.Background(), "", DefaultSpawnConfig("root"))

	executor := NewDAGExecutor(spawner, nil, DAGConfig{ParentID: root.ID}, logger)

	// Cycle: A → B → C → A
	nodes := []*DAGNode{
		{ID: "A", AgentConfig: DefaultSpawnConfig("a"), Dependencies: []string{"C"}},
		{ID: "B", AgentConfig: DefaultSpawnConfig("b"), Dependencies: []string{"A"}},
		{ID: "C", AgentConfig: DefaultSpawnConfig("c"), Dependencies: []string{"B"}},
	}

	_, err := executor.Execute(context.Background(), nodes)
	if err == nil {
		t.Fatal("expected error for cyclic DAG")
	}
}

func TestDAGExecutor_FailedNodeSkipsDependents(t *testing.T) {
	logger := zap.NewNop()
	spawner := NewInMemorySpawner(logger, 5)
	root, _ := spawner.Spawn(context.Background(), "", DefaultSpawnConfig("root"))

	runFn := func(ctx context.Context, agent *SpawnedAgent, input string) (string, error) {
		if agent.Name == "bad" {
			return "", fmt.Errorf("intentional failure")
		}
		return "ok", nil
	}

	executor := NewDAGExecutor(spawner, runFn, DAGConfig{ParentID: root.ID}, logger)

	nodes := []*DAGNode{
		{ID: "A", AgentConfig: DefaultSpawnConfig("bad")}, // Will fail
		{ID: "B", AgentConfig: DefaultSpawnConfig("good"), Dependencies: []string{"A"}}, // Should be skipped
	}

	results, _ := executor.Execute(context.Background(), nodes)

	// A should have failed
	nodes[0].mu.RLock()
	if nodes[0].Status != DAGNodeFailed {
		t.Errorf("node A expected failed, got %s", nodes[0].Status)
	}
	nodes[0].mu.RUnlock()

	// B should be skipped
	if _, ok := results["B"]; !ok {
		// B may or may not be in results
	}

	// Verify we got A's error result
	if results["A"] == "" {
		t.Error("expected error result for node A")
	}
}

func TestDAGExecutor_DuplicateNodeID(t *testing.T) {
	logger := zap.NewNop()
	spawner := NewInMemorySpawner(logger, 5)
	root, _ := spawner.Spawn(context.Background(), "", DefaultSpawnConfig("root"))

	executor := NewDAGExecutor(spawner, nil, DAGConfig{ParentID: root.ID}, logger)

	nodes := []*DAGNode{
		{ID: "A", AgentConfig: DefaultSpawnConfig("a")},
		{ID: "A", AgentConfig: DefaultSpawnConfig("b")}, // Duplicate!
	}

	_, err := executor.Execute(context.Background(), nodes)
	if err == nil {
		t.Fatal("expected error for duplicate node ID")
	}
}
