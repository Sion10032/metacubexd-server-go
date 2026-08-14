# AGENTS.md

## 沟通

- 始终使用中文与用户沟通。
- 代码、命令、文件名等技术内容保持原文。

## 开发

- 修改代码前先阅读相关实现。
- 遵循项目现有代码风格和配置。
- 优先进行最小化修改，避免无关重构。
- 注意 `internal/server/`、`internal/api/`、`internal/tui/` 的跨包依赖方向：`api` 只放共享 wire 类型，`server` 与 `tui` 通过它通信，避免循环依赖。
- TUI 开发时参考根目录的 `TUI_PLAN.md`、`tui_plan_1.md`、`tui_plan_2.md`；`TUI_DEFERRED.md`、`todo.md` 记录延后事项。

## 常用命令

```bash
go build ./...                     # 全部编译（server + tui）
go build ./cmd/metacubexd-server   # 仅服务端（CI 用：go build -trimpath ./cmd/metacubexd-server）
go vet ./...                       # CI 静态检查
go test ./...                      # 全部测试（supervisor 测试会现场编译 testdata/fake-mihomo）
```

- 开发环境用 direnv + Nix flake（`.envrc` 自动加载）；`GOPROXY` 指向 goproxy.cn，`GOPATH` 指向本地 `.go/`。
- 没有独立的 lint 脚本，CI 只有 `go vet ./...`；格式化用 `go fmt`。

## 前端资源

- 服务端通过 `go:embed` 打包 `internal/server/static/web/`（见 `internal/server/static/static.go`）。
- 该目录被 git 忽略（只保留 `.gitkeep`），需要运行 `bash scripts/fetch-ui.sh` 下载 metacubexd 发布包才能嵌入真实 UI。
- 未打包 UI 时服务端会输出 stub 错误页，仍可通过 `UI_DIST` 指向磁盘目录加载；API 不受影响。

## Commit Message

生成提交信息前必须查看当前暂存区变更：

```bash
git status
git diff --cached
```

- 提交信息仅基于已暂存的内容生成。
- 遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范。
- 允许的类型：`feat`、`fix`、`chore`、`docs`、`refactor`、`test`、`ci`、`perf`、`style`。
- 范围（scope）可选，在类型后加括号指定，例如 `feat(config):`、`ci(docker):`。
- 描述使用英文，首字母小写，末尾不加句号。
- 如果提交涉及多个独立变更，在正文中用 `-` 列表分行说明。

## 代码风格

### Go

- 使用 `go fmt` 格式化代码。
- 遵循标准 Go 项目布局（`cmd/`、`internal/` 等）。
- 错误处理优先，避免静默丢弃错误。

### 通用

- 遵循项目中的 ESLint、Prettier、Biome 等配置（如果有）。
- 不要因个人偏好修改代码格式。

## 项目结构

```
cmd/metacubexd-server/   # 服务端入口（配置来自环境变量，见 README § 配置）
cmd/mihomo-tui/          # 交互式 TUI 客户端（连接运行中的 server）
internal/
  api/                   # 共享 wire 类型（server 与 tui 共用）
  server/                # 服务端
    server.go            # 组装入口（路由挂载、启动顺序）
    auth/                # 登录页 + Cookie 认证
    clashproxy/          # Clash API 同源代理
    config/              # 环境变量配置
    control/             # /api/control/* API
    kernel/              # mihomo 内核
    merge/               # 配置合并
    profile/             # 配置文件管理
    scheduler/           # 定时任务
    sse/                 # Server-Sent Events
    static/              # 静态资源（go:embed web/）
    supervisor/          # mihomo 进程管理
    webdav/              # WebDAV 备份
  tui/                   # TUI（bubbletea v2）
    client/              # HTTP/SSE 客户端
    pages/               # 各标签页
    shared/              # 跨标签页基础设施
```

## 技术栈

- **语言**: Go 1.26（CI 用 go-version 1.26，go.mod 声明 1.26.4）
- **服务端路由**: chi (go-chi/chi/v5)
- **WebSocket**: gorilla/websocket
- **TUI**: charm.land/bubbletea + lipgloss (v2)
- **YAML**: gopkg.in/yaml.v3
- **打包**: Docker + GoReleaser（`scripts/fetch-ui.sh` 为 goreleaser before hook）
- **开发环境**: Nix flake + direnv
- **CI/CD**: GitHub Actions（CI / Docker / Release，触发于 main 分支与 v* 标签）
