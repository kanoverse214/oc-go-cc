# oc-go-cc

一个 Go CLI 代理工具，让你能将 [OpenCode Go](https://opencode.ai/docs/go/) 订阅与 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 搭配使用。

`oc-go-cc` 位于 Claude Code 和 OpenCode Go 之间，拦截 Anthropic API 请求，将其转换为 OpenAI 格式，并转发到 OpenCode Go 的端点。Claude Code 以为自己正在与 Anthropic 通信——但实际你的请求被转发到了价格实惠的开源模型上。

## 为什么要用？

OpenCode Go 提供了强大的开源编程模型，仅需 **$5/月**（之后 $10/月）。本代理使这些模型能够无缝地与 Claude Code 的界面配合工作——无需任何补丁或分支，只需设置两个环境变量即可开始使用。

## 特性

- **透明代理** — Claude Code 发送 Anthropic 格式的请求，代理将其转换为 OpenAI 格式后再转换回来
- **模型直通** — 直接从请求中透传模型 ID；你在 Claude Code 配置什么模型就用什么模型
- **双端点支持** — 根据模型自动路由到 OpenAI 兼容端点（`/v1/chat/completions`）或 Anthropic 原生端点（`/v1/messages`）
- **实时流式传输** — 完整的 SSE 流式传输，支持 OpenAI ↔ Anthropic 格式的实时转换
- **工具调用** — 支持 Anthropic `tool_use`/`tool_result` 与 OpenAI 函数调用之间的双向转换
- **DeepSeek 思考模式** — 支持 `output_config.effort` 控制推理力度，并在所有助手消息中传播 `reasoning_content`
- **JSON 配置** — 灵活的配置文件，支持环境变量覆盖和 `${VAR}` 插值
- **后台运行模式** — 作为守护进程脱离终端运行
- **开机自启** — 通过 launchd 在系统启动时自动启动（macOS）
- **速率限制与请求去重** — 保护上游 API 免受滥用

## 安装

### Homebrew（macOS & Linux）

```bash
brew tap samueltuyizere/tap
brew install oc-go-cc
```

### 源码编译

```bash
git clone https://github.com/samueltuyizere/oc-go-cc.git
cd oc-go-cc
make build

# 二进制文件位于 bin/oc-go-cc
# 可选：安装到 $GOPATH/bin
make install
```

### 下载发行版

从 [Releases 页面](https://github.com/samueltuyizere/oc-go-cc/releases) 下载对应平台的二进制文件：

| 平台                      | 文件                               |
| ------------------------- | ---------------------------------- |
| macOS（Apple Silicon）    | `oc-go-cc_darwin-arm64`            |
| macOS（Intel）            | `oc-go-cc_darwin-amd64`            |
| Linux（x86_64）           | `oc-go-cc_linux-amd64`             |
| Linux（ARM64）            | `oc-go-cc_linux-arm64`             |
| Windows（x86_64）         | `oc-go-cc_windows-amd64.exe`       |
| Windows（ARM64）          | `oc-go-cc_windows-arm64.exe`       |

```bash
# 示例：macOS Apple Silicon
curl -L -o oc-go-cc https://github.com/samueltuyizere/oc-go-cc/releases/latest/download/oc-go-cc_darwin-arm64
chmod +x oc-go-cc
sudo mv oc-go-cc /usr/local/bin/
```

### 环境要求

- 一个 [OpenCode Go](https://opencode.ai/auth) 订阅和 API 密钥
- Go 1.21+（仅源码编译时需要）

## 快速开始

### 1. 初始化配置

```bash
oc-go-cc init
```

在 `~/.config/oc-go-cc/config.json` 创建默认配置文件。如果配置已存在，则显示路径供你直接编辑。

### 2. 设置 API 密钥

```bash
export OC_GO_CC_API_KEY=sk-opencode-your-key-here
```

### 3. 启动代理

```bash
oc-go-cc serve
```

你会看到类似输出：

```
Starting oc-go-cc v0.1.0
Listening on 127.0.0.1:3456
Forwarding to: https://opencode.ai/zen/go/v1/chat/completions

Configure Claude Code with:
  export ANTHROPIC_BASE_URL=http://127.0.0.1:3456
  export ANTHROPIC_AUTH_TOKEN=unused
```

#### 后台运行

```bash
oc-go-cc serve --background
# 或
oc-go-cc serve -b
```

日志写入 `~/.config/oc-go-cc/oc-go-cc.log`。

#### 开机自启

```bash
oc-go-cc autostart enable   # 启用
oc-go-cc autostart disable  # 禁用
oc-go-cc autostart status   # 查看状态
```

### 4. 配置 Claude Code

在另一个终端（或运行 `claude` 前的同一个终端）：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3456
export ANTHROPIC_AUTH_TOKEN=unused
```

### 5. 配置 Claude Code 模型

设置 Claude Code 使用的模型。例如 DeepSeek V4 Pro 启用最大思考模式：

```json
// ~/.claude/settings.json
{
  "modelOverride": "deepseek-v4-pro",
  "outputConfig": {
    "effort": "high"
  }
}
```

或在 CLI 中：

```bash
claude --model deepseek-v4-pro
```

### 6. 运行 Claude Code

```bash
claude
```

搞定。Claude Code 现在将通过 oc-go-cc 将所有请求路由到 OpenCode Go。

## 工作原理

代理根据模型的不同有两种路径：

**OpenAI 兼容模型（大多数模型）：**

```
┌─────────────┐    Anthropic API     ┌─────────────┐    OpenAI API      ┌─────────────┐
│  Claude Code ├───────────────────►│  oc-go-cc    ├──────────────────►│  OpenCode Go │
│  (CLI)       │  POST /v1/messages │  (代理)      │  /chat/completions│  (上游)      │
│              │◄───────────────────┤              │◄──────────────────┤              │
└─────────────┘  Anthropic SSE      └─────────────┘  OpenAI SSE        └─────────────┘
```

**Anthropic 原生模型（MiniMax M2.7 / M2.5）：**

```
┌─────────────┐    Anthropic API     ┌─────────────┐   Anthropic API    ┌─────────────┐
│  Claude Code ├───────────────────►│  oc-go-cc    ├──────────────────►│  OpenCode Go │
│  (CLI)       │  POST /v1/messages │  (代理)      │  POST /v1/messages│  (上游)      │
│              │◄───────────────────┤              │◄──────────────────┤              │
└─────────────┘  Anthropic SSE      └─────────────┘  Anthropic SSE     └─────────────┘
```

对于 OpenAI 兼容模型：
1. Claude Code 发送 [Anthropic Messages API](https://docs.anthropic.com/en/api/messages) 格式的请求
2. oc-go-cc 解析请求并将其转换为 [OpenAI Chat Completions](https://platform.openai.com/docs/api-reference/chat) 格式
3. 模型 ID 直接从 Anthropic 请求中透传——不做任何自动路由
4. 转换后的请求发送到 OpenCode Go 的 OpenAI 端点
5. 响应（流式或非流式）被转换回 Anthropic 格式
6. Claude Code 收到响应，仿佛直接来自 Anthropic

对于 Anthropic 原生模型（MiniMax），请求直接转发（仅替换模型名称），无需格式转换。

### 格式转换对照

| Anthropic                                                  | OpenAI                                |
| ---------------------------------------------------------- | ------------------------------------- |
| `system`（字符串或数组）                                   | `messages[0]` 中 `role: "system"`     |
| `content: [{"type":"text","text":"..."}]`                  | `content: "..."`                      |
| `tool_use` 内容块                                          | `tool_calls` 数组                     |
| `tool_result` 内容块                                       | `role: "tool"` 消息                   |
| `thinking` 内容块                                          | `reasoning_content`                   |
| `stop_reason: "end_turn"`                                  | `finish_reason: "stop"`               |
| `stop_reason: "tool_use"`                                  | `finish_reason: "tool_calls"`         |
| SSE 事件 `message_start` / `content_block_delta` / `message_stop` | SSE 事件 `role` / `delta.content` / `[DONE]` |

### DeepSeek V4 思考模式

DeepSeek V4 Pro 和 Flash 通过 OpenCode Go 使用兼容 OpenAI 的 `/chat/completions` 端点。它们支持思考模式和可配置的推理力度。

对于 Claude Code，使用 `output_config.effort` 配置 DeepSeek V4：

```json
{
  "modelOverride": "deepseek-v4-pro",
  "outputConfig": {
    "effort": "high"
  }
}
```

代理将其映射为 OpenAI Chat Completions 参数：
- `output_config.effort`（`"low"`、`"medium"`、`"high"`）→ `reasoning_effort`
- 对话历史中的 thinking 内容块 → 自动设置 `reasoning_effort` + `thinking` 标志

思考模式启用时，`reasoning_content` 会被传播到**所有**助手消息（包括工具调用消息），以满足上游验证器的要求。DeepSeek 将推理内容作为 OpenAI `reasoning_content` 返回，代理再将其转换回 Anthropic `thinking` 内容块供 Claude Code 使用。

代理同样处理特定供应商的验证器要求：
- **DeepSeek**：思考模式下每一条助手消息都需要 `reasoning_content`（当客户端移除了原始 thinking 时，会填充占位符）
- **Moonshot (Kimi)**：工具调用消息需要非空的 `reasoning_content`

## 配置

### 配置文件

位置：`~/.config/oc-go-cc/config.json`

通过 `OC_GO_CC_CONFIG` 环境变量覆盖路径。

### 完整配置参考

```json
{
  "api_key": "${OC_GO_CC_API_KEY}",
  "host": "127.0.0.1",
  "port": 3456,

  "opencode_go": {
    "base_url": "https://opencode.ai/zen/go/v1/chat/completions",
    "anthropic_base_url": "https://opencode.ai/zen/go/v1/messages",
    "timeout_ms": 300000
  },

  "logging": {
    "level": "info",
    "requests": true
  }
}
```

### 环境变量

环境变量会覆盖配置文件中的值。配置值也支持 `${VAR}` 插值语法。

| 变量                      | 说明                               | 默认值                                          |
| ------------------------- | ---------------------------------- | ----------------------------------------------- |
| `OC_GO_CC_API_KEY`        | OpenCode Go API 密钥（**必填**）   | —                                               |
| `OC_GO_CC_CONFIG`         | 自定义配置文件路径                 | `~/.config/oc-go-cc/config.json`                |
| `OC_GO_CC_HOST`           | 代理监听地址                       | `127.0.0.1`                                     |
| `OC_GO_CC_PORT`           | 代理监听端口                       | `3456`                                          |
| `OC_GO_CC_OPENCODE_URL`   | OpenCode Go OpenAI API 端点        | `https://opencode.ai/zen/go/v1/chat/completions` |
| `OC_GO_CC_LOG_LEVEL`      | 日志级别：`debug`/`info`/`warn`/`error` | `info`                                    |

### 端点选择

代理根据模型 ID 自动选择正确的上游端点：

- **OpenAI 端点**（`opencode_go.base_url`，默认为 `/v1/chat/completions`）—— 大多数模型（GLM、Kimi、MiMo、Qwen、DeepSeek）
- **Anthropic 端点**（`opencode_go.anthropic_base_url`，默认为 `/v1/messages`）—— MiniMax M2.7 / M2.5

运行 `oc-go-cc models` 查看哪些模型使用哪个端点。

### 可用模型

关于模型能力、费用和建议，请查阅 [MODELS.md](MODELS.md)。

运行 `oc-go-cc models` 查看所有支持的模型 ID 及其端点类型。

> **⚠️ 注意：** MiniMax M2.5 和 M2.7 使用 **Anthropic 兼容**的 `/v1/messages` 原生端点——请求直接转发，无需格式转换。其他所有模型均使用 OpenAI 兼容的 `/chat/completions` 端点，需要格式转换。

## CLI 命令

```
oc-go-cc serve              启动代理服务器
oc-go-cc serve -b           后台启动（脱离终端）
oc-go-cc serve --port 8080  自定义端口启动
oc-go-cc serve --config /path/to/config.json  使用自定义配置
oc-go-cc stop               停止正在运行的代理服务器
oc-go-cc status             检查代理是否正在运行
oc-go-cc autostart enable   启用开机自启
oc-go-cc autostart disable  禁用开机自启
oc-go-cc autostart status   查看开机自启状态
oc-go-cc init               创建默认配置文件
oc-go-cc validate           验证配置文件
oc-go-cc models             列出可用的 OpenCode Go 模型及端点类型
oc-go-cc --version          显示版本号
```

## API 端点

代理暴露以下 Claude Code 期望的端点：

| 方法   | 路径                          | 说明                   |
| ------ | ----------------------------- | ---------------------- |
| `POST` | `/v1/messages`                | 主要聊天端点（Anthropic 格式） |
| `POST` | `/v1/messages/count_tokens`   | Token 计数             |
| `GET`  | `/health`                     | 健康检查               |

## 故障排查

### "invalid request body" 错误

代理无法解析来自 Claude Code 的请求。启用调试日志查看原始请求：

```json
{ "logging": { "level": "debug" } }
```

或设置环境变量：

```bash
export OC_GO_CC_LOG_LEVEL=debug
```

### "upstream request failed" 错误

上游 OpenCode Go API 返回了错误。请检查：

1. API 密钥是否有效：`oc-go-cc validate`
2. 是否超过了[使用限制](https://opencode.ai/auth)
3. OpenCode Go 服务是否可达：`curl -H "Authorization: Bearer $OC_GO_CC_API_KEY" https://opencode.ai/zen/go/v1/models`
4. Claude Code 中指定的模型是否存在并支持所需功能（如思考模式）

### 连接被拒绝

确保代理正在运行：

```bash
oc-go-cc status
```

并检查 Claude Code 指向正确的地址：

```bash
echo $ANTHROPIC_BASE_URL  # 应为 http://127.0.0.1:3456
```

### 流式传输异常

代理实时将 OpenAI SSE 转换为 Anthropic SSE。如果流式传输异常：

1. 将日志级别设为 `debug` 查看原始 SSE 数据块
2. 检查是否有代理或防火墙在缓冲连接
3. 先尝试非流式请求，确认模型正常工作

### 调试模式

以调试级别运行以获取最详细的日志：

```bash
OC_GO_CC_LOG_LEVEL=debug oc-go-cc serve
```

将记录：

- 来自 Claude Code 的原始 Anthropic 请求体
- 发送到 OpenCode Go 的转换后 OpenAI 请求
- 收到的原始 OpenAI 响应
- 流式传输中的 SSE 事件
- DeepSeek 思考模式下的上游 SSE 数据块详细信息

## 项目架构

```
cmd/oc-go-cc/main.go           CLI 入口（cobra 命令）
internal/
├── config/
│   ├── config.go               配置类型（api_key、host、port、opencode_go、logging）
│   └── loader.go               JSON 加载、环境变量覆盖、${VAR} 插值
├── server/
│   └── server.go               HTTP 服务器、优雅关闭、PID 管理
├── handlers/
│   ├── messages.go             POST /v1/messages 处理（流式 + 非流式）
│   └── health.go               健康检查和 Token 计数端点
├── transformer/
│   ├── request.go              Anthropic → OpenAI 请求转换
│   ├── response.go             OpenAI → Anthropic 响应转换
│   └── stream.go               实时 SSE 流转换
├── client/
│   └── opencode.go             OpenCode Go HTTP 客户端（双端点支持）
├── middleware/
│   ├── rate_limiter.go         客户端速率限制
│   └── dedup.go                请求去重
├── metrics/
│   └── metrics.go              请求指标收集
├── token/
│   └── counter.go              Tiktoken 令牌计数器（cl100k_base）
└── daemon/
    └── daemon.go               后台守护进程和 PID 管理
pkg/types/
├── anthropic.go                Anthropic API 类型（多态 system/content 字段）
└── openai.go                   OpenAI API 类型
configs/
└── config.example.json         示例配置文件
```

### 关键设计决策

- **多态字段处理**：Anthropic 的 `system` 和 `content` 字段同时支持字符串和数组。我们使用 `json.RawMessage` 配合访问方法（`SystemText()`、`ContentBlocks()`）正确处理两种格式。
- **实时流式代理**：SSE 事件在传输过程中实时转换，而非缓存后转换。这意味着 Claude Code 能实时看到从 OpenCode Go 返回的响应。
- **模型直通**：Claude Code 请求中的模型 ID 直接透传。无自动路由或模型选择——你在 Claude Code 配置什么模型就使用什么模型。
- **双端点路由**：根据 `client.IsAnthropicModel()` 按模型 ID 前缀自动路由到 OpenAI 或 Anthropic 端点。
- **环境变量插值**：配置值如 `"${OC_GO_CC_API_KEY}"` 在加载时解析，因此你无需在配置文件中存储密钥。
- **DeepSeek 思考模式**：设置 `output_config.effort` 或对话历史包含 thinking 内容块时，在所有助手消息中添加 `reasoning_content` 以满足上游验证器要求。

## 开发

```bash
# 编译（版本自动从 git 获取）
make build

# 开发模式运行
make run

# 运行测试（含竞态检测）
make test

# 运行 go vet
make vet

# 清理编译产物
make clean

# 安装到 $GOPATH/bin
make install

# 交叉编译多平台发行版
make dist
```

## 许可协议

MIT
