# Palworld Server Control

A lightweight web application that controls and manages a Palworld server running in a Docker container, including auto-pausing/stopping on idle, scheduled RCON broadcasts, automatic backups, and Discord invite link caching.

## Features

- **Timer & Auto-Pause**: Automatically counts down and stops the server container when the timer expires.
- **Captcha Verification**: Prevents bots from launching or extending server lifetime. Features German/English mathematical puzzles with copy-protection (disabled copy/paste on puzzle text).
- **Discord Integration**: Cache and serve dynamic server invites via a Discord bot.
- **RCON Commands**: Broadcast periodic status updates to active players.
- **Backups**: Automatically trigger container-level backups.

## Building & Running

### Prerequisites
- Go 1.26+ installed on your host.
- Docker running and accessible (via `DOCKER_HOST` or standard socket).

### Run Locally
```bash
go run .
```

### Run Tests
```bash
go test -v ./...
```

### Build Binary
```bash
go build -o palworld-starter .
```

---

## Configuration

Set the following environment variables:

| Variable | Description | Default |
|---|---|---|
| `DOCKER_HOST` | URI of the Docker daemon socket | `unix:///var/run/docker.sock` |
| `DOCKER_CONTAINER_NAME` | Name of the container managing Palworld | `my_container` |
| `DISCORD_BOT_TOKEN` | Token for the Discord bot | *Optional* |
| `DISCORD_GUILD_ID` | Guild ID of your Discord Server | *Optional* |
| `DISCORD_CHANNEL_ID` | Channel ID for generating invites | *Optional* |
| `DISCORD_FALLBACK_URL` | Fallback invitation link | `https://discord.gg/XXXXXINVITENOTFOUNDXXXXXX` |
| `SERVER_ADDRESS` | Public game address shown on the page (IP:port) | `80.66.59.216:8211` |
| `REST_API_HOST` | Hostname/IP of the Palworld REST API | `host.docker.internal` |
| `WEBSITE_URL` | Website URL used for in-game broadcasts | `https://pal.wowcraft.pw/` |
| `GOOGLE_SITE_VERIFICATION` | Google Search Console verification token | *Optional* |
| `BING_SITE_VERIFICATION` | Bing Webmaster Tools verification token | *Optional* |
| `YANDEX_SITE_VERIFICATION` | Yandex Webmaster verification token | *Optional* |
| `SERVERS` | Comma-separated server ids to enable multi-server mode | *unset (single server)* |
| `ADMIN_PASSWORD` | Palworld REST API admin password | *scraped from the game container's env* |
| `ADMIN_GUI_PASSWORD` | Global admin-GUI login password. Enables the admin GUI at `/admin` when set | *unset (admin GUI disabled)* |
| `SESSION_KEY` | Secret used to encrypt session cookies; set it so sessions survive restarts | *random per start* |
| `STOP_TOKEN` | Shared secret for `POST /stop` via the `X-Stop-Token` header (needed behind a reverse proxy) | *unset (loopback-only)* |

### Search Engine Optimization & Verification

- **Zero-Config Multi-domain SEO**: Sitemap URLs, `robots.txt` paths, and canonical `<link>` elements are dynamically resolved using the requesting host header (falling back to `WEBSITE_URL` if set). This ensures search engine crawlers index the correct domain (e.g., `freepalworld.wowcraft.pw`).
- **Meta-Tag Verification**: Supply `GOOGLE_SITE_VERIFICATION`, `BING_SITE_VERIFICATION`, or `YANDEX_SITE_VERIFICATION` environment variables to automatically output verification tags into the landing page `<head>`.
- **File-Based Verification**: Place custom Search Console HTML/XML files (e.g., `google*.html`, `yandex_*.html`, `pinterest-*.html`, `BingSiteAuth.xml`) directly inside the `static/` folder. The application serves them directly at the root (e.g., `/google123456789.html`).

### Multiple Servers

Set `SERVERS` to a comma-separated list of server ids to manage several
Palworld containers from one page. Each server is configured with its own
variables (id uppercased, non-alphanumeric characters replaced by `_`):

