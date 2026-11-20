# Backend -- Linux WireGuard Manager

Go REST API. See the root [README.md](../README.md) for the full
architecture overview (backend + frontend + docker).

## Running

```bash
go run ./cmd/api
# or
make run
```

See `../.env.example` for the full list of environment variables (`LXWG_*`).

## Structure

```
cmd/api/main.go           entry point: load config, init db, register modules, start server
internal/core/             module/controller/method router + standard JSON response
internal/config/           flags + env vars
internal/middleware/       auth (JWT), CORS, logging, recovery
internal/library/          store (jsondb), wireguard, linuxserver (SSH), network, emailer, telegram, jwt, hash
internal/model/            data structs
internal/modules/          business logic per domain (auth, users, wireguard, network, system)
```

## Adding a New Module

1. Create a folder at `internal/modules/{name}/`.
2. Create `{controller}_controller.go` with an `XController` struct + methods
   shaped as `func (c *XController) MethodName(w http.ResponseWriter, r *http.Request, params []string)`.
3. Create `module.go` with `Register(registry *core.Registry, cfg *config.App, db store.IStore)`
   that registers the controller via `registry.RegisterController` and each
   endpoint via `registry.RegisterMethod("module", "controller", "method", handler)`.
4. Call `{name}.Register(app.Registry, cfg, db)` from `cmd/api/main.go`.

Use `middleware.RequireAuth(cfg)` for endpoints that need a login, and wrap
it again with `middleware.RequireAdmin` for admin-only endpoints.

## Response Helpers

```go
core.Success(w, "message", data)         // {status:true, code:200, data:{...}}
core.SuccessList(w, "message", datas)    // {status:true, code:200, datas:[...]}
core.BadRequest(w, "message")            // 400
core.Unauthorized(w, "message")          // 401
core.Forbidden(w, "message")             // 403
core.NotFound(w, "message")              // 404
core.InternalError(w, "message", err)    // 500, err is logged server-side
```

## Linux Server integration

`internal/library/linuxserver` talks to a remote Linux server over SSH (key
auth only, every command prefixed with `sudo`).
`internal/modules/wireguard/interfaces_controller.go` (`applyOne`) always
writes the local `wg0.conf`, and additionally calls `linuxserver.ApplySync`
when `LinuxServerSettings.Enabled` is true; `status_controller.go` (`Get`)
likewise switches between the local `wgctrl` device and
`linuxserver.GetPeerStatuses` based on that same flag. Settings are managed
via `internal/modules/wireguard/linuxserver_settings_controller.go`
(`GET/POST /wireguard/linuxserver/{get,update,test}`). See the root
[README.md](../README.md#linux-server-integration) for the operator-facing
setup steps (creating a restricted Linux user, granting it sudo, generating
the SSH key).

Unlike a RouterOS device (see this project's sibling mikrotik-wireguard
project, which this one was forked from), a wg-quick interface has no
incremental "add one peer" command or per-peer comment field to tag entries
with, so `ApplySync` doesn't diff individual peers -- it renders and pushes
the *complete* desired config for the interface (the same content written
locally, see `library.RenderWireGuardServerConfig`). If the device is
already up (checked via `ip link show`, not the systemd unit's own state --
see the package doc comment for why), it's reloaded live with
`wg syncconf` instead of a full `systemctl restart`: a restart's stop phase
runs PreDown first, and a PreDown failure (seen in production with a
Docker-chain iptables rule) leaves the device up but the unit "failed",
after which every subsequent restart's start phase fails with
`` `wg0' already exists `` forever, never actually applying the new config.
