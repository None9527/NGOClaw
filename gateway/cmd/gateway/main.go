package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ngoclaw/ngoclaw/gateway/internal/application"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/config"
	"github.com/ngoclaw/ngoclaw/gateway/internal/infrastructure/logger"
	"github.com/ngoclaw/ngoclaw/gateway/internal/interfaces/repl"
	"go.uber.org/zap"
)

const (
	appName    = "ngoclaw-gateway"
	appVersion = "0.1.0"
)

func main() {
	// Check for subcommand
	mode := "gateway"
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "repl":
			mode = "repl"
		case "version":
			fmt.Printf("%s v%s\n", appName, appVersion)
			return
		case "help", "--help", "-h":
			printUsage()
			return
		}
	}

	// Initialize logger
	logFormat := "json"
	logLevel := "info"
	if mode == "repl" {
		logFormat = "console"
		logLevel = "warn" // Reduce noise in REPL mode
	}
	log, err := logger.NewLogger(logger.Config{
		Level:      logLevel,
		Format:     logFormat,
		OutputPath: "stdout",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	log.Info("Starting NGOClaw",
		zap.String("name", appName),
		zap.String("version", appVersion),
		zap.String("mode", mode),
	)

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Create application context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize application
	app, err := application.NewApp(cfg, log)
	if err != nil {
		log.Fatal("Failed to initialize application", zap.Error(err))
	}

	switch mode {
	case "repl":
		runREPL(ctx, app, cfg)
	default:
		runGateway(ctx, app, log)
	}
}

// runGateway starts the full gateway with all interfaces
func runGateway(ctx context.Context, app *application.App, log *zap.Logger) {
	if err := app.Start(ctx); err != nil {
		log.Fatal("Failed to start application", zap.Error(err))
	}

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	log.Info("Received shutdown signal", zap.String("signal", sig.String()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Stop(shutdownCtx); err != nil {
		log.Error("Error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	log.Info("Application stopped successfully")
}

// runREPL starts the interactive REPL mode
func runREPL(ctx context.Context, app *application.App, cfg *config.Config) {
	r := repl.New(
		app.ProcessMessageUseCase(),
		app.Logger(),
		repl.Config{
			DefaultModel: cfg.Agent.DefaultModel,
			UserName:     os.Getenv("USER"),
		},
	)

	if err := r.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "REPL error: %v\n", err)
		os.Exit(1)
	}

	// Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.Stop(shutdownCtx)
}

// printUsage displays usage information
func printUsage() {
	fmt.Printf(`%s v%s

Usage:
  gateway           Start the gateway server (default)
  gateway repl      Start interactive REPL mode
  gateway version   Show version
  gateway help      Show this help

Environment:
  NGOCLAW_*         Configuration overrides (see config.yaml)
`, appName, appVersion)
}
