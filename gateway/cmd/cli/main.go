package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/config"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/logger"
	"github.com/ngoclaw/ngoclaw/gateway/internal/interfaces/cli"
)

const (
	cliVersion = "0.2.0"
	cliName    = "ngoclaw"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   cliName + " [message]",
		Short: "NGOClaw — AI Coding Agent",
		Long:  "NGOClaw CLI — 交互式 AI 编程助手, 支持代码生成/编辑/调试/搜索",
		Args:  cobra.ArbitraryArgs,
		RunE:  runInteractive,
	}

	rootCmd.Flags().StringP("model", "m", "", "指定模型 (覆盖配置)")
	rootCmd.Flags().BoolP("no-approve", "y", false, "跳过工具审批 (YOLO 模式)")
	rootCmd.Flags().StringP("workspace", "w", "", "工作目录")

	// --- Subcommands ---

	rootCmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "启动完整网关服务 (HTTP + Telegram + gRPC)",
		Long:  "启动 NGOClaw Gateway 全量服务, 包含 HTTP API、Telegram Bot、gRPC Agent Server",
		RunE:  runServe,
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "显示版本",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s v%s\n", cliName, cliVersion)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "环境诊断",
		RunE:  runDoctor,
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ─── CLI Interactive Mode (default) ───

func runInteractive(cmd *cobra.Command, args []string) error {
	// Quiet logger for CLI
	log, err := logger.NewLogger(logger.Config{
		Level:      "error",
		Format:     "console",
		OutputPath: "/dev/null",
	})
	if err != nil {
		return fmt.Errorf("logger init: %w", err)
	}
	defer log.Sync()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// CLI flag overrides
	if m, _ := cmd.Flags().GetString("model"); m != "" {
		cfg.Agent.DefaultModel = m
	}
	// Workspace: always use CWD (where user launched ngoclaw)
	// --workspace flag overrides CWD; config workspace is for gateway mode only
	workspace, _ := os.Getwd()
	if w, _ := cmd.Flags().GetString("workspace"); w != "" {
		workspace = w
	}
	noApprove, _ := cmd.Flags().GetBool("no-approve")

	// Init app (CLI mode — no HTTP/TG/gRPC servers, silent DB)
	fmt.Print("\033[90m⏳ 初始化中...\033[0m")
	app, err := application.NewAppCLI(cfg, log)
	if err != nil {
		return fmt.Errorf("\n初始化失败: %w", err)
	}
	fmt.Print("\r\033[2K") // Clear "initializing" line

	// Tool count
	toolCount := 0
	if reg := app.ToolRegistry(); reg != nil {
		toolCount = len(reg.List())
	}

	// Build initial prompt from trailing args
	initPrompt := ""
	if len(args) > 0 {
		initPrompt = strings.Join(args, " ")
	}

	replCfg := cli.REPLConfig{
		Model:      cfg.Agent.DefaultModel,
		Workspace:  workspace,
		ToolCount:  toolCount,
		NoApprove:  noApprove,
		InitPrompt: initPrompt,
	}

	return cli.RunREPL(app.AgentLoop(), app.PromptEngine(), replCfg)
}

// ─── Gateway Server Mode ───

func runServe(cmd *cobra.Command, args []string) error {
	log, err := logger.NewLogger(logger.Config{
		Level:      "info",
		Format:     "json",
		OutputPath: "stdout",
	})
	if err != nil {
		return fmt.Errorf("logger init: %w", err)
	}
	defer log.Sync()

	log.Info("Starting NGOClaw Gateway",
		zap.String("version", cliVersion),
	)

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app, err := application.NewApp(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize application", zap.Error(err))
	}

	if err := app.Start(ctx); err != nil {
		log.Fatal("Failed to start application", zap.Error(err))
	}

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := app.Stop(shutdownCtx); err != nil {
		log.Error("Error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	log.Info("Application stopped successfully")
	return nil
}

// ─── Doctor ───

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Printf("◇ NGOClaw Doctor v%s\n\n", cliVersion)

	checks := []struct {
		name  string
		check func() (string, bool)
	}{
		{"配置文件", checkConfig},
		{"Go 工具链", checkGo},
		{"Python 环境", checkPython},
	}

	allOK := true
	for _, c := range checks {
		val, ok := c.check()
		icon := "\033[92m✓\033[0m"
		if !ok {
			icon = "\033[91m✗\033[0m"
			allOK = false
		}
		fmt.Printf("  %s %s: %s\n", icon, c.name, val)
	}

	fmt.Println()
	if allOK {
		fmt.Println("所有检查通过 ✓")
	} else {
		fmt.Println("存在问题, 请检查上方标记")
	}
	return nil
}

func checkConfig() (string, bool) {
	path := os.Getenv("HOME") + "/.ngoclaw/config.yaml"
	if _, err := os.Stat(path); err == nil {
		return path, true
	}
	return "未找到 ~/.ngoclaw/config.yaml", false
}

func checkGo() (string, bool) {
	for _, p := range []string{"/usr/local/go/bin/go", "/usr/bin/go", "/usr/lib/go/bin/go"} {
		if _, err := os.Stat(p); err == nil {
			return "已安装", true
		}
	}
	return "未安装", false
}

func checkPython() (string, bool) {
	p := os.Getenv("HOME") + "/miniconda3/envs/claw"
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "conda 'claw' 环境未找到", false
}
