# metacubexd-server-go

> A Go rewrite of metacubexd-server, replacing the upstream Node.js implementation with added Clash API same-origin proxy. The browser only needs a single port for all management operations — no need to expose the mihomo control port separately. Suitable for self-hosting / NAS / VPS.

[**中文文档**](README.zh-CN.md) | **English**

---

## ✨ Features

- 🌐 **Cross-platform static binary** — `linux/amd64`, `linux/arm64`, zero external dependencies, no Node.js runtime required
- ⚡ **Lightweight** — Smaller footprint (~19MB vs ~50MB), lower memory usage, faster startup
- 🔐 **Built-in authentication** — Login page + Cookie-based auth, no need for extra reverse proxy auth

---

## 🚀 Quick Start

The image `sion10032/metacubexd-server:latest` is published on Docker Hub — pull directly, no need to clone the source.

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
      # 9090 (Clash API) is not exposed by default; uncomment if external direct access to mihomo is needed
      # - "9090:9090"
    volumes:
      - ./data:/data
    environment:
      CONTROL_TOKEN: "<your-password>" # Login page password; omit to disable the login page and allow open access
      TZ: Asia/Shanghai
      PUID: 1000 # Match host user uid to avoid ./data permission issues (check with echo $UID)
      PGID: 1000
```

Open [http://localhost:8080](http://localhost:8080) in your browser → enter the password (i.e. `CONTROL_TOKEN`) to access the Dashboard.

> Setting `CONTROL_TOKEN` enables the login page; omitting it allows open access (suitable for internal networks). If `CLASH_SECRET` is not set, a random value is auto-generated on startup and printed to logs — cross-origin panels can read it from the logs. See [Authentication](#authentication) below for details.

### Binary

No Docker required — download the static binary, prepare the mihomo kernel and UI resources:

```bash
./metacubexd-server
```

Required environment variables are listed in the [Configuration](#️-configuration) section below.

### systemd / OpenRC Service

Deploy the binary as a system service (requires root or sudo):

```bash
# One-click install (auto-detects systemd / OpenRC, generates random secrets, enables and starts the service)
curl -fsSL https://raw.githubusercontent.com/Sion10032/metacubexd-server-go/main/deploy/metacubexd-ctl.sh | sudo bash
```

Manual installation:

```bash
# 1. Install the binary
sudo install -m 0755 metacubexd-server-go /usr/local/bin/metacubexd-server

# 2. Create system user
sudo useradd --system --no-create-home --shell /usr/sbin/nologin metacubexd

# 3. Install service files (choose based on your init system)

# systemd:
sudo cp deploy/systemd/metacubexd.service /etc/systemd/system/
sudo mkdir -p /etc/metacubexd
cp deploy/systemd/metacubexd.env.sample /etc/metacubexd/metacubexd.env
# Edit /etc/metacubexd/metacubexd.env, set CONTROL_TOKEN and CLASH_SECRET
sudo systemctl daemon-reload
sudo systemctl enable --now metacubexd

