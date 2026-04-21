# Windsurf Proxy Go

将 Windsurf IDE 的 AI 模型转换为 OpenAI 兼容 REST API 的代理服务。

## 功能

- 支持 70+ Windsurf AI 模型（Claude、GPT、Gemini、DeepSeek 等）
- OpenAI 兼容 API（`/v1/chat/completions`、`/v1/models`）
- SSE 流式响应
- 多实例负载均衡（Round-Robin、Weighted、Least-Connections）
- 自动凭证发现（从运行中的 Windsurf 提取 CSRF token、gRPC port、API key）
- Standalone 模式（无需 Windsurf IDE 运行）
- API Key 管理 + Rate Limit
- 管理接口（实例管理、API Key CRUD、实时日志 WebSocket）
- 跨平台支持（darwin/linux/windows）

## 快速开始

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/your-repo/windsurf-proxy-go.git
cd windsurf-proxy-go

# 安装依赖
go mod download

# 构建
make build

# 运行（需要 Windsurf IDE 运行）
./build/windsurf-proxy -c config.yaml.example
```

### Docker

```bash
# 构建镜像
make docker

# 运行容器
docker run -p 8000:8000 windsurf-proxy:latest
```

## 配置

配置文件 `config.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8000

api_keys:
  - key: "sk-your-api-key"
    name: "default"
    rate_limit: 60
    allowed_models: ["*"]

instances:
  - name: "local"
    type: "local"
    auto_discover: true
    weight: 10

balancing:
  strategy: "round_robin"
  health_check_interval: 30
  max_retries: 3
```

### 实例类型

- **local**: 自动发现运行中的 Windsurf IDE 凭证
- **manual**: 手动配置 host/port/csrf_token/api_key
- **standalone**: 独立启动 language_server 进程（需要 Windsurf 安装）

## API 使用

### 获取模型列表

```bash
curl http://localhost:8000/v1/models \
  -H "Authorization: Bearer sk-your-api-key"
```

### 聊天补全

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -d '{
    "model": "claude-3.5-sonnet",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### 流式响应

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

## 管理接口

管理接口用于桌面 GUI，端口与 API 相同：

- `GET /api/instances` - 列出所有实例
- `POST /api/instances` - 添加实例
- `DELETE /api/instances/{name}` - 删除实例
- `GET /api/keys` - 列出 API Keys
- `POST /api/keys` - 创建 API Key
- `PUT /api/keys/{key_id}` - 更新 API Key
- `DELETE /api/keys/{key_id}` - 删除 API Key
- `GET /api/stats` - 运行统计
- `GET /api/models` - 支持的模型列表
- `GET /api/requests` - 请求历史
- `WebSocket /api/logs` - 实时日志流

## 支持的模型

部分常用模型：

| 模型名称 | Windsurf UID |
|---------|-------------|
| claude-3.5-sonnet | claude-3-5-sonnet@20241022 |
| claude-4-opus | claude-4-opus@20250414 |
| claude-4-sonnet | claude-4-sonnet@20250414 |
| gpt-4o | gpt-4o-2024-05-13 |
| gpt-4.1 | gpt-4.1-2025-04-14 |
| gemini-2.5-pro | gemini-2.5-pro-preview-03-25 |
| deepseek-v3 | deepseek-chat |

完整模型列表见 `internal/core/model_map.go`。

## 开发

### 项目结构

```
windsurf-proxy-go/
├── cmd/windsurf-proxy/main.go      # CLI 入口
├── internal/
│   ├── api/                        # OpenAI 兼容 API
│   ├── auth/                       # Firebase 认证 + 凭证发现
│   ├── balancer/                   # 负载均衡
│   ├── config/                     # YAML 配置
│   ├── core/
│   │   ├── grpc/                   # gRPC 客户端
│   │   ├── protobuf/               # Protobuf 编解码
│   │   └── model_map.go            # 模型映射
│   ├── keys/                       # API Key 管理
│   ├── management/                 # 管理接口
│   ├── standalone/                 # 进程管理
│   └── tool_adapter/               # Tool calling 适配
├── configs/config.yaml.example
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

### 运行测试

```bash
make test
```

### 代码检查

```bash
make check
make lint
```

## 许可证

MIT License