| Variable | Description | Default |
|---|---|---|
| `SERVER_<ID>_CONTAINER` | Docker container name | the id |
| `SERVER_<ID>_NAME` | Display name on the website | the id |
| `SERVER_<ID>_ADDRESS` | Public game address (IP:port) | *empty* |
| `SERVER_<ID>_RESTHOST` | Hostname/IP of the Palworld REST API | `host.docker.internal` |
| `SERVER_<ID>_RESTPORT` | Host port of the container's Palworld REST API | `8212` |
| `SERVER_<ID>_ADMIN_PASSWORD` | REST API admin password for this server | `ADMIN_PASSWORD` |
| `SERVER_<ID>_ADMIN_GUI_PASSWORD` | Seeds a per-server admin-GUI password granting access to this server only | *unset* |

Example:

```yaml
environment:
  SERVERS: "pal1,pal2"
  SERVER_PAL1_CONTAINER: "palworld-1"
  SERVER_PAL1_NAME: "Palworld Main"
  SERVER_PAL1_ADDRESS: "80.66.59.216:8211"
  SERVER_PAL1_RESTHOST: "palworld-1"
  SERVER_PAL1_RESTPORT: "8212"
  SERVER_PAL2_CONTAINER: "palworld-2"
  SERVER_PAL2_NAME: "Palworld Hardcore"
  SERVER_PAL2_ADDRESS: "80.66.59.216:8221"
  SERVER_PAL2_RESTHOST: "palworld-2"
  SERVER_PAL2_RESTPORT: "8222"
```

Each server keeps its own timer (`/hostmem/gamecontroller-<id>-time_remaining.json`),
tickers, backups and boot page; without `SERVERS` the legacy single-server
variables keep working unchanged.

---

## Admin GUI

Set `ADMIN_GUI_PASSWORD` to enable an authenticated admin dashboard at **`/admin`**.
Without it the admin routes are not registered at all (they return 404).

### Access model

- The **global password** (`ADMIN_GUI_PASSWORD`) grants access to every managed
  server.
- Each server can additionally have its own **server-only password** that grants
  admin access to that one server. Global admins set these from the dashboard
  (*Server-only password* card), or you can seed them from the environment with
  `SERVER_<ID>_ADMIN_GUI_PASSWORD`. Per-server passwords are stored salted and
  hashed (SHA-256) in `admin.json` in the state directory.

Login is a single password field: the app resolves it to the matching scope.
Sessions are the same encrypted cookies used elsewhere and last 8 hours. All
admin actions are CSRF-protected.

### Actions per server

- **Announce** – broadcast a message to online players.
- **Kick / Ban** – one click on a live player (uses the REST API user id),
  plus an **Unban by user id** field.
- **Save world**, **Start**, **Stop**.
- **Reboot now** – graceful restart with a choice of announcement lead time.
- **Cancel reboot** – abort an in-progress reboot.

### Scheduled reboots

Add **daily** (at `HH:MM`) or **one-time** (at a specific date/time) reboots per
server. Each schedule has an announcement lead time (default 10 minutes). Times
are interpreted in the container's local timezone — set the `TZ` environment
variable to control it (defaults to UTC).

Before a reboot, players are warned in-game on this cadence, counting down to the
restart:

- every **minute** while more than a minute remains (10, 9, … 1 min),
- every **10 seconds** under a minute (50s, 40s, 30s),
- every **second** under 30 seconds (30, 29, … 1s).

Announcements are only sent while players are online; an empty (or auto-paused)
server is restarted immediately without a countdown. A freshly rebooted server is
kept alive for at least an hour so the idle timer does not stop it again right
away.

---

## Set Up a Discord Bot (One-Time)

1. Go to the **Discord Developer Portal**.
2. Create a new Application > Bot tab > Add Bot.
3. Under **Privileged Gateway Intents**, enable **Server Members Intent** (if needed).
4. Copy the **Bot Token** (keep it secret!).
5. In your Discord server: OAuth2 > URL Generator > Scopes: `bot` > Permissions: `Create Instant Invite` > Paste URL to invite bot to server.
6. Note your **Server ID** (Guild ID) and **Channel ID** (e.g., general channel: Right-click channel > Copy ID, with Developer Mode enabled in Discord settings).
