package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MiniApp 生成的 Telegram Mini App
type MiniApp struct {
	ID          string    // 唯一标识
	Title       string    // 应用标题
	Description string    // 应用描述
	HTML        string    // 完整 HTML (单文件, 内联 CSS+JS)
	URL         string    // 访问地址
	ChatID      int64     // 所属聊天
	UserID      int64     // 创建者
	CreatedAt   time.Time // 创建时间
}

// MiniAppGenerator AI 驱动的 Mini App 生成器
type MiniAppGenerator struct {
	aiClient    MiniAppAIClient
	baseURL     string // HTTP 服务器基础 URL
	apps        map[string]*MiniApp
	mu          sync.RWMutex
	logger      *zap.Logger
}

// MiniAppAIClient AI 客户端接口 (用于生成 HTML)
type MiniAppAIClient interface {
	GenerateHTML(ctx context.Context, prompt string) (string, error)
}

// NewMiniAppGenerator 创建 Mini App 生成器
func NewMiniAppGenerator(aiClient MiniAppAIClient, baseURL string, logger *zap.Logger) *MiniAppGenerator {
	return &MiniAppGenerator{
		aiClient: aiClient,
		baseURL:  strings.TrimRight(baseURL, "/"),
		apps:     make(map[string]*MiniApp),
		logger:   logger,
	}
}

// Generate 根据用户描述生成 Mini App
func (g *MiniAppGenerator) Generate(ctx context.Context, chatID int64, userID int64, description string) (*MiniApp, error) {
	id := generateID()

	// 构建 prompt: 要求 AI 生成完整的单文件 HTML
	prompt := g.buildPrompt(description)

	g.logger.Info("Generating Mini App",
		zap.String("id", id),
		zap.String("description", description),
		zap.Int64("chat_id", chatID),
	)

	html, err := g.aiClient.GenerateHTML(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI 生成 HTML 失败: %w", err)
	}

	// 提取 HTML (AI 可能包含 markdown 代码块标记)
	html = extractHTML(html)

	// 注入 Telegram WebApp SDK
	html = injectTelegramSDK(html)

	app := &MiniApp{
		ID:          id,
		Title:       extractTitle(description),
		Description: description,
		HTML:        html,
		URL:         fmt.Sprintf("%s/miniapp/%s", g.baseURL, id),
		ChatID:      chatID,
		UserID:      userID,
		CreatedAt:   time.Now(),
	}

	g.mu.Lock()
	g.apps[id] = app
	g.mu.Unlock()

	g.logger.Info("Mini App generated",
		zap.String("id", id),
		zap.String("title", app.Title),
		zap.Int("html_bytes", len(html)),
		zap.String("url", app.URL),
	)

	return app, nil
}

// GetApp 获取 Mini App (用于 HTTP 路由)
func (g *MiniAppGenerator) GetApp(id string) (*MiniApp, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	app, exists := g.apps[id]
	return app, exists
}

// ListApps 获取指定 chat 的所有 Mini App
func (g *MiniAppGenerator) ListApps(chatID int64) []*MiniApp {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*MiniApp
	for _, app := range g.apps {
		if app.ChatID == chatID {
			result = append(result, app)
		}
	}
	return result
}

// DeleteApp 删除 Mini App
func (g *MiniAppGenerator) DeleteApp(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.apps[id]; exists {
		delete(g.apps, id)
		return true
	}
	return false
}

// ─────────────────── 内部方法 ───────────────────

func (g *MiniAppGenerator) buildPrompt(description string) string {
	return fmt.Sprintf(`Generate a complete, single-file HTML web application for Telegram Mini App.

User request: %s

Requirements:
1. Single HTML file with inline CSS and JavaScript
2. Modern design: dark theme, rounded corners, smooth animations
3. Responsive layout that works in Telegram's WebView
4. Use CSS variables for theming
5. Include touch-friendly interactions
6. Use vanilla HTML/CSS/JS only (no frameworks)
7. The app should be fully functional, not a mockup
8. Include proper viewport meta tag for mobile
9. Use modern fonts (system font stack)
10. Add a subtle gradient background

Output ONLY the HTML code, nothing else.`, description)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func extractHTML(content string) string {
	// AI 可能返回 markdown 包裹的 HTML
	if idx := strings.Index(content, "```html"); idx != -1 {
		content = content[idx+7:]
		if end := strings.LastIndex(content, "```"); end != -1 {
			content = content[:end]
		}
	} else if idx := strings.Index(content, "```"); idx != -1 {
		content = content[idx+3:]
		if end := strings.LastIndex(content, "```"); end != -1 {
			content = content[:end]
		}
	}
	return strings.TrimSpace(content)
}

func extractTitle(description string) string {
	// 取描述的前 30 个字符作为标题
	title := description
	if len(title) > 30 {
		title = title[:30] + "..."
	}
	return title
}

func injectTelegramSDK(html string) string {
	sdkScript := `<script src="https://telegram.org/js/telegram-web-app.js"></script>
<script>
  // Initialize Telegram WebApp
  const tg = window.Telegram.WebApp;
  tg.ready();
  tg.expand();
  
  // Apply Telegram theme
  document.documentElement.style.setProperty('--tg-theme-bg-color', tg.themeParams.bg_color || '#1a1a2e');
  document.documentElement.style.setProperty('--tg-theme-text-color', tg.themeParams.text_color || '#e0e0e0');
  document.documentElement.style.setProperty('--tg-theme-button-color', tg.themeParams.button_color || '#6c5ce7');
  document.documentElement.style.setProperty('--tg-theme-button-text-color', tg.themeParams.button_text_color || '#ffffff');
</script>`

	// 在 </head> 前注入, 或在 <html> 后注入
	if idx := strings.Index(html, "</head>"); idx != -1 {
		return html[:idx] + sdkScript + "\n" + html[idx:]
	}
	if idx := strings.Index(html, "<body"); idx != -1 {
		return html[:idx] + sdkScript + "\n" + html[idx:]
	}
	// 降级: 直接追加到开头
	return sdkScript + "\n" + html
}
