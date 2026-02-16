package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DAGNode represents a single node in a multi-agent execution graph.
type DAGNode struct {
	ID           string            // Unique node identifier
	AgentConfig  *SpawnConfig      // Agent configuration for this node
	Dependencies []string          // IDs of nodes that must complete before this one starts
	Result       string            // Output from execution
	Error        error             // Error if execution failed
	Status       DAGNodeStatus     // Current status
	Metadata     map[string]string // Additional metadata
	mu           sync.RWMutex
}

// DAGNodeStatus represents the execution state of a DAG node.
type DAGNodeStatus int

const (
	DAGNodePending DAGNodeStatus = iota
	DAGNodeRunning
	DAGNodeCompleted
	DAGNodeFailed
	DAGNodeSkipped
)

func (s DAGNodeStatus) String() string {
	switch s {
	case DAGNodePending:
		return "pending"
	case DAGNodeRunning:
		return "running"
	case DAGNodeCompleted:
		return "completed"
	case DAGNodeFailed:
		return "failed"
	case DAGNodeSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// DAGExecutor runs a Directed Acyclic Graph of agent tasks,
// executing independent nodes in parallel and respecting dependency ordering.
type DAGExecutor struct {
	spawner     Spawner
	logger      *zap.Logger
	parentID    string
	maxParallel int

	// runFn is the function that actually runs an agent.
	// Injected to decouple from AgentLoop internals.
	runFn func(ctx context.Context, agent *SpawnedAgent, input string) (string, error)
}

// DAGConfig configures a DAG-based multi-agent execution.
type DAGConfig struct {
	ParentID    string // Parent agent that owns this DAG
	MaxParallel int    // Max parallel agent executions (default: 4)
}

// NewDAGExecutor creates a new DAG executor.
func NewDAGExecutor(
	spawner Spawner,
	runFn func(ctx context.Context, agent *SpawnedAgent, input string) (string, error),
	config DAGConfig,
	logger *zap.Logger,
) *DAGExecutor {
	if config.MaxParallel <= 0 {
		config.MaxParallel = 4
	}
	return &DAGExecutor{
		spawner:     spawner,
		runFn:       runFn,
		parentID:    config.ParentID,
		maxParallel: config.MaxParallel,
		logger:      logger.With(zap.String("component", "dag-executor")),
	}
}

// Execute runs all nodes in the DAG, respecting dependencies and parallelism.
// Returns a map of node ID → result string.
func (d *DAGExecutor) Execute(ctx context.Context, nodes []*DAGNode) (map[string]string, error) {
	if err := d.validate(nodes); err != nil {
		return nil, fmt.Errorf("DAG validation failed: %w", err)
	}

	// Build lookup and dependency tracking
	nodeMap := make(map[string]*DAGNode, len(nodes))
	remaining := make(map[string]int) // nodeID → unresolved dependency count
	dependents := make(map[string][]string) // nodeID → nodes that depend on it

	for _, n := range nodes {
		nodeMap[n.ID] = n
		remaining[n.ID] = len(n.Dependencies)
		for _, depID := range n.Dependencies {
			dependents[depID] = append(dependents[depID], n.ID)
		}
	}

	// Channels for coordination
	readyCh := make(chan *DAGNode, len(nodes))
	doneCh := make(chan *DAGNode, len(nodes))
	results := make(map[string]string)
	var resultsMu sync.Mutex
	var execErr error

	// Enqueue initially ready nodes (no dependencies)
	for _, n := range nodes {
		if remaining[n.ID] == 0 {
			readyCh <- n
		}
	}

	// Semaphore for concurrency control
	sem := make(chan struct{}, d.maxParallel)

	var wg sync.WaitGroup
	completed := 0
	total := len(nodes)

	// Dispatch loop
	go func() {
		for completed < total {
			select {
			case <-ctx.Done():
				return
			case node := <-readyCh:
				wg.Add(1)
				go func(n *DAGNode) {
					defer wg.Done()

					// Acquire semaphore
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-ctx.Done():
						n.mu.Lock()
						n.Status = DAGNodeSkipped
						n.mu.Unlock()
						doneCh <- n
						return
					}

					d.executeNode(ctx, n)
					doneCh <- n
				}(node)

			case done := <-doneCh:
				completed++
				done.mu.RLock()
				status := done.Status
				result := done.Result
				done.mu.RUnlock()

				resultsMu.Lock()
				results[done.ID] = result
				resultsMu.Unlock()

				if status == DAGNodeFailed {
					// Skip dependents
					for _, depID := range dependents[done.ID] {
						if depNode, ok := nodeMap[depID]; ok {
							depNode.mu.Lock()
							depNode.Status = DAGNodeSkipped
							depNode.mu.Unlock()
							completed++
							doneCh <- depNode
						}
					}
					continue
				}

				// Unblock dependents
				for _, depID := range dependents[done.ID] {
					remaining[depID]--
					if remaining[depID] == 0 {
						readyCh <- nodeMap[depID]
					}
				}
			}
		}
	}()

	// Wait with timeout
	waitCh := make(chan struct{})
	go func() {
		for completed < total {
			time.Sleep(10 * time.Millisecond)
		}
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// All nodes completed
	case <-ctx.Done():
		execErr = ctx.Err()
	}

	return results, execErr
}

// executeNode runs a single DAG node.
func (d *DAGExecutor) executeNode(ctx context.Context, node *DAGNode) {
	node.mu.Lock()
	node.Status = DAGNodeRunning
	node.mu.Unlock()

	start := time.Now()

	// Spawn an agent for this node
	agent, err := d.spawner.Spawn(ctx, d.parentID, node.AgentConfig)
	if err != nil {
		node.mu.Lock()
		node.Status = DAGNodeFailed
		node.Error = fmt.Errorf("spawn failed: %w", err)
		node.mu.Unlock()
		d.logger.Error("DAG node spawn failed",
			zap.String("node", node.ID),
			zap.Error(err),
		)
		return
	}

	// Build input from dependency results if needed
	input := ""
	if md, ok := node.Metadata["input"]; ok {
		input = md
	}

	// Execute the agent
	result, err := d.runFn(ctx, agent, input)
	duration := time.Since(start)

	node.mu.Lock()
	if err != nil {
		node.Status = DAGNodeFailed
		node.Error = err
		node.Result = fmt.Sprintf("Error: %v", err)
	} else {
		node.Status = DAGNodeCompleted
		node.Result = result
	}
	node.mu.Unlock()

	d.logger.Info("DAG node completed",
		zap.String("node", node.ID),
		zap.String("status", node.Status.String()),
		zap.Duration("duration", duration),
	)
}

// validate checks the DAG for cycles and missing dependencies.
func (d *DAGExecutor) validate(nodes []*DAGNode) error {
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if nodeSet[n.ID] {
			return fmt.Errorf("duplicate node ID: %s", n.ID)
		}
		nodeSet[n.ID] = true
	}

	// Check for missing dependencies
	for _, n := range nodes {
		for _, depID := range n.Dependencies {
			if !nodeSet[depID] {
				return fmt.Errorf("node %s depends on missing node %s", n.ID, depID)
			}
		}
	}

	// Topological sort to detect cycles (Kahn's algorithm)
	inDegree := make(map[string]int, len(nodes))
	adj := make(map[string][]string)
	for _, n := range nodes {
		inDegree[n.ID] = len(n.Dependencies)
		for _, dep := range n.Dependencies {
			adj[dep] = append(adj[dep], n.ID)
		}
	}

	queue := make([]string, 0)
	for _, n := range nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	visited := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adj[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(nodes) {
		return fmt.Errorf("DAG contains a cycle (visited %d of %d nodes)", visited, len(nodes))
	}

	return nil
}
