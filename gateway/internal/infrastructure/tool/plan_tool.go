// Copyright 2026 NGOClaw Authors. All rights reserved.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// PlanStatus represents the execution state of a plan step.
type PlanStatus string

const (
	PlanStatusPending    PlanStatus = "pending"
	PlanStatusInProgress PlanStatus = "in_progress"
	PlanStatusDone       PlanStatus = "done"
	PlanStatusError      PlanStatus = "error"
	PlanStatusSkipped    PlanStatus = "skipped"
)

// PlanStep represents a single step in the execution plan.
type PlanStep struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Status    PlanStatus `json:"status"`
	Notes     string     `json:"notes,omitempty"`
	UpdatedAt string     `json:"updatedAt"`
}

// Plan represents the full execution plan.
type Plan struct {
	Goal      string     `json:"goal"`
	Steps     []PlanStep `json:"steps"`
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`
}

// UpdatePlanTool allows the agent to create and update execution plans.
// Source: Deer-Flow TodoList pattern — agents report progress via tool calls.
//
// Plan files are stored per-session at ~/.ngoclaw/plans/<session>.json.
type UpdatePlanTool struct {
	mu     sync.Mutex
	logger *zap.Logger
}

// NewUpdatePlanTool creates the update_plan tool.
func NewUpdatePlanTool(logger *zap.Logger) *UpdatePlanTool {
	return &UpdatePlanTool{logger: logger}
}

func (t *UpdatePlanTool) Name() string         { return "update_plan" }
func (t *UpdatePlanTool) Kind() domaintool.Kind { return domaintool.KindThink }
func (t *UpdatePlanTool) Description() string {
	return "Create or update the execution plan. " +
		"Use action='create' with steps to start a new plan; " +
		"action='update' with step_id and status to mark progress."
}

func (t *UpdatePlanTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: 'create' to create a new plan, 'update' to update a step status.",
				"enum":        []string{"create", "update"},
			},
			"goal": map[string]interface{}{
				"type":        "string",
				"description": "Goal of the plan (required for 'create').",
			},
			"steps": map[string]interface{}{
				"type":        "array",
				"description": "List of step titles (required for 'create').",
				"items":       map[string]interface{}{"type": "string"},
			},
			"step_id": map[string]interface{}{
				"type":        "number",
				"description": "Step ID to update (required for 'update', 1-indexed).",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "New status for the step.",
				"enum":        []string{"pending", "in_progress", "done", "error", "skipped"},
			},
			"notes": map[string]interface{}{
				"type":        "string",
				"description": "Optional notes for the step update.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *UpdatePlanTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	action, _ := args["action"].(string)

	switch action {
	case "create":
		return t.createPlan(args)
	case "update":
		return t.updateStep(args)
	default:
		return &Result{Output: "Error: action must be 'create' or 'update'", Success: false}, nil
	}
}

func (t *UpdatePlanTool) createPlan(args map[string]interface{}) (*Result, error) {
	goal, _ := args["goal"].(string)
	if goal == "" {
		return &Result{Output: "Error: 'goal' is required for create", Success: false}, nil
	}

	rawSteps, ok := args["steps"].([]interface{})
	if !ok || len(rawSteps) == 0 {
		return &Result{Output: "Error: 'steps' array is required for create", Success: false}, nil
	}

	now := time.Now().Format(time.RFC3339)
	plan := Plan{
		Goal:      goal,
		Steps:     make([]PlanStep, len(rawSteps)),
		CreatedAt: now,
		UpdatedAt: now,
	}

	for i, s := range rawSteps {
		title := fmt.Sprintf("%v", s)
		plan.Steps[i] = PlanStep{
			ID:        i + 1,
			Title:     title,
			Status:    PlanStatusPending,
			UpdatedAt: now,
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.savePlan(&plan); err != nil {
		return &Result{Output: fmt.Sprintf("Failed to save plan: %v", err), Success: false}, nil
	}

	t.logger.Info("Plan created",
		zap.String("goal", goal),
		zap.Int("steps", len(plan.Steps)),
	)

	return &Result{
		Output:  fmt.Sprintf("Plan created: \"%s\" with %d steps", goal, len(plan.Steps)),
		Display: t.renderPlan(&plan),
		Success: true,
	}, nil
}

func (t *UpdatePlanTool) updateStep(args map[string]interface{}) (*Result, error) {
	stepID, ok := args["step_id"].(float64)
	if !ok || stepID < 1 {
		return &Result{Output: "Error: 'step_id' (1-indexed) is required", Success: false}, nil
	}

	statusStr, _ := args["status"].(string)
	if statusStr == "" {
		return &Result{Output: "Error: 'status' is required", Success: false}, nil
	}
	status := PlanStatus(statusStr)

	t.mu.Lock()
	defer t.mu.Unlock()

	plan, err := t.loadPlan()
	if err != nil || plan == nil {
		return &Result{Output: "Error: no active plan found. Use action='create' first.", Success: false}, nil
	}

	idx := int(stepID) - 1
	if idx < 0 || idx >= len(plan.Steps) {
		return &Result{Output: fmt.Sprintf("Error: step_id %d out of range (1-%d)", int(stepID), len(plan.Steps)), Success: false}, nil
	}

	plan.Steps[idx].Status = status
	plan.Steps[idx].UpdatedAt = time.Now().Format(time.RFC3339)
	if notes, ok := args["notes"].(string); ok && notes != "" {
		plan.Steps[idx].Notes = notes
	}
	plan.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := t.savePlan(plan); err != nil {
		return &Result{Output: fmt.Sprintf("Failed to save plan: %v", err), Success: false}, nil
	}

	t.logger.Info("Plan step updated",
		zap.Int("step", int(stepID)),
		zap.String("status", statusStr),
	)

	return &Result{
		Output:  fmt.Sprintf("Step %d → %s", int(stepID), statusStr),
		Display: t.renderPlan(plan),
		Success: true,
	}, nil
}

// --- Plan I/O ---

func (t *UpdatePlanTool) planPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ngoclaw", "current_plan.json")
}

func (t *UpdatePlanTool) loadPlan() (*Plan, error) {
	data, err := os.ReadFile(t.planPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (t *UpdatePlanTool) savePlan(plan *Plan) error {
	path := t.planPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// renderPlan creates a visual representation of the plan for display.
func (t *UpdatePlanTool) renderPlan(plan *Plan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 **%s**\n", plan.Goal))

	doneCount := 0
	for _, s := range plan.Steps {
		var icon string
		switch s.Status {
		case PlanStatusDone:
			icon = "✅"
			doneCount++
		case PlanStatusInProgress:
			icon = "🔄"
		case PlanStatusError:
			icon = "❌"
		case PlanStatusSkipped:
			icon = "⏭️"
			doneCount++
		default:
			icon = "⬜"
		}
		line := fmt.Sprintf("%s %d. %s", icon, s.ID, s.Title)
		if s.Notes != "" {
			line += fmt.Sprintf(" (%s)", s.Notes)
		}
		sb.WriteString(line + "\n")
	}

	progress := float64(doneCount) / float64(len(plan.Steps)) * 100
	sb.WriteString(fmt.Sprintf("\n📊 Progress: %.0f%%", progress))

	return sb.String()
}

// LoadCurrentPlan loads the active plan (for prompt injection and display).
func LoadCurrentPlan() (*Plan, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".ngoclaw", "current_plan.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}
