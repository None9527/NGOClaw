# NGOClaw 工具插件系统

> 写一个脚本，改一行配置，10 秒切换。

---

## 快速上手

### 第一步：写你的工具脚本

任何语言都行，满足一个契约即可：

| 输入 | 输出 |
|:---|:---|
| **stdin** 接收 JSON 参数 | **stdout** 输出 JSON 结果 |

**示例 — 自定义搜索** `~/tools/my_search.py`：

```python
#!/usr/bin/env python3
"""NGOClaw Tool: 自定义搜索"""
import sys, json

def main():
    # 1. 从 stdin 读取参数
    args = json.loads(sys.stdin.read())
    query = args.get("query", "")

    # 2. 你的搜索逻辑
    results = do_search(query)  # 调 SearXNG / API / 爬虫 / 任何你想要的

    # 3. 输出结果到 stdout
    print(json.dumps({
        "success": True,
        "result": results
    }))

def do_search(query):
    # 替换成你的实现
    import subprocess
    out = subprocess.check_output(["curl", "-s", f"http://localhost:8888/search?q={query}&format=json"])
    return json.loads(out)["results"][:5]

if __name__ == "__main__":
    main()
```

### 第二步：注册到 config.yaml

```yaml
agent:
  tools:
    registry:
      - name: web_search
        backend: command
        command: "python3 ~/tools/my_search.py"
        enabled: true
        timeout: 15s                          # 可选，覆盖全局 tool_timeout
        aliases:
          claude: ["WebSearch", "web_search"]
          gemini: ["web_search", "google_search"]
          openai: ["web_search"]
```

### 第三步：重启 gateway

```bash
systemctl restart ngoclaw-gateway
# 或直接重启进程
```

完成。Agent 下次调用搜索时会用你的脚本。

---

## 四种后端类型

| Backend | 适用场景 | 运行方式 |
|:---|:---|:---|
| `command` | **最常用** — 任意脚本/命令 | ProcessSandbox 沙箱执行 |
| `go` | 性能关键型工具 (如 read_file) | Go 函数直接调用，零开销 |
| `python` | 需要 Python 生态库 (如 Playwright) | 通过 gRPC 委托给 AI-Service |
| `grpc` | 独立微服务 | 直连 gRPC endpoint |

### command 后端详解

```yaml
- name: my_tool
  backend: command
  command: "python3 ~/tools/my_tool.py"     # 执行的命令
  timeout: 30s                               # 超时 (可选)
  enabled: true
```

**执行流程**:
```
Agent 需要工具 → ToolRegistry 查找 → ProcessSandbox 启动子进程
→ JSON 参数写入 stdin → 等待 stdout → 解析 JSON 结果 → 返回 Agent
```

**安全保障**: 通过 ProcessSandbox 执行，自动应用白名单、超时、进程组隔离。

### go 后端详解

```yaml
- name: read_file
  backend: go
  handler: "builtin:read_file"              # 内置 Go 函数名
  enabled: true
```

内置工具列表:
- `builtin:read_file` — 读文件
- `builtin:write_file` — 写文件
- `builtin:execute_command` — 执行命令
- `builtin:list_dir` — 列目录

### python 后端详解

```yaml
- name: browser_navigate
  backend: python
  grpc_method: "ExecuteTool"                # AI-Service 上的方法
  enabled: true
```

适合需要 Python 库的工具 (Playwright, pandas, requests 等)。

### grpc 后端详解

```yaml
- name: code_analysis
  backend: grpc
  grpc_endpoint: "localhost:50052"          # 独立服务地址
  grpc_method: "AnalyzeCode"
  enabled: true
```

适合已有独立服务的高级工具。

---

## 工具别名 — 适配不同 AI 模型

不同模型对同一工具的叫法不一样：

| 工具 | Claude 叫 | Gemini 叫 | GPT 叫 |
|:---|:---|:---|:---|
| 搜索 | `WebSearch` | `web_search` | `web_search` |
| 读文件 | `ReadFile` | `read_file` | `read_file` |
| 执行命令 | `Bash` / `ExecuteCommand` | `execute_command` | `execute_command` |

在 config.yaml 中配置别名：

