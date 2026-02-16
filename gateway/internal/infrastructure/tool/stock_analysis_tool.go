package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domaintool "github.com/ngoclaw/ngoclaw/gateway/internal/domain/tool"
	"go.uber.org/zap"
)

// StockAnalysisTool 股票分析工具 — 调用 stock_data.py 脚本
type StockAnalysisTool struct {
	pythonBin  string // Python 可执行文件路径
	scriptPath string // stock_data.py 完整路径
	timeout    time.Duration
	logger     *zap.Logger
}

// NewStockAnalysisTool 创建股票分析工具
func NewStockAnalysisTool(pythonEnv string, skillsDir string, logger *zap.Logger) *StockAnalysisTool {
	pythonBin := "python3"
	if pythonEnv != "" {
		pythonBin = filepath.Join(pythonEnv, "bin", "python3")
	}

	return &StockAnalysisTool{
		pythonBin:  pythonBin,
		scriptPath: filepath.Join(skillsDir, "stock-trader-insight", "stock_data.py"),
		timeout:    60 * time.Second,
		logger:     logger,
	}
}

func (t *StockAnalysisTool) Kind() domaintool.Kind { return domaintool.KindFetch }

func (t *StockAnalysisTool) Name() string {
	return "stock_analysis"
}

func (t *StockAnalysisTool) Description() string {
	return "Analyze stock market data (Realtime Quote, K-Line Chart, Technical Analysis). " +
		"Supports fetching realtime data and generating tactical charts with buy/sell signals."
}

func (t *StockAnalysisTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Action mode: 'quote' (realtime price), 'kline' (history data), 'chart' (tactical analysis image)",
				"enum":        []string{"quote", "kline", "chart"},
			},
			"symbol": map[string]interface{}{
				"type":        "string",
				"description": "Stock symbol (e.g. 300383, 600519), optionally with sh/sz prefix",
			},
			"days": map[string]interface{}{
				"type":        "integer",
				"description": "Number of days for chart/kline (default: 30 for kline, 20 for chart)",
				"default":     30,
			},
			"period": map[string]interface{}{
				"type":        "string",
				"description": "Time period for kline: daily, weekly, 60min, 30min, 15min, 5min",
				"enum":        []string{"daily", "weekly", "60min", "30min", "15min", "5min"},
				"default":     "daily",
			},
		},
		"required": []string{"mode", "symbol"},
	}
}

func (t *StockAnalysisTool) Execute(ctx context.Context, args map[string]interface{}) (*domaintool.Result, error) {
	mode, ok := args["mode"].(string)
	if !ok || mode == "" {
		return nil, fmt.Errorf("mode is required")
	}
	symbol, ok := args["symbol"].(string)
	if !ok || symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	// Build command args
	cmdArgs := []string{t.scriptPath, mode, symbol}

	if days, ok := args["days"].(float64); ok { // JSON unmarshals ints as float64
		cmdArgs = append(cmdArgs, "--days", strconv.Itoa(int(days)))
	} else if days, ok := args["days"].(int); ok {
		cmdArgs = append(cmdArgs, "--days", strconv.Itoa(days))
	}

	if period, ok := args["period"].(string); ok && period != "" {
		cmdArgs = append(cmdArgs, "--period", period)
	}

	t.logger.Info("Executing stock analysis",
		zap.String("mode", mode),
		zap.String("symbol", symbol),
		zap.String("python", t.pythonBin),
	)

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, t.pythonBin, cmdArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if execCtx.Err() == context.DeadlineExceeded {
		return &domaintool.Result{
			Output:  fmt.Sprintf("Stock analysis timed out after %v", t.timeout),
			Success: false,
		}, nil
	}

	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return &domaintool.Result{
			Output:  fmt.Sprintf("Tool error: %s", strings.TrimSpace(errMsg)),
			Success: false,
		}, nil
	}

	return &domaintool.Result{
		Output:  stdout.String(),
		Success: true,
	}, nil
}
