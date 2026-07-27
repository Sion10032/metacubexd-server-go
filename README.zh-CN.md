# metacubexd-server-go

**中文** | [**English**](README.md)

> 使用 Go 重写 metacubexd-server，替代上游 Node.js 实现，并增加 Clash API 同源代理。浏览器只需访问一个端口即可完成所有管理操作，无需单独暴露 mihomo 控制端口。适合自部署 / NAS / VPS 场景。

---

## ✨ 特性

- 🌐 **跨平台静态二进制** —— `linux/amd64`、`linux/arm64`，零外部依赖，无需 Node.js runtime
- ⚡ **轻量** —— 体积更小（~19MB vs ~50MB）、内存更低、启动更快
- 🔐 **内置鉴权** —— 登录页 + Cookie 鉴权，无需额外配置反代认证

---

## 🚀 快速开始

镜像 `sion10032/metacubexd-server:latest` 已发布到 Docker Hub，直接拉取即可，无需 clone 源码。

### docker

```bash
docker run -d \
  --name metacubexd-server \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 7890:7890 \
  -v "./data:/data" \
  -e CONTROL_TOKEN="<your-password>" \
  -e TZ=Asia/Shanghai \
  sion10032/metacubexd-server:latest
```

### docker compose

```yaml
services:
  server:
    image: sion10032/metacubexd-server:latest
    container_name: metacubexd-server
    restart: unless-stopped
    ports:
      - "8080:8080" # dashboard + control/clash API
      - "7890:7890" # mihomo mixed proxy
      # 9090 (Clash API) 默认不开放；如需外部直连 mihomo 可取消注释
      # - "9090:9090"
    volumes:
      - ./data:/data
    environment:
      CONTROL_TOKEN: "<your-password>" # 登录页密码；不设则无登录页，完全开放
      TZ: Asia/Shanghai
      PUID: 1000 # 匹配宿主用户 uid，避免 ./data 权限问题（echo $UID 查看）
      PGID: 1000
```

