package agent

import (
	"context"
	"testing"
)

func TestSpawner(t *testing.T) {
	spawner := NewInMemorySpawner(nil, 3)

	t.Run("Spawn root agent", func(t *testing.T) {
		config := DefaultSpawnConfig("test-agent")
		config.SystemPrompt = "You are a test agent"
		config.AllowedTools = []string{"bash", "read_file"}

		agent, err := spawner.Spawn(context.Background(), "", config)
		if err != nil {
			t.Fatalf("Failed to spawn agent: %v", err)
		}

		if agent.ID == "" {
			t.Error("Agent ID should not be empty")
		}
		if agent.Name != "test-agent" {
			t.Errorf("Agent name = %s, want test-agent", agent.Name)
		}
		if agent.Depth != 1 {
			t.Errorf("Root agent depth = %d, want 1", agent.Depth)
		}
		if agent.Status != AgentStatusIdle {
			t.Errorf("Initial status = %v, want idle", agent.Status)
		}
	})

	t.Run("Spawn child agent", func(t *testing.T) {
		// First create parent
		parentConfig := DefaultSpawnConfig("parent")
		parentConfig.AllowedTools = []string{"bash", "read_file", "write_file"}
		parent, _ := spawner.Spawn(context.Background(), "", parentConfig)

		// Then create child
		childConfig := DefaultSpawnConfig("child")
		childConfig.InheritTools = true
		childConfig.AllowedTools = []string{"grep_search"}

		child, err := spawner.Spawn(context.Background(), parent.ID, childConfig)
		if err != nil {
			t.Fatalf("Failed to spawn child: %v", err)
		}

		if child.ParentID != parent.ID {
			t.Errorf("Child parent ID = %s, want %s", child.ParentID, parent.ID)
		}
		if child.Depth != 2 {
			t.Errorf("Child depth = %d, want 2", child.Depth)
		}

		// Check inheritance
		children := spawner.ListChildren(parent.ID)
		if len(children) != 1 {
			t.Errorf("Parent should have 1 child, got %d", len(children))
		}
	})

	t.Run("Max depth limit", func(t *testing.T) {
		// Create chain of agents up to max depth
		var currentID string
		for i := 0; i < 3; i++ {
			config := DefaultSpawnConfig("level-agent")
			agent, err := spawner.Spawn(context.Background(), currentID, config)
			if err != nil {
				t.Fatalf("Failed to spawn at depth %d: %v", i+1, err)
			}
			currentID = agent.ID
		}

		// Try to exceed max depth
		config := DefaultSpawnConfig("too-deep")
		_, err := spawner.Spawn(context.Background(), currentID, config)
		if err == nil {
			t.Error("Should fail when exceeding max depth")
		}
	})

	t.Run("Terminate agent", func(t *testing.T) {
		config := DefaultSpawnConfig("to-terminate")
		agent, _ := spawner.Spawn(context.Background(), "", config)

		err := spawner.Terminate(agent.ID)
		if err != nil {
			t.Fatalf("Failed to terminate: %v", err)
		}

		if agent.GetStatus() != AgentStatusTerminated {
			t.Error("Agent should be terminated")
		}
	})

	t.Run("Terminate with children", func(t *testing.T) {
		parentConfig := DefaultSpawnConfig("parent-term")
		parent, _ := spawner.Spawn(context.Background(), "", parentConfig)

		childConfig := DefaultSpawnConfig("child-term")
		child, _ := spawner.Spawn(context.Background(), parent.ID, childConfig)

		// Terminate parent should also terminate children
		spawner.Terminate(parent.ID)

		if child.GetStatus() != AgentStatusTerminated {
			t.Error("Child should be terminated when parent is terminated")
		}
	})
}

func TestPermission(t *testing.T) {
	t.Run("CanUseTool with empty lists", func(t *testing.T) {
		perm := &Permission{
			Tools:       []string{},
			DeniedTools: []string{},
		}

		if !perm.CanUseTool("any_tool") {
			t.Error("Empty lists should allow any tool")
		}
	})

	t.Run("CanUseTool with allow list", func(t *testing.T) {
		perm := &Permission{
			Tools:       []string{"bash", "read_file"},
			DeniedTools: []string{},
		}

		if !perm.CanUseTool("bash") {
			t.Error("Should allow bash")
		}
		if perm.CanUseTool("write_file") {
			t.Error("Should not allow write_file")
		}
	})

	t.Run("CanUseTool with deny list", func(t *testing.T) {
		perm := &Permission{
			Tools:       []string{},
			DeniedTools: []string{"rm", "sudo"},
		}

		if perm.CanUseTool("rm") {
			t.Error("Should deny rm")
		}
		if !perm.CanUseTool("ls") {
			t.Error("Should allow ls")
		}
	})

	t.Run("Deny takes precedence", func(t *testing.T) {
		perm := &Permission{
			Tools:       []string{"bash", "rm"},
			DeniedTools: []string{"rm"},
		}

		if perm.CanUseTool("rm") {
			t.Error("Deny should take precedence over allow")
		}
	})
}

func TestAgentStatus(t *testing.T) {
	tests := []struct {
		status AgentStatus
		want   string
	}{
		{AgentStatusIdle, "idle"},
		{AgentStatusRunning, "running"},
		{AgentStatusCompleted, "completed"},
		{AgentStatusError, "error"},
		{AgentStatusTerminated, "terminated"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpawnedAgentConcurrency(t *testing.T) {
	spawner := NewInMemorySpawner(nil, 10)
	
	// Spawn multiple agents concurrently
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			config := DefaultSpawnConfig("concurrent-agent")
			_, err := spawner.Spawn(context.Background(), "", config)
			if err != nil {
				t.Errorf("Concurrent spawn %d failed: %v", idx, err)
			}
			done <- struct{}{}
		}(i)
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}
}
