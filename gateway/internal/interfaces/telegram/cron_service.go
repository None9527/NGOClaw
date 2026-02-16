package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CronJob 定时任务
type CronJob struct {
	ID        string
	ChatID    int64
	CronExpr  string // cron 表达式
	Command   string // 要执行的命令
	Enabled   bool
	LastRun   time.Time
	NextRun   time.Time
	CreatedAt time.Time
}

// CronService 定时任务服务
type CronService struct {
	db       *sql.DB
	jobs     map[string]*CronJob
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	executor func(chatID int64, command string) error
}

// NewCronService 创建定时任务服务
func NewCronService(db *sql.DB) *CronService {
	ctx, cancel := context.WithCancel(context.Background())
	return &CronService{
		db:     db,
		jobs:   make(map[string]*CronJob),
		ctx:    ctx,
		cancel: cancel,
	}
}

// SetExecutor 设置命令执行器
func (c *CronService) SetExecutor(executor func(chatID int64, command string) error) {
	c.executor = executor
}

// Start 启动定时任务调度器
func (c *CronService) Start() error {
	// 加载现有任务
	if err := c.loadJobs(); err != nil {
		return err
	}

	// 启动调度循环
	go c.scheduleLoop()

	return nil
}

// Stop 停止定时任务服务
func (c *CronService) Stop() {
	c.cancel()
}

// loadJobs 从数据库加载任务
func (c *CronService) loadJobs() error {
	rows, err := c.db.Query(`
		SELECT id, chat_id, cron_expr, command, enabled, last_run, next_run, created_at
		FROM cron_jobs WHERE enabled = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	c.mu.Lock()
	defer c.mu.Unlock()

	for rows.Next() {
		job := &CronJob{}
		var lastRun, nextRun, createdAt sql.NullTime
		err := rows.Scan(&job.ID, &job.ChatID, &job.CronExpr, &job.Command,
			&job.Enabled, &lastRun, &nextRun, &createdAt)
		if err != nil {
			continue
		}
		if lastRun.Valid {
			job.LastRun = lastRun.Time
		}
		if nextRun.Valid {
			job.NextRun = nextRun.Time
		}
		if createdAt.Valid {
			job.CreatedAt = createdAt.Time
		}
		c.jobs[job.ID] = job
	}

	return nil
}

// scheduleLoop 调度循环
func (c *CronService) scheduleLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case now := <-ticker.C:
			c.runDueJobs(now)
		}
	}
}

// runDueJobs 运行到期的任务
func (c *CronService) runDueJobs(now time.Time) {
	c.mu.RLock()
	var dueJobs []*CronJob
	for _, job := range c.jobs {
		if job.Enabled && !job.NextRun.IsZero() && now.After(job.NextRun) {
			dueJobs = append(dueJobs, job)
		}
	}
	c.mu.RUnlock()

	for _, job := range dueJobs {
		go c.executeJob(job)
	}
}

// executeJob 执行单个任务
func (c *CronService) executeJob(job *CronJob) {
	if c.executor == nil {
		return
	}

	// 执行命令
	if err := c.executor(job.ChatID, job.Command); err != nil {
		// 记录错误但继续
	}

	// 更新运行时间
	c.mu.Lock()
	job.LastRun = time.Now()
	job.NextRun = c.calculateNextRun(job.CronExpr, job.LastRun)
	c.mu.Unlock()

	// 持久化
	c.db.Exec(`
		UPDATE cron_jobs SET last_run = ?, next_run = ? WHERE id = ?`,
		job.LastRun, job.NextRun, job.ID)
}

// Schedule 添加定时任务
func (c *CronService) Schedule(chatID int64, cronExpr, command string) (string, error) {
	// 验证 cron 表达式
	nextRun := c.calculateNextRun(cronExpr, time.Now())
	if nextRun.IsZero() {
		return "", fmt.Errorf("无效的 cron 表达式: %s", cronExpr)
	}

	job := &CronJob{
		ID:        fmt.Sprintf("cron_%d_%d", chatID, time.Now().UnixNano()),
		ChatID:    chatID,
		CronExpr:  cronExpr,
		Command:   command,
		Enabled:   true,
		NextRun:   nextRun,
		CreatedAt: time.Now(),
	}

	// 保存到数据库
	_, err := c.db.Exec(`
		INSERT INTO cron_jobs (id, chat_id, cron_expr, command, enabled, next_run, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.ChatID, job.CronExpr, job.Command, 1, job.NextRun, job.CreatedAt)
	if err != nil {
		return "", err
	}

	// 添加到内存
	c.mu.Lock()
	c.jobs[job.ID] = job
	c.mu.Unlock()

	return job.ID, nil
}

// Cancel 取消定时任务
func (c *CronService) Cancel(jobID string) error {
	c.mu.Lock()
	delete(c.jobs, jobID)
	c.mu.Unlock()

	_, err := c.db.Exec(`DELETE FROM cron_jobs WHERE id = ?`, jobID)
	return err
}

// List 列出聊天的所有定时任务
func (c *CronService) List(chatID int64) []*CronJob {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*CronJob
	for _, job := range c.jobs {
		if job.ChatID == chatID {
			result = append(result, job)
		}
	}
	return result
}

// calculateNextRun 计算下次运行时间
// 简化实现：支持 @hourly, @daily, @weekly, 或 "分钟 小时 日 月 星期" 格式
func (c *CronService) calculateNextRun(cronExpr string, after time.Time) time.Time {
	now := after.Add(time.Minute) // 至少 1 分钟后

	// 预设表达式
	switch cronExpr {
	case "@hourly":
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
	case "@daily":
		return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	case "@weekly":
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 0, 0, 0, 0, now.Location())
	}

	// 解析标准 cron: "分 时 日 月 周"
	parts := strings.Fields(cronExpr)
	if len(parts) < 2 {
		return time.Time{}
	}

	minute, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return time.Time{}
	}

	hour := 0
	if len(parts) > 1 {
		hour, err = parseCronField(parts[1], 0, 23)
		if err != nil {
			return time.Time{}
		}
	}

	// 简化：只处理分钟和小时
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}

// parseCronField 解析 cron 字段
func parseCronField(field string, min, max int) (int, error) {
	if field == "*" {
		return min, nil
	}
	val, err := strconv.Atoi(field)
	if err != nil {
		return 0, err
	}
	if val < min || val > max {
		return 0, fmt.Errorf("值 %d 超出范围 [%d, %d]", val, min, max)
	}
	return val, nil
}