浏览器打开 [http://localhost:8080](http://localhost:8080) → 输入密码（即 `CONTROL_TOKEN`）进入 Dashboard。

> 设了 `CONTROL_TOKEN` 才有登录页；不设则完全开放（适合内网）。不设 `CLASH_SECRET` 时启动会自动生成随机值并打印到日志，跨源面板从日志取值填入即可。详见下方 [鉴权说明](#鉴权说明)。

### 二进制

无需 Docker，下载静态二进制 + 准备 mihomo 内核和 UI 资源：

```bash
./metacubexd-server
```

所需环境变量见下方 [配置](#️-配置) 章节。

### systemd / OpenRC 服务

将二进制部署为系统服务（需 root 或 sudo）：

```bash
# 一键安装（自动检测 systemd / OpenRC，生成随机密钥，启用并启动服务）
curl -fsSL https://raw.githubusercontent.com/Sion10032/metacubexd-server-go/main/deploy/metacubexd-ctl.sh | sudo bash
```

手动安装：

```bash
# 1. 安装二进制
sudo install -m 0755 metacubexd-server-go /usr/local/bin/metacubexd-server

# 2. 创建系统用户
sudo useradd --system --no-create-home --shell /usr/sbin/nologin metacubexd

# 3. 安装服务文件（根据你的 init 系统选择）

# systemd:
sudo cp deploy/systemd/metacubexd.service /etc/systemd/system/
sudo mkdir -p /etc/metacubexd
cp deploy/systemd/metacubexd.env.sample /etc/metacubexd/metacubexd.env
# 编辑 /etc/metacubexd/metacubexd.env，填入 CONTROL_TOKEN 和 CLASH_SECRET
sudo systemctl daemon-reload
sudo systemctl enable --now metacubexd

# OpenRC:
sudo cp deploy/openrc/metacubexd.initd /etc/init.d/metacubexd
sudo cp deploy/openrc/metacubexd.confd /etc/conf.d/metacubexd
# 编辑 /etc/conf.d/metacubexd，填入 CONTROL_TOKEN 和 CLASH_SECRET
sudo rc-update add metacubexd default
sudo rc-service metacubexd start
```

> **mihomo 内核**：服务不包含 mihomo，需自行安装到 `/usr/local/bin/mihomo`（或在 env 中设置 `MIHOMO_BIN` 指向实际路径）。TUN 模式需要 `CAP_NET_ADMIN` 权限（systemd unit 已配置 `AmbientCapabilities`；OpenRC 的 `start_pre` 会对 mihomo 执行 `setcap`）。

---

## ⚙️ 配置

### 端口

| 端口   | 用途                                  | 是否对外                                            |
| ------ | ------------------------------------- | --------------------------------------------------- |
| `8080` | Dashboard + 控制 API + Clash API 代理 | ✅ 浏览器只连这个                                   |
| `7890` | mihomo 混合代理（客户端流量）         | ✅ 代理客户端连这个                                 |
| `9090` | mihomo Clash API                      | ❌ 默认不暴露（同源代理已覆盖，如需外部直连可开放） |

### 环境变量

| 变量            | 默认值                  | 说明                                                                                                                    |
| --------------- | ----------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `CONTROL_PORT`  | `8080`                  | Dashboard 与 API 监听端口                                                                                               |
| `MIXED_PORT`    | `7890`                  | mihomo 混合代理端口                                                                                                     |
| `DATA_DIR`      | `/data`                 | 配置与数据目录                                                                                                          |
| `MIHOMO_BIN`    | `/usr/local/bin/mihomo` | mihomo 内核路径                                                                                                         |
| `CONTROL_TOKEN` | _(空)_                  | 登录页密码，也是跨源访问 `/api/control/*` 的 Bearer Token（不设 = 无登录页，`/api/control/*` 裸奔）                     |
| `CLASH_SECRET`  | _(空)_                  | mihomo 密钥。同源访问由 Server 代注入；跨源访问（官方面板）填入此值。不设时启动自动生成随机值并打印到日志，可从日志取用 |
| `TZ`            | _(容器默认)_            | 时区，如 `Asia/Shanghai`                                                                                                |
| `PUID` / `PGID` | `1000`                  | 容器内运行用户的 uid/gid，匹配宿主用户可避免权限问题                                                                    |

生产部署建议设置 Token 和 Secret（生成随机串：`openssl rand -hex 16`）：

```yaml
environment:
  CONTROL_TOKEN: "<random-string>"
  CLASH_SECRET: "<random-string>"
  TZ: Asia/Shanghai
```

#### 鉴权说明

设置 `CONTROL_TOKEN` 后，Server 启用登录页鉴权。两个 API 路径用不同凭证，对应不同资源域：

| API 路径         | 资源归属                                            | 同源访问（登录后）           | 跨源访问（官方面板/API 客户端）                                   |
| ---------------- | --------------------------------------------------- | ---------------------------- | ----------------------------------------------------------------- |
| `/api/control/*` | **Go server**（启停内核、profile、配置、备份）      | Cookie 放行                  | `Authorization: Bearer <CONTROL_TOKEN>`                           |
| `/api/clash/*`   | **mihomo 内核**（流量、proxies、connections、logs） | Server 自动注入 CLASH_SECRET | `Authorization: Bearer <CLASH_SECRET>` 或 `?token=<CLASH_SECRET>` |

**同源访问**（浏览器打开 `http://server:8080`）：未登录自动跳 `/login`，输入密码（即 `CONTROL_TOKEN`）后服务端签发签名 Cookie，后续请求（含 WebSocket / SSE）自动携带。两条 API 都靠 cookie 放行，CLASH_SECRET 由 Server 代注入，前端无感。

**跨源访问**：

- **metacubexd 官方面板**（只调 `/api/clash/*`）：在「Secret」栏填 `CLASH_SECRET`。不设 `CLASH_SECRET` 时 Server 启动会自动生成随机值并打印到启动日志（`clash secret: <值> (auto-generated)`），从日志取值填入即可。
- **API 客户端调 `/api/control/*`**：请求头携带 `Authorization: Bearer <CONTROL_TOKEN>`。

> ⚠️ **注意**：单独设置 `CLASH_SECRET` 而不设 `CONTROL_TOKEN` 无意义——open mode（无 `CONTROL_TOKEN`）跳过所有鉴权，`CLASH_SECRET` 不生效，`/api/clash/*` 完全开放。

两层鉴权职责分明：

| 鉴权层        | 变量            | 保护范围                                      | 不设的后果                                               |
| ------------- | --------------- | --------------------------------------------- | -------------------------------------------------------- |
| 登录/管理 API | `CONTROL_TOKEN` | 登录页 + `/api/control/*`（同源 cookie 替代） | 登录页不启用，`/api/control/*` 裸奔                      |
| mihomo 内核   | `CLASH_SECRET`  | `/api/clash/*`（同源代注入，跨源需自带）      | 自动生成随机值并打印到日志，同源可用，跨源从日志取值即可 |

**例外**：`/api/control/health` 和 `/api/control/info` 始终公开（dashboard 启动时需先探测能力）。

**Cookie 安全**：HttpOnly（防 XSS 偷取）+ SameSite=Strict（防 CSRF）+ HMAC-SHA256 签名（防伪造）+ 密钥从 `CONTROL_TOKEN` 派生（改密后所有会话自动失效）。有效期 30 天。

### 文件权限（PUID / PGID）

容器以 `PUID`/`PGID` 指定的用户身份运行。如果 `./data` 不可写（启动报 `permission denied`），确认宿主用户的 uid 与 `PUID` 一致：

```bash
echo $UID    # 查看当前用户 uid，填入 PUID（gid 同理查 id -g）
```

---

---

## 📊 与上游对比

| 功能                                                    | 上游               | 本项目（Go） | 说明                                                                           |
| ------------------------------------------------------- | ------------------ | ------------ | ------------------------------------------------------------------------------ |
| Profile 管理（local / remote / merge）                  | ✅                 | ✅           |                                                                                |
| Profile 合并（merge overlay / prepend / append）        | ✅                 | ⚠️           | merge overlay 完整，script overlay 静默跳过（上游通过 scriptRunner 执行）      |
| 订阅自动刷新                                            | ✅                 | ✅           |                                                                                |
| 内核控制（启停 / 重启 / 崩溃自重启）                    | ✅                 | ✅           |                                                                                |
| 配置校验 + 回滚                                         | ✅                 | ✅           |                                                                                |
| GEO 资源下载                                            | ✅                 | ✅           |                                                                                |
| WebDAV 备份 / 恢复                                      | ✅                 | ⚠️           | 备份完整；恢复时 script profile 静默丢弃（上游保留），managed overlay 正确恢复 |
| 配置分区编辑（`/config/section`）                       | ✅                 | ✅           |                                                                                |
| 运行时配置查看（`/config/runtime`）                     | ✅                 | ✅           |                                                                                |
| SSE 日志推送                                            | ✅                 | ✅           |                                                                                |
| WebSocket 端点（traffic / memory / connections / logs） | ✅                 | ✅           |                                                                                |
| 单端口同源代理（Clash API 集成）                        | ✅                 | ✅           |                                                                                |
| 登录页 + Cookie 鉴权                                    | ❌ 仅 Bearer Token | ✅ 新增      | Go 版独有                                                                      |
| Script 类型 overlay                                     | ✅                 | ❌           | 用户自定义 JS 转换配置（创建返回 501，迁移遗留静默跳过）                       |
| 可视化配置编辑器                                        | ✅                 | ❌           | 浏览器内 diff + patch 编辑，依赖 `@metacubexd/config-editor`（4 个端点）       |
| TUN 模式                                                | ✅                 | ❌           | TUN 设备配置生成 + 系统路由注入 + 卸载，桌面端专属（3 个端点）                 |
| 系统代理管理                                            | ✅                 | ❌           | 桌面端 OS 代理开关，服务端部署不涉及（2 个端点）                               |
| 内核版本切换                                            | ✅                 | ❌           | 下载 / 切换 mihomo 版本，桌面端专属（2 个端点）                                |

---

## ❓ FAQ

**Q：启动报 `/data` permission denied？**
A：容器内用户 uid 与 `./data` 宿主 owner 不一致。设 `PUID`/`PGID` 为宿主用户的 uid/gid（`docker-compose.yml` 里改，或 `echo $UID` 查看）。

**Q：在 Nginx / Caddy / Traefik 反代后面用，需要注意什么？**
A：必须透传 `X-Forwarded-Proto` 和 `X-Forwarded-Host`，否则前端会连接到错误的内部地址、cookie 的 Secure 标志也会判断错误。

**Q：如何更新版本？**
A：`docker compose pull && docker compose up -d`（预构建镜像），或修改 build-arg 后本地重建。数据在 `./data/` 中持久化，更新不影响。

**Q：设了 `CONTROL_TOKEN` 和 `CLASH_SECRET`，怎么访问？**
A：两种方式：

- **同源**（推荐）：浏览器打开 `http://server:8080`，输入 `CONTROL_TOKEN` 作为密码登录即可，Server 自动处理两层鉴权。
- **跨源**（metacubexd 官方面板）：在「Secret」栏填 `CLASH_SECRET`（连 mihomo 用）；若要远程调用 `/api/control/*` 管理 server，则用 `Authorization: Bearer <CONTROL_TOKEN>`。详见上方「[鉴权说明](#鉴权说明)」。

**Q：没设 `CLASH_SECRET`，怎么跨源访问？**
A：设了 `CONTROL_TOKEN` 时，Server 启动会自动生成随机 `CLASH_SECRET` 并打印到日志：`clash secret: <值> (auto-generated)`。从日志取值填入官方面板的「Secret」栏即可跨源连接。

**Q：怎么登出？**
A：当前 Dashboard 内没有登出按钮（上游 UI 不感知这套登录机制）。访问 `http://server:8080/api/auth/logout` 即可清除登录 Cookie，下次刷新会被跳回登录页。

---

## 📜 License

[MIT](LICENSE)
