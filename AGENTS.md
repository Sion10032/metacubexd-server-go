# AGENTS.md

## 沟通

- 始终使用中文与用户沟通。
- 代码、命令、文件名等技术内容保持原文。

## 开发

- 修改代码前先阅读相关实现。
- 遵循项目现有代码风格和配置。
- 优先进行最小化修改，避免无关重构。
- 当涉及 `internal/` 下多个包时，注意跨包依赖关系。

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

- 使用 `go fmt` / `gofumpt` 格式化代码。
- 遵循标准 Go 项目布局（`cmd/`、`internal/` 等）。
- 错误处理优先，避免静默丢弃错误。
- 在 `internal/` 中按职责分包，避免循环依赖。

### 通用

- 遵循项目中的 ESLint、Prettier、Biome 等配置（如果有）。
- 不要因个人偏好修改代码格式。

## 项目结构

```
cmd/metacubexd-server/   # 主入口
internal/                # 内部包
  auth/                  # 认证中间件
  clashproxy/            # Clash 代理
  config/                # 配置管理
  control/               # API 控制层
  kernel/                # 内核管理
  merge/                 # 配置合并
  profile/               # 配置文件管理
  scheduler/             # 定时任务
  sse/                   # Server-Sent Events
  static/                # 静态资源
  supervisor/            # 进程管理
  webdav/                # WebDAV 备份
```

## 技术栈

- **语言**: Go 1.26
- **路由**: chi (go-chi/chi/v5)
- **WebSocket**: gorilla/websocket
- **YAML**: gopkg.in/yaml.v3
- **打包**: Docker + GoReleaser
- **开发环境**: Nix flake + direnv
- **CI/CD**: GitHub Actions
