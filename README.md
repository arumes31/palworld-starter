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
| `WEBSITE_URL` | Website URL used for in-game broadcasts | `https://pal.wowcraft.pw/` |
| `SERVERS` | Comma-separated server ids to enable multi-server mode | *unset (single server)* |

### Multiple Servers

Set `SERVERS` to a comma-separated list of server ids to manage several
Palworld containers from one page. Each server is configured with its own
variables (id uppercased, non-alphanumeric characters replaced by `_`):

| Variable | Description | Default |
|---|---|---|
| `SERVER_<ID>_CONTAINER` | Docker container name | the id |
| `SERVER_<ID>_NAME` | Display name on the website | the id |
| `SERVER_<ID>_ADDRESS` | Public game address (IP:port) | *empty* |
| `SERVER_<ID>_RESTPORT` | Host port of the container's Palworld REST API | `8212` |

Example:

```yaml
environment:
  SERVERS: "pal1,pal2"
  SERVER_PAL1_CONTAINER: "palworld-1"
  SERVER_PAL1_NAME: "Palworld Main"
  SERVER_PAL1_ADDRESS: "80.66.59.216:8211"
  SERVER_PAL1_RESTPORT: "8212"
  SERVER_PAL2_CONTAINER: "palworld-2"
  SERVER_PAL2_NAME: "Palworld Hardcore"
  SERVER_PAL2_ADDRESS: "80.66.59.216:8221"
  SERVER_PAL2_RESTPORT: "8222"
```

Each server keeps its own timer (`/hostmem/gamecontroller-<id>-time_remaining.json`),
tickers, backups and boot page; without `SERVERS` the legacy single-server
variables keep working unchanged.

---

## Set Up a Discord Bot (One-Time)

1. Go to the **Discord Developer Portal**.
2. Create a new Application > Bot tab > Add Bot.
3. Under **Privileged Gateway Intents**, enable **Server Members Intent** (if needed).
4. Copy the **Bot Token** (keep it secret!).
5. In your Discord server: OAuth2 > URL Generator > Scopes: `bot` > Permissions: `Create Instant Invite` > Paste URL to invite bot to server.
6. Note your **Server ID** (Guild ID) and **Channel ID** (e.g., general channel: Right-click channel > Copy ID, with Developer Mode enabled in Discord settings).