# OpenRC:
sudo cp deploy/openrc/metacubexd.initd /etc/init.d/metacubexd
sudo cp deploy/openrc/metacubexd.confd /etc/conf.d/metacubexd
# Edit /etc/conf.d/metacubexd, set CONTROL_TOKEN and CLASH_SECRET
sudo rc-update add metacubexd default
sudo rc-service metacubexd start
```

> **mihomo kernel**: The service does not include mihomo; install it at `/usr/local/bin/mihomo` (or set `MIHOMO_BIN` in the env file to the actual path). TUN mode requires `CAP_NET_ADMIN` capability (the systemd unit already has `AmbientCapabilities` configured; OpenRC's `start_pre` runs `setcap` on mihomo).

---

## ⚙️ Configuration

### Ports

| Port   | Purpose                               | External Exposure                                |
| ------ | ------------------------------------- | ------------------------------------------------ |
| `8080` | Dashboard + Control API + Clash API proxy | ✅ Browser connects here only              |
| `7890` | mihomo mixed proxy (client traffic)   | ✅ Proxy clients connect here                    |
| `9090` | mihomo Clash API                      | ❌ Not exposed by default (same-origin proxy covers this, expose if external direct access needed) |

### Environment Variables

| Variable        | Default                  | Description                                                                                                                          |
| --------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `CONTROL_PORT`  | `8080`                  | Dashboard and API listen port                                                                                                        |
| `MIXED_PORT`    | `7890`                  | mihomo mixed proxy port                                                                                                              |
| `DATA_DIR`      | `/data`                 | Configuration and data directory                                                                                                     |
| `MIHOMO_BIN`    | `/usr/local/bin/mihomo` | mihomo kernel path                                                                                                                   |
| `CONTROL_TOKEN` | _(empty)_               | Login page password, also used as Bearer Token for cross-origin access to `/api/control/*` (omit = no login page, `/api/control/*` open) |
| `CLASH_SECRET`  | _(empty)_               | mihomo secret. Injected automatically by the server for same-origin access; for cross-origin (official panel), enter this value. If unset, a random value is auto-generated on startup and printed to logs |
| `TZ`            | _(container default)_   | Timezone, e.g. `Asia/Shanghai`                                                                                                       |
| `PUID` / `PGID` | `1000`                  | uid/gid for the user running inside the container; match the host user to avoid permission issues                                    |

For production, it is recommended to set both Token and Secret (generate random strings: `openssl rand -hex 16`):

```yaml
environment:
  CONTROL_TOKEN: "<random-string>"
  CLASH_SECRET: "<random-string>"
  TZ: Asia/Shanghai
```

#### Authentication

When `CONTROL_TOKEN` is set, the Server enables login page authentication. Two API paths use different credentials for different resource domains:

| API Path         | Resource Owner                                       | Same-Origin Access (after login)                     | Cross-Origin Access (official panel / API clients)                     |
| ---------------- | ---------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------------------------- |
| `/api/control/*` | **Go server** (start/stop kernel, profiles, config, backups) | Cookie-based                                     | `Authorization: Bearer <CONTROL_TOKEN>`                                |
| `/api/clash/*`   | **mihomo kernel** (traffic, proxies, connections, logs)      | Server auto-injects CLASH_SECRET              | `Authorization: Bearer <CLASH_SECRET>` or `?token=<CLASH_SECRET>`      |

**Same-origin access** (browser opens `http://server:8080`): If not logged in, auto-redirects to `/login`. After entering the password (i.e. `CONTROL_TOKEN`), the server issues a signed Cookie — subsequent requests (including WebSocket / SSE) are automatically authenticated. Both API paths are authorized via cookie, and CLASH_SECRET is injected by the server transparently.

**Cross-origin access**:

- **metacubexd official panel** (only calls `/api/clash/*`): Enter `CLASH_SECRET` in the "Secret" field. If `CLASH_SECRET` is not set, the server auto-generates a random value on startup and prints it to the startup log (`clash secret: <value> (auto-generated)`) — retrieve it from the logs.
- **API clients calling `/api/control/*`**: Include `Authorization: Bearer <CONTROL_TOKEN>` in the request header.

> ⚠️ **Note**: Setting only `CLASH_SECRET` without `CONTROL_TOKEN` is meaningless — open mode (no `CONTROL_TOKEN`) skips all authentication, `CLASH_SECRET` has no effect, and `/api/clash/*` is fully open.

Two authentication layers with distinct responsibilities:

| Auth Layer      | Variable        | Protected Resources                               | Consequences if Unset                                              |
| --------------- | --------------- | ------------------------------------------------- | ------------------------------------------------------------------ |
| Login / Admin API | `CONTROL_TOKEN` | Login page + `/api/control/*` (cookie for same-origin) | Login page disabled, `/api/control/*` exposed                |
| mihomo kernel   | `CLASH_SECRET`  | `/api/clash/*` (auto-injected for same-origin, required for cross-origin) | Random value auto-generated and printed to logs; same-origin works, cross-origin reads from logs |

**Exceptions**: `/api/control/health` and `/api/control/info` are always public (the dashboard needs to probe capabilities on startup).

**Cookie security**: HttpOnly (prevents XSS theft) + SameSite=Strict (prevents CSRF) + HMAC-SHA256 signed (prevents forgery) + key derived from `CONTROL_TOKEN` (all sessions are invalidated automatically when the password is changed). Valid for 30 days.

### File Permissions (PUID / PGID)

The container runs as the user specified by `PUID`/`PGID`. If `./data` is not writable (startup shows `permission denied`), verify that the host user's uid matches `PUID`:

```bash
echo $UID    # Check current user's uid, set as PUID (for gid, use id -g)
```

---

## 📊 Comparison with Upstream

| Feature                                                  | Upstream | This Project (Go) | Notes                                                                  |
| -------------------------------------------------------- | -------- | ----------------- | ---------------------------------------------------------------------- |
| Profile management (local / remote / merge)              | ✅       | ✅                |                                                                        |
| Profile merging (merge overlay / prepend / append)       | ✅       | ⚠️                | Merge overlay complete; script overlay silently skipped (upstream uses scriptRunner) |
| Auto-subscription refresh                                | ✅       | ✅                |                                                                        |
| Kernel control (start / stop / restart / auto-restart)   | ✅       | ✅                |                                                                        |
| Config validation + rollback                             | ✅       | ✅                |                                                                        |
| GEO resource download                                    | ✅       | ✅                |                                                                        |
| WebDAV backup / restore                                  | ✅       | ⚠️                | Backup complete; script profiles silently dropped on restore (upstream retains), managed overlay restored correctly |
| Config section editing (`/config/section`)               | ✅       | ✅                |                                                                        |
| Runtime config view (`/config/runtime`)                  | ✅       | ✅                |                                                                        |
| SSE log push                                             | ✅       | ✅                |                                                                        |
| WebSocket endpoints (traffic / memory / connections / logs) | ✅    | ✅                |                                                                        |
| Single-port same-origin proxy (Clash API integration)    | ✅       | ✅                |                                                                        |
| Login page + Cookie auth                                 | ❌ Bearer Token only | ✅ **New**   | Go version exclusive                                                   |
| Script type overlay                                      | ✅       | ❌                | User custom JS for config transformation (returns 501 on create, silently skipped on migration) |
| Visual config editor                                     | ✅       | ❌                | In-browser diff + patch editing, depends on `@metacubexd/config-editor` (4 endpoints) |
| TUN mode                                                 | ✅       | ❌                | TUN device config generation + system route injection / cleanup, desktop-only (3 endpoints) |
| System proxy management                                  | ✅       | ❌                | Desktop OS proxy toggle, not relevant for server deployments (2 endpoints) |
| Kernel version switching                                 | ✅       | ❌                | Download / switch mihomo versions, desktop-only (2 endpoints)          |

---

## ❓ FAQ

**Q: Permission denied on `/data` at startup?**
A: The uid of the user inside the container does not match the `./data` directory owner on the host. Set `PUID`/`PGID` to match the host user's uid/gid (change in `docker-compose.yml`, or check with `echo $UID`).

**Q: What should I watch out for when using behind Nginx / Caddy / Traefik?**
A: You must forward `X-Forwarded-Proto` and `X-Forwarded-Host`, otherwise the frontend will connect to the wrong internal address and the cookie's Secure flag will be mis-evaluated.

**Q: How do I update?**
A: `docker compose pull && docker compose up -d` (pre-built images), or rebuild locally with modified build-args. Data is persisted in `./data/` — updates won't affect it.

**Q: I've set both `CONTROL_TOKEN` and `CLASH_SECRET`. How do I access?**
A: Two methods:

- **Same-origin** (recommended): Open `http://server:8080` in a browser, enter `CONTROL_TOKEN` as the password to log in. The server handles both auth layers automatically.
- **Cross-origin** (metacubexd official panel): Enter `CLASH_SECRET` in the "Secret" field (for mihomo connection). To call `/api/control/*` remotely to manage the server, use `Authorization: Bearer <CONTROL_TOKEN>`. See [Authentication](#authentication) above for details.

**Q: I haven't set `CLASH_SECRET`. How do I access cross-origin?**
A: When `CONTROL_TOKEN` is set, the server auto-generates a random `CLASH_SECRET` on startup and prints it to logs: `clash secret: <value> (auto-generated)`. Retrieve it from the logs and enter it in the official panel's "Secret" field for cross-origin connection.

**Q: How do I log out?**
A: The current Dashboard does not have a logout button (upstream UI is unaware of this auth mechanism). Visit `http://server:8080/api/auth/logout` to clear the login Cookie — the next refresh will redirect back to the login page.

---

## 📜 License

[MIT](LICENSE)