```yaml
- name: web_search           # 规范名
  aliases:
    claude: ["WebSearch", "web_search", "websearch"]
    gemini: ["web_search", "google_search"]
    openai: ["web_search"]
    minimax: ["web_search", "search"]
```

**匹配逻辑** (自动按优先级):
1. 精确匹配规范名
2. 匹配当前模型的别名列表
3. 大小写不敏感匹配
4. 去下划线匹配 (`WebSearch` → `websearch` → 匹配 `web_search`)

---

## 运行时超参数

> 所有超参数都在 `config.yaml` 中，**不需要改代码**。

```yaml
agent:
  runtime:
    tool_timeout: 30s           # 单个工具最大执行时间
    run_timeout: 5m             # 单次 Agent Run 最大时长
    sub_agent_timeout: 2m       # 子 Agent 任务超时
    max_token_budget: 100000    # Token 预算上限 (防成本爆炸)
    concurrent_tools: true      # 多工具是否并发执行

  guardrails:
    context_max_tokens: 128000  # 上下文窗口容量
    context_warn_ratio: 0.7     # 70% 时日志警告
    context_hard_ratio: 0.85    # 85% 时自动压缩
    loop_detect_threshold: 5    # 同一工具连调 5 次中断
    cost_guard_enabled: true    # 启用成本保护

  compaction:
    message_threshold: 30       # 超过 30 条触发压缩
    token_threshold: 30000      # 超过 30K token 触发
    keep_recent: 10             # 保留最近 10 条
    pre_flush_to_memory: true   # 压缩前存关键事实到向量库
```

**环境变量覆盖** (Viper 自动映射):
```bash
export NGOCLAW_AGENT_RUNTIME_TOOL_TIMEOUT=60s
export NGOCLAW_AGENT_GUARDRAILS_LOOP_DETECT_THRESHOLD=3
```

---

## stdin/stdout 契约规范

### 输入 (stdin)

Gateway 向工具 stdin 写入一个 JSON 对象：

```json
{
  "query": "NGOClaw agent tool system",
  "max_results": 5
}
```

字段由工具的 `parameters` JSON Schema 定义，与 AI 模型的 tool_call arguments 一致。

### 输出 (stdout)

工具必须输出一个 JSON 对象：

```json
{
  "success": true,
  "result": "搜索结果文本..."
}
```

| 字段 | 类型 | 必须 | 说明 |
|:---|:---:|:---:|:---|
| `success` | bool | ✅ | 是否成功 |
| `result` | string | ✅ | 结果文本 (传给 AI 模型) |
| `error` | string | ❌ | 失败时的错误信息 |

### 错误处理

```json
{
  "success": false,
  "error": "SearXNG 服务不可达"
}
```

工具也可以直接**返回非零退出码** + stderr，Gateway 会自动捕获为错误。

### 超时处理

如果工具超过 `tool_timeout` 未输出，Gateway 会：
1. 发送 SIGTERM 到进程组
2. 等待 5 秒
3. 发送 SIGKILL
4. 向 Agent 返回 `[Tool timed out after 30s]`

---

## 完整示例：替换搜索引擎

**场景**: 原来用 SearXNG，现在想切到 Tavily API。

**第一步** — 写 `~/tools/tavily_search.py`:

```python
#!/usr/bin/env python3
import sys, json, urllib.request

def main():
    args = json.loads(sys.stdin.read())
    query = args.get("query", "")

    req = urllib.request.Request(
        "https://api.tavily.com/search",
        data=json.dumps({"query": query, "max_results": 5}).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer tvly-xxx"}
    )
    resp = urllib.request.urlopen(req, timeout=10)
    data = json.loads(resp.read())

    results = "\n".join(f"- {r['title']}: {r['url']}" for r in data.get("results", []))
    print(json.dumps({"success": True, "result": results}))

if __name__ == "__main__":
    main()
```

**第二步** — 改 `config.yaml` 一行:

```diff
  tools:
    registry:
      - name: web_search
        backend: command
-       command: "python3 ~/tools/searxng_search.py"
+       command: "python3 ~/tools/tavily_search.py"
```

**第三步** — 重启。完成。
