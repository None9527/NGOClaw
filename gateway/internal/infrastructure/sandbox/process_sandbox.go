package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// Config 沙箱配置
type Config struct {
	WorkDir       string        // 工作目录
	Timeout       time.Duration // 执行超时
	AllowedBins   []string      // 允许的二进制文件
	MemoryLimit   int64         // 内存限制 (bytes)
	EnableNetwork bool          // 是否允许网络访问
	TempDir       string        // 临时文件目录
	PythonEnv     string        // 全局 Python 环境路径 (conda env / venv 根目录)
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	// Use real user HOME as workspace — commands must see real ~/.ssh, etc.
	// The sandbox provides process-group isolation and timeouts, NOT filesystem isolation.
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = "/tmp/ngoclaw-sandbox" // fallback only
	}
	return &Config{
		WorkDir: homeDir,
		Timeout: 30 * time.Second,
		AllowedBins: []string{
			// Shell 本身 (ExecuteShell 使用 bash -c)
			"bash", "sh",
			// 基础命令
			"ls", "cat", "head", "tail", "grep", "awk", "sed",
			"find", "wc", "sort", "uniq", "cut", "tr",
			// 文件操作
			"cp", "mv", "rm", "mkdir", "touch", "chmod", "chown",
			// 开发工具
			"go", "python", "python3", "node", "npm", "npx",
			"git", "make", "cargo", "rustc",
			// 系统信息
			"pwd", "whoami", "date", "env", "echo", "printf",
			// 网络
			"curl", "wget",
			// SSH (needed for remote system management tasks)
			"ssh", "scp", "ssh-keygen", "ssh-copy-id", "sshpass",
			// 系统管理
			"systemctl", "journalctl", "docker", "ping", "ip", "ss",
			"tar", "gzip", "unzip", "rsync",
		},
		MemoryLimit:   512 * 1024 * 1024, // 512MB
		EnableNetwork: true,
		TempDir:       "/tmp/ngoclaw-sandbox-tmp",
	}
}

// ProcessSandbox 进程级沙箱
type ProcessSandbox struct {
	config *Config
	logger *zap.Logger
}

// NewProcessSandbox 创建进程沙箱
func NewProcessSandbox(config *Config, logger *zap.Logger) (*ProcessSandbox, error) {
	// 确保工作目录存在
	if err := os.MkdirAll(config.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	// 确保临时目录存在
	if err := os.MkdirAll(config.TempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	return &ProcessSandbox{
		config: config,
		logger: logger,
	}, nil
}

// Result 执行结果
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Killed   bool // 是否被超时杀死
}

// Execute 执行命令
func (s *ProcessSandbox) Execute(ctx context.Context, command string, args []string) (*Result, error) {
	startTime := time.Now()

	// 验证命令是否被允许
	if !s.isAllowed(command) {
		return nil, fmt.Errorf("command '%s' is not allowed", command)
	}

	// 查找命令的完整路径
	cmdPath, err := exec.LookPath(command)
	if err != nil {
		return nil, fmt.Errorf("command not found: %s", command)
	}

	// 创建带超时的上下文
	execCtx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	// 创建命令
	cmd := exec.CommandContext(execCtx, cmdPath, args...)
	cmd.Dir = s.config.WorkDir

	// 设置环境变量
	cmd.Env = s.buildEnvironment()

	// 设置进程属性 (Linux 进程隔离)
	cmd.SysProcAttr = s.buildSysProcAttr()

	// 捕获输出
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 执行
	s.logger.Info("Executing sandboxed command",
		zap.String("command", command),
		zap.Strings("args", args),
		zap.String("work_dir", s.config.WorkDir),
	)

	err = cmd.Run()

	result := &Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(startTime),
	}

	// 检查是否超时
	if execCtx.Err() == context.DeadlineExceeded {
		result.Killed = true
		result.ExitCode = -1
		s.logger.Warn("Command killed due to timeout",
			zap.String("command", command),
			zap.Duration("timeout", s.config.Timeout),
		)
		return result, fmt.Errorf("command timed out after %v", s.config.Timeout)
	}

	// 获取退出码
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("execution failed: %w", err)
		}
	}

	s.logger.Info("Command completed",
		zap.String("command", command),
		zap.Int("exit_code", result.ExitCode),
		zap.Duration("duration", result.Duration),
	)

	return result, nil
}

