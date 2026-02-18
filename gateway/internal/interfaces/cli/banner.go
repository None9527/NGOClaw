package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/charmbracelet/lipgloss"
)

const appVersion = "0.2.0"

// brand colors
var (
	colorCyan    = lipgloss.Color("#00D7FF")
	colorDimCyan = lipgloss.Color("#00AFAF")
	colorGray    = lipgloss.Color("#6C6C6C")
	colorWhite   = lipgloss.Color("#FFFFFF")
	colorDim     = lipgloss.Color("#4E4E4E")
	colorGreen   = lipgloss.Color("#00FF87")
	colorYellow  = lipgloss.Color("#FFD75F")
)

// Logo lines — clean block font, no box-drawing corners
var logoLines = []string{
	" ██  ██   ██████   ██████   █████  ██       █████  ██     ██",
	" ███ ██  ██       ██   ██  ██      ██      ██   ██ ██     ██",
	" ██████  ██  ███  ██   ██  ██      ██      ███████ ██  █  ██",
	" ██ ███  ██   ██  ██   ██  ██      ██      ██   ██ ██ ███ ██",
	" ██  ██   ██████   ██████   █████  ██████  ██   ██  ███ ███ ",
}

// Gradient colors top→bottom (cyan → blue → violet)
var logoGradient = []lipgloss.Color{
	lipgloss.Color("#00FFFF"),
	lipgloss.Color("#00CFFF"),
	lipgloss.Color("#009FFF"),
	lipgloss.Color("#006FFF"),
	lipgloss.Color("#5F5FFF"),
}

// BannerInfo carries dynamic stats shown in the welcome banner
type BannerInfo struct {
	Model      string
	ToolCount  int
	MCPServers int
	Workspace  string
	ProjectLng string
}

// DetectProjectLanguage scans cwd for known project markers
func DetectProjectLanguage(dir string) string {
	markers := []struct {
		file string
		lang string
	}{
		{"go.mod", "Go"},
		{"Cargo.toml", "Rust"},
		{"package.json", "Node.js"},
		{"pyproject.toml", "Python"},
		{"requirements.txt", "Python"},
		{"pom.xml", "Java"},
		{"build.gradle", "Java"},
		{"Gemfile", "Ruby"},
		{"mix.exs", "Elixir"},
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m.file)); err == nil {
			return m.lang
		}
	}
	return ""
}

// RenderBanner returns the styled welcome banner with gradient logo
func RenderBanner(info BannerInfo, width int) string {
	labelStyle := lipgloss.NewStyle().Foreground(colorGray)
	valueStyle := lipgloss.NewStyle().Foreground(colorWhite)
	tipStyle := lipgloss.NewStyle().Foreground(colorDim)
	greenStyle := lipgloss.NewStyle().Foreground(colorGreen)
	versionStyle := lipgloss.NewStyle().Foreground(colorDimCyan)

	// Render gradient logo
	var logo string
	if width >= 62 {
		for i, line := range logoLines {
			c := logoGradient[i%len(logoGradient)]
			logo += lipgloss.NewStyle().Foreground(c).Bold(true).Render(line) + "\n"
		}
	} else {
		// Compact fallback
		logo = lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(" ◇  N G O C L A W") + "\n"
	}

	ver := versionStyle.Render(fmt.Sprintf("  v%s", appVersion))

	// Stats
	modelLine := fmt.Sprintf("  %s %s",
		labelStyle.Render("Model"),
		valueStyle.Render(info.Model),
	)
	toolsLine := fmt.Sprintf("  %s %s",
		labelStyle.Render("Tools"),
		greenStyle.Render(fmt.Sprintf("%d loaded", info.ToolCount)),
	)

	ws := info.Workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}
	projectDesc := ws
	if info.ProjectLng != "" {
		projectDesc += fmt.Sprintf(" (%s)", info.ProjectLng)
	}
	projectLine := fmt.Sprintf("  %s %s",
		labelStyle.Render("Path "),
		valueStyle.Render(projectDesc),
	)
	envLine := fmt.Sprintf("  %s %s/%s",
		labelStyle.Render("Env  "),
		labelStyle.Render(runtime.GOOS),
		labelStyle.Render(runtime.GOARCH),
	)

	tips := tipStyle.Render("  Enter 提问 · /help 命令 · Ctrl+C 中断")

	return fmt.Sprintf("\n%s%s\n\n%s\n%s\n%s\n%s\n\n%s\n",
		logo, ver,
		modelLine, toolsLine, projectLine, envLine,
		tips,
	)
}
