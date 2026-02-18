package tool

import (
	"context"
	"encoding/json"
	"fmt"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// SkillExecutor executes skills (abstracts browser/external skill backend)
type SkillExecutor interface {
	ExecuteSkill(ctx context.Context, skillID string, input string, config map[string]string) (string, error)
}

// browserTool is a base for all browser tools that delegate to the SkillExecutor backend
type browserTool struct {
	skillExec SkillExecutor
	logger    *zap.Logger
}

// executeBrowserSkill sends a browser action to the skill executor backend
func (bt *browserTool) executeBrowserSkill(ctx context.Context, skillID string, params map[string]interface{}) (*Result, error) {
	// Guard: skill executor not connected
	if bt.skillExec == nil {
		return &Result{
			Output:  "Browser tools are unavailable: the browser gRPC service is not connected. Use web_fetch or web_search instead.",
			Display: "⚠️ 浏览器服务未连接，请用 web_fetch 替代",
			Success: false,
		}, nil
	}

	// Serialize params to JSON for the skill input
	inputBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize browser params: %w", err)
	}

	bt.logger.Info("Executing browser skill",
		zap.String("skill_id", skillID),
	)

	output, err := bt.skillExec.ExecuteSkill(ctx, skillID, string(inputBytes), nil)
	if err != nil {
		bt.logger.Error("Browser skill execution failed",
			zap.String("skill_id", skillID),
			zap.Error(err),
		)
		return &Result{
			Output:  fmt.Sprintf("Browser action failed: %s", err.Error()),
			Success: false,
		}, nil
	}

	return &Result{
		Output:  output,
		Success: true,
	}, nil
}

// BrowserNavigateTool navigates to a URL via skill executor
type BrowserNavigateTool struct {
	browserTool
}

func NewBrowserNavigateTool(skillExec SkillExecutor, logger *zap.Logger) *BrowserNavigateTool {
	return &BrowserNavigateTool{
		browserTool: browserTool{skillExec: skillExec, logger: logger},
	}
}

func (t *BrowserNavigateTool) Name() string        { return "browser_navigate" }
func (t *BrowserNavigateTool) Kind() domaintool.Kind { return domaintool.KindFetch }
func (t *BrowserNavigateTool) Description() string  { return "Navigate browser to a URL" }

func (t *BrowserNavigateTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to navigate to",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url is required")
	}

	t.logger.Info("Browser navigate", zap.String("url", url))

	return t.executeBrowserSkill(ctx, "browser_navigate", map[string]interface{}{
		"url": url,
	})
}

// BrowserScreenshotTool captures a screenshot of the current page
type BrowserScreenshotTool struct {
	browserTool
}

func NewBrowserScreenshotTool(skillExec SkillExecutor, logger *zap.Logger) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{
		browserTool: browserTool{skillExec: skillExec, logger: logger},
	}
}

func (t *BrowserScreenshotTool) Name() string        { return "browser_screenshot" }
func (t *BrowserScreenshotTool) Kind() domaintool.Kind { return domaintool.KindRead }
func (t *BrowserScreenshotTool) Description() string  { return "Take a screenshot of the current browser page" }

func (t *BrowserScreenshotTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *BrowserScreenshotTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	t.logger.Info("Browser screenshot")

	return t.executeBrowserSkill(ctx, "browser_screenshot", map[string]interface{}{})
}

// BrowserClickTool clicks an element on the page
type BrowserClickTool struct {
	browserTool
}

func NewBrowserClickTool(skillExec SkillExecutor, logger *zap.Logger) *BrowserClickTool {
	return &BrowserClickTool{
		browserTool: browserTool{skillExec: skillExec, logger: logger},
	}
}

func (t *BrowserClickTool) Name() string        { return "browser_click" }
func (t *BrowserClickTool) Kind() domaintool.Kind { return domaintool.KindExecute }
func (t *BrowserClickTool) Description() string  { return "Click an element on the page by CSS selector" }

func (t *BrowserClickTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector of element to click",
			},
		},
		"required": []string{"selector"},
	}
}

func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	selector, ok := args["selector"].(string)
	if !ok || selector == "" {
		return nil, fmt.Errorf("selector is required")
	}

	t.logger.Info("Browser click", zap.String("selector", selector))

	return t.executeBrowserSkill(ctx, "browser_click", map[string]interface{}{
		"selector": selector,
	})
}

// BrowserTypeTool types text into an element
type BrowserTypeTool struct {
	browserTool
}

func NewBrowserTypeTool(skillExec SkillExecutor, logger *zap.Logger) *BrowserTypeTool {
	return &BrowserTypeTool{
		browserTool: browserTool{skillExec: skillExec, logger: logger},
	}
}

func (t *BrowserTypeTool) Name() string        { return "browser_type" }
func (t *BrowserTypeTool) Kind() domaintool.Kind { return domaintool.KindExecute }
func (t *BrowserTypeTool) Description() string  { return "Type text into an element by CSS selector" }

func (t *BrowserTypeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector of element to type into",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type",
			},
		},
		"required": []string{"selector", "text"},
	}
}

func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]interface{}) (*Result, error) {
	selector, _ := args["selector"].(string)
	text, _ := args["text"].(string)
	if selector == "" || text == "" {
		return nil, fmt.Errorf("selector and text are required")
	}

	t.logger.Info("Browser type", zap.String("selector", selector))

	return t.executeBrowserSkill(ctx, "browser_type", map[string]interface{}{
		"selector": selector,
		"text":     text,
	})
}