// ExecuteScript 执行脚本文件
func (s *ProcessSandbox) ExecuteScript(ctx context.Context, interpreter string, script string) (*Result, error) {
	// 创建临时脚本文件
	tmpFile, err := os.CreateTemp(s.config.TempDir, "script-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp script: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// 写入脚本内容
	if _, err := tmpFile.WriteString(script); err != nil {
		return nil, fmt.Errorf("failed to write script: %w", err)
	}
	tmpFile.Close()

	// 执行脚本
	return s.Execute(ctx, interpreter, []string{tmpFile.Name()})
}

// ExecuteShell 执行 shell 命令字符串
func (s *ProcessSandbox) ExecuteShell(ctx context.Context, command string) (*Result, error) {
	return s.Execute(ctx, "bash", []string{"-c", command})
}

// isAllowed 检查命令是否被允许
func (s *ProcessSandbox) isAllowed(command string) bool {
	// 提取基本命令名
	baseName := filepath.Base(command)

	for _, allowed := range s.config.AllowedBins {
		if allowed == baseName || allowed == command {
			return true
		}
	}
	return false
}

// buildEnvironment 构建安全的环境变量
func (s *ProcessSandbox) buildEnvironment() []string {
	// Inherit system PATH so tools like ssh-copy-id, sshpass are available.
	// Fall back to a reasonable default if PATH is empty.
	sysPath := os.Getenv("PATH")
	if sysPath == "" {
		sysPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}

	// If Python env configured, prepend its bin/ to PATH
	if s.config.PythonEnv != "" {
		envBin := filepath.Join(s.config.PythonEnv, "bin")
		sysPath = envBin + ":" + sysPath
	}

	// Use real user HOME — commands need access to ~/.ssh, ~/.config, etc.
	realHome, _ := os.UserHomeDir()
	if realHome == "" {
		realHome = s.config.WorkDir
	}

	env := []string{
		"PATH=" + sysPath,
		"HOME=" + realHome,
		"TMPDIR=" + s.config.TempDir,
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		// Propagate USER for tools that need it (e.g., ssh)
		"USER=" + os.Getenv("USER"),
	}

	// Python 环境变量 (conda / venv 均可)
	if s.config.PythonEnv != "" {
		env = append(env,
			"CONDA_PREFIX="+s.config.PythonEnv,
			"VIRTUAL_ENV="+s.config.PythonEnv,
		)
	}

	// 如果允许网络，传递代理设置
	if s.config.EnableNetwork {
		if proxy := os.Getenv("HTTP_PROXY"); proxy != "" {
			env = append(env, "HTTP_PROXY="+proxy)
		}
		if proxy := os.Getenv("HTTPS_PROXY"); proxy != "" {
			env = append(env, "HTTPS_PROXY="+proxy)
		}
	}

	return env
}

// buildSysProcAttr 构建进程属性
func (s *ProcessSandbox) buildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// 创建新的进程组
		Setpgid: true,
		Pgid:    0,
	}
}

// SetWorkDir 设置工作目录
func (s *ProcessSandbox) SetWorkDir(dir string) error {
	// 验证目录存在
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("invalid work dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("work dir is not a directory: %s", dir)
	}

	s.config.WorkDir = dir
	return nil
}

// GetWorkDir 获取当前工作目录
func (s *ProcessSandbox) GetWorkDir() string {
	return s.config.WorkDir
}

// AddAllowedBin 添加允许的二进制
func (s *ProcessSandbox) AddAllowedBin(bin string) {
	s.config.AllowedBins = append(s.config.AllowedBins, bin)
}

// Cleanup 清理临时文件
func (s *ProcessSandbox) Cleanup() error {
	// 清理临时目录
	entries, err := os.ReadDir(s.config.TempDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		path := filepath.Join(s.config.TempDir, entry.Name())
		if strings.HasPrefix(entry.Name(), "script-") {
			os.Remove(path)
		}
	}

	return nil
}
