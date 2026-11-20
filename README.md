# Linux WireGuard Manager

> A web panel to manage WireGuard, with an optional **remote Linux server** integration over SSH — deploy once, manage your VPN from any browser.

**Author:** Aldo · [aldo-expert.com](https://aldo-expert.com)
**Company:** [indonetsoft.com](https://indonetsoft.com)
**Stack:** Go (backend) · Framework7 PWA (frontend, embedded into the binary)
**License:** MIT

> Forked from this author's sibling [mikrotik-wireguard](https://github.com/aldoexpert/mikrotik-wireguard)
> project, which targets a Mikrotik RouterOS 7 device instead of a plain
> Linux server -- the two share the same multi-interface architecture,
> client model and frontend, and differ only in how the server config is
> pushed to the device that actually runs WireGuard.

---

## Table of Contents

1. [Installation (Docker)](#installation-docker)
2. [Overview & Architecture](#overview--architecture)
3. [Environment Variables](#environment-variables)
4. [Development (without Docker)](#development-without-docker)
5. [Features](#features)
6. [Roadmap](#roadmap)
7. [Linux Server Integration](#linux-server-integration)

--- 

## Installation (Docker)

### Prerequisites

- Docker ≥ 24.x
- Docker Compose v2 (`docker compose`, not the old `docker-compose`)
- One free port on the server (default `8220`) — serves **both** the UI and the API

> **No build needed.** The image is pre-built on GitHub Container Registry
> for `linux/amd64` and `linux/arm64`: `ghcr.io/aldoexpert/linux-wireguard`.
> Docker automatically pulls the one matching your server's architecture.

### Installation Steps

**1. Get `docker-compose.yaml` and `.env.example`**

The image is built by CI and published to GHCR, so you don't need the
source code at all -- just these two files:

```bash
mkdir linux-wireguard && cd linux-wireguard
curl -fsSLO https://raw.githubusercontent.com/aldoexpert/linux-wireguard/main/docker-compose.yaml
curl -fsSLO https://raw.githubusercontent.com/aldoexpert/linux-wireguard/main/.env.example
```

(Or `git clone https://github.com/aldoexpert/linux-wireguard.git` if you
want the full source too.)

**2. Create the config file**

```bash
cp .env.example .env
```

**3. Edit `.env` — fill in the required values**

```bash
nano .env
```

At minimum, set:

```env
# Generate with: openssl rand -hex 32
LXWG_JWT_SECRET=replace-with-a-long-random-string

# The address clients use to connect (your VPS/router's public IP)
LXWG_ENDPOINT_ADDRESS=203.0.113.10
```

> Generate a secret with: `openssl rand -hex 32`

**4. Run it**

```bash
docker compose pull
docker compose up -d
```

Docker pulls `ghcr.io/aldoexpert/linux-wireguard:latest` — no local build.

**5. Open the app**

```
http://<SERVER-IP>:8220
```

Log in with:
- **Username:** `admin`
- **Password:** `admin`

> **Change the password immediately** after your first login, on the Profile page.

---

### Update / Upgrade

Since the image is pre-built on the registry, upgrading is just a pull + restart:

```bash
docker compose pull
docker compose up -d
```

> **Don't run `docker compose down -v`** — the `-v` flag deletes the volume
> and all your data (clients, keys, settings).

Persistent data lives on the host in `./data` (bind mount, see `docker-compose.yaml`), so it survives image upgrades untouched.

---

### Build from Source (Developer)

If you want to build the image yourself (e.g. after modifying the code):

```bash
docker compose build --no-cache
docker compose up -d
```

To build multi-arch and push to your own registry:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t ghcr.io/<your-username>/linux-wireguard:latest --push .
```

---

### Port Used

| Port | Purpose |
|---|---|
| `8220` | UI + API, single port (set via `APP_PORT` in `.env`) |
| `51820/udp` | The WireGuard tunnel itself, on whichever host actually runs `wg0` (not this container, unless you still rely on a local interface — see the commented `network_mode: host` block in `docker-compose.yaml`) |

---

### Troubleshooting

**View logs:**
```bash
docker compose logs -f app
```

**Restart:**
```bash
docker compose restart app
```

**Container status:**
```bash
docker compose ps
```

**Forgot the admin password:**

There's no `reset-password` CLI yet. The initial admin user is only
auto-seeded while `./data/users/` is empty, so to get back in:

```bash
docker compose stop app
rm -rf ./data/users
# optionally set LXWG_USERNAME / LXWG_PASSWORD in .env first
docker compose up -d
```

A fresh admin user is re-seeded from `LXWG_USERNAME` / `LXWG_PASSWORD`
(default `admin` / `admin`) on the next start. This only touches the
`users/` data — clients, server keys and settings are untouched.

---

## Overview & Architecture

```
linux-wireguard/
├── backend/                 # Go REST API + embedded frontend
│   ├── cmd/api/             # entry point
│   ├── web/                 # //go:embed of the built frontend (web/dist/)
│   └── internal/
│       ├── core/            # module/controller/method router + JSON response
│       ├── config/          # flags + env vars (LXWG_*)
│       ├── middleware/      # auth (JWT), CORS, logging, recovery
│       ├── library/         # store (jsondb), wireguard, linuxserver (SSH), network, emailer, telegram, jwt
│       ├── model/           # data structs (client, server, user, settings, ...)
│       └── modules/         # auth, users, wireguard, network, system
├── frontend/                 # Framework7 PWA (SPA, consumes the backend REST API)
│   └── src/
│       ├── app.f7           # shell (panel menu + main view)
│       ├── js/               # app.js, routes.js, store.js, api.js
│       └── pages/            # login, clients, server, settings, users, ...
├── data/                    # persistent data (users, clients, settings) -- JSON files, gitignored
├── Dockerfile                # multi-stage: builds the frontend, embeds it into the Go binary
└── docker-compose.yaml       # single service, single port, ready to run
```

The backend is a **pure REST API** (it renders no HTML) -- every endpoint
follows the `/{module}/{controller}/{method}/{param?}` pattern and replies
with a consistent JSON envelope:

```json
{ "status": true, "code": 200, "message": "", "data": {}, "datas": [] }
```

Auth uses a **JWT Bearer token** (not a cookie session). The frontend is a
static Framework7 PWA that is built and embedded straight into the Go
binary (see `Dockerfile` and `backend/web/embed.go`), so in production both
the API and the UI are served from the same origin/port -- no CORS/CSRF
concerns there either. There's no granular RBAC -- just an `admin` flag per
user, matching wireguard-ui's original design.

Data is stored as JSON files under `data/` on the host (no MySQL/Postgres),
so deployment stays lightweight for a single container.

---

## Environment Variables

Every variable uses the `LXWG_` prefix. The full list is in
[`.env.example`](.env.example). The important ones:

| Variable | Default | Description |
|---|---|---|
| `LXWG_JWT_SECRET` | *(required)* | The JWT session signing key. Change it for every production deploy. |
| `LXWG_DISABLE_LOGIN` | `false` | Disables authentication. **Never use this in production.** |
| `LXWG_CORS_ALLOWED_ORIGINS` | `*` | Allowed CORS origin(s). Not needed for the normal single-port setup (same-origin); only set this if you call the API from a different origin. |
| `LXWG_USERNAME` / `LXWG_PASSWORD` | `admin` / `admin` | Initial admin user, only used while the database is still empty. |
| `LXWG_SERVER_INTERFACE_ADDRESSES` | `10.252.1.1/24` | The WireGuard VPN subnet. The server itself owns `.1`; client IPs (auto-suggested or manual) start from `.2` -- `.1`, the network address (`.0`) and the broadcast address (`.255`) are always excluded. |
| `LXWG_SERVER_LISTEN_PORT` | `51820` | The WireGuard UDP port. |
| `LXWG_ENDPOINT_ADDRESS` | *(auto-detected public IP)* | The address clients use to connect. |
| `LXWG_SUBNET_RANGES` | *(empty)* | Extra IP pools, format `name1:cidr1,cidr2;name2:cidr3`. |
| `LXWG_CONFIG_FILE_PATH` | `/app/data/wg0.conf` (Docker) | Where the locally-written `wg0.conf` is saved. Defaults into the persisted data volume, since the image has no `/etc/wireguard` (no `wireguard-tools`, no local wg0 device -- this project targets a remote Linux server over SSH). |
| `LXWG_SMTP_*`, `LXWG_SENDGRID_API_KEY` | *(empty)* | Send client configs by email. |
| `LXWG_TELEGRAM_TOKEN` | *(empty)* | Send client configs via a Telegram bot. |
| `APP_PORT` | `8220` | Host port published by `docker-compose.yaml` (container always listens on `8220` internally). |

---

## Development (without Docker)

### Backend

```bash
cd backend
go run ./cmd/api
# or with env overrides:
LXWG_DISABLE_LOGIN=true LXWG_CONFIG_FILE_PATH=/tmp/wg0.conf go run ./cmd/api
```

### Frontend

```bash
cd frontend
npm install --legacy-peer-deps
npm run dev          # dev server on :5173, proxies API calls to :8220 (see vite.config.js)
npm run build         # production build into frontend/www/
```

This two-process setup (`:5173` + `:8220`) is for local development only. In
production (Docker), `npm run build`'s output is copied into
`backend/web/dist/` and embedded into the Go binary at compile time (see
`Dockerfile` and `backend/web/embed.go`), so the shipped app is a single
binary serving everything on one port -- no separate frontend
container/process.

To test that single-binary setup locally without Docker:

```bash
cd backend && make build-frontend   # builds ../frontend and syncs into web/dist/
go run ./cmd/api                    # now serving UI + API on :8220
```

---

## Features

- WireGuard client management: create/edit/delete, enable/disable, automatic
  IP suggestion, QR code, `.conf` download, send via email/Telegram
- WireGuard server configuration: interface address/port, key pair, apply
  config
- Global settings: DNS, MTU, persistent keepalive, firewall mark, etc.
- Multi-user (admin & manager) without granular RBAC
- Real-time peer connection status (handshake, traffic) -- from the local
  WireGuard device, or from a remote Linux server when that integration is enabled
- **Linux Server integration over SSH**: push the server interface and
  client peers to a real remote server (wg-quick + systemd) instead of (or
  alongside) the local `wg0.conf` -- see [Linux Server Integration](#linux-server-integration) below
- **Copy Mikrotik Client Setup Command**: generate the RouterOS terminal
  commands to configure a *client* that is itself a Mikrotik router (e.g.
  a branch/home router), since RouterOS has no importer for the wg-quick
  `.conf` format the other client delivery options use
- Wake-on-LAN for hosts on the same LAN
- Installable PWA (Framework7) -- can be installed as an app on desktop/mobile

---

## Roadmap

- [ ] Multi-server support per instance (today: one Linux server per app instance)
- [ ] Pin the Linux server's SSH host key fingerprint instead of trusting it
      on first connect (see [Linux Server Integration](#linux-server-integration) below)
- [ ] `reset-password` CLI (today: manual, see [Troubleshooting](#troubleshooting))

---

## Linux Server Integration

When enabled, clicking **Apply Configuration** on the Server page pushes the
complete WireGuard interface config (interface + every enabled client as a
peer) to a remote Linux server over SSH, in addition to writing the local
`wg0.conf`. The Status page also switches to reading live handshake/traffic
data from that server instead of the local kernel WireGuard device.

- Unlike a Mikrotik RouterOS device (there's no incremental "add one peer"
  command or per-peer comment field on a wg-quick interface), applying
  always renders and pushes the *complete* desired config file for the
  interface, then restarts the `wg-quick@<ifName>` systemd unit -- simpler,
  and no less safe, since wg-quick already treats a config reload as
  replacing the whole peer set.
- The config file is written to `/etc/wireguard/<ifName>.conf` on that
  server, where `<ifName>` is each interface's own "Linux wg-quick
  interface name" field (e.g. `wg0`) -- one server can host several
  WireGuard interfaces.
- Authentication is **always by SSH key**, never password. Every command
  run over that connection is prefixed with `sudo`, since the dedicated
  user below is not expected to be root.

### One-time setup on the Linux server

Create a dedicated, least-privilege user instead of using an existing admin
account for this integration:

```bash
# on the server, create a dedicated user with no login shell of its own concern:
sudo useradd -m -s /bin/bash wgmanager

# generate a keypair on the machine running this app and install the public half:
#   ssh-keygen -t ed25519 -f ./wgmanager -N "" -C "linux-wireguard-app"
sudo mkdir -p /home/wgmanager/.ssh
sudo tee /home/wgmanager/.ssh/authorized_keys < ./wgmanager.pub > /dev/null
sudo chown -R wgmanager:wgmanager /home/wgmanager/.ssh
sudo chmod 700 /home/wgmanager/.ssh
sudo chmod 600 /home/wgmanager/.ssh/authorized_keys

# grant passwordless sudo for exactly the commands this integration runs --
# never blanket root access. Adjust the binary paths below if `which mkdir
# tee chmod systemctl wg wg-quick ip true` reports different ones on your
# distro:
sudo tee /etc/sudoers.d/wgmanager-wireguard > /dev/null <<'EOF'
wgmanager ALL=(root) NOPASSWD: /usr/bin/mkdir -p /etc/wireguard, /usr/bin/tee /etc/wireguard/*, /usr/bin/chmod 600 /etc/wireguard/*, /usr/bin/mkdir -p /etc/systemd/system/wg-quick@*.service.d, /usr/bin/tee /etc/systemd/system/wg-quick@*.service.d/*, /usr/bin/systemctl daemon-reload, /usr/bin/systemctl enable wg-quick@*, /usr/bin/systemctl restart wg-quick@*, /usr/bin/systemctl reset-failed wg-quick@*, /usr/bin/wg show * dump, /usr/bin/wg syncconf *, /usr/bin/wg-quick strip *, /usr/sbin/ip link show *, /usr/bin/true
EOF
sudo visudo -c   # verify the file before relying on it
```

Make sure `wireguard-tools` is installed on that server (`apt install
wireguard-tools` on Debian/Ubuntu) so `wg-quick` and `wg` are available.

### App-side configuration

In the panel, go to **WireGuard → Linux Server Integration** (admin only)
and fill in:

| Field | Example |
|---|---|
| Host / IP Address | `203.0.113.10` (example -- use your server's actual address) |
| SSH Port | `22` (or a custom port, e.g. `8222`, if that's what sshd listens on) |
| Username | `wgmanager` |
| SSH Private Key | the **private** half of the keypair generated above |

Use **Test Connection** to verify before saving, then toggle **Enabled** and
save. The next "Apply Configuration" on the Server page syncs to that
server. The WireGuard interface name pushed there comes from each
interface's own settings on the Interfaces page (admin only), not from this
form -- see [Overview & Architecture](#overview--architecture).

### Running this app on the same box as the WireGuard server

If you run this app's own container on the same Linux server that will
actually run `wg-quick`, point the Linux Server Integration's Host/IP at
that same server (its real address, or an address this app can SSH to --
not `127.0.0.1`/`localhost` *inside the container*, since the container has
no SSH server of its own; point at the host instead). This makes Apply
Configuration push the config straight to `/etc/wireguard/<ifName>.conf` on
the host over SSH and restart its `wg-quick@<ifName>` service, so there is
no second, out-of-sync copy of the config to track by hand -- the app's own
`/app/data/wg0.conf` (inside its Docker volume) is only its local working
copy, never what `wg-quick` on the host actually reads.

### Known gotchas

- **AllowedIPs is a routing filter on both ends, not just a permission
  list.** If a client's `AllowedIPs` doesn't include a private subnet it
  needs to reach through the tunnel, traffic to that subnet silently
  bypasses the tunnel instead of erroring -- the UI warns about this when
  `AllowedIPs` isn't the full-tunnel default (`0.0.0.0/0`).
- **Don't reference Docker's `DOCKER-USER` chain in PostUp/PreDown.**
  `wg-quick@<ifName>.service` has no ordering guarantee relative to
  `docker.service`, so if it starts first, that chain doesn't exist yet and
  the whole unit fails. Plain `FORWARD`/`MASQUERADE` rules (without
  `DOCKER-USER`) work the same without that race, and the UI warns if it
  detects that chain name in either field. As of this integration, Apply
  Configuration also pushes a systemd drop-in ordering `wg-quick@<ifName>`
  after `docker.service` as a second line of defense -- harmless even on a
  server with no Docker installed.

### If the client is itself a Mikrotik router

A phone, laptop or Linux box can all import a client's `.conf` directly
(see **Download .conf** on the Clients page), but RouterOS has no importer
for that file format. For that case, use **Copy Mikrotik Client Setup
Command** on the same client's action menu instead -- it generates the
RouterOS terminal commands to paste into that *other* router's own
terminal, independent of whether your own server here is a Linux box or
(on the sibling mikrotik-wireguard project) a Mikrotik device too.
