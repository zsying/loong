# loong

A general-purpose software platform built on a **component tree**. Bring loong into your project, mount your own business components, and get user auth, config, logging, web and wechat channels out of the box — so you stop rebuilding the same plumbing for every new project and focus on your business.

## Why loong

Every new software project starts by re-implementing the same parts: login, config, logging, users, message push. loong turns that repeated scaffolding into a reusable platform:

- Import the platform, write only your **business components**.
- Platform capabilities are pluggable components, not hard-coded features.

## Features

- **Component tree architecture** — the whole system is a tree of component instances; the tree expresses composition, config scope and communication paths.
- **Three-phase lifecycle** — `Build` → `Run` → `Stop`, with graceful shutdown in reverse Run order.
- **Base skeleton component** — embed `loong.Base` and override only the phases you care about; the core interface stays minimal.
- **One-call bootstrap** — `loong.LoadAndRun(path, loong.WithWait())` loads the config tree, assembles and runs the whole tree, then optionally blocks for a SIGINT/SIGTERM shutdown signal.
- **Schema-per-component config tree** — the kernel reads only the skeleton (`type` / `id` / `config` / `children`); every component decodes its own opaque `config` block from a YAML file, so config structure grows freely with the components you mount.
- **Two-way parent-child communication** — config injected downward at build time, events emitted upward at runtime, type-based service lookup as a side channel.
- **Tree-scoped service lookup** — `ctx.Get[T]()` resolves the sole provider, or the closest one on the node's parent chain when several exist (`GetFrom[T](id)` disambiguates); services are declared at registration via `loong.WithService[T](get)` (multiple per component), never through global state.
- **Lazy activation** — activation is one rule: nodes are built at startup unless marked `lazy: true` in the config tree; lazy nodes (and service lookups that hit an inactive provider) are activated on first use.
- **Component catalog** — `loong.Components()` lists every registered type with its description, services, activation mode and emitted events, so consumers can discover and wire components without reading source.
- **Single-process monolith** — the whole tree runs in one process; simple to debug, zero network overhead between components.
- **Zero-framework web channel** — stdlib `net/http` (Go 1.22 routing patterns) behind a friendly `*web.Router` registration surface; sessions (`auth`), account API (`web.account`) and static hosting (`web.static`) are separate optional components. Middleware is plain `func(http.Handler) http.Handler`: `Recover`, request logging and CORS ship with the channel, and the wrappers stay transparent so streaming (SSE) and protocol upgrades (WebSocket) still work. HTTPS is opt-in via a `tls: {cert, key}` block; route conflicts fail assembly instead of panicking.

## Repository layout

```
loong/
├── *.go            # kernel (package loong, the platform itself)
├── components/     # platform components: log, user, auth, web, cli
├── examples/       # sample projects
└── docs/           # design documents
```

## Requirements

- Go 1.27+ (generic methods; net/http method+path routing patterns)

## Quick start

```bash
# run the hello sample (Web channel)
LOONG_JWT_SECRET=dev-secret go run ./examples/hello ./examples/hello/loong.yaml
```

Then exercise the endpoints:

```bash
# register a user
curl -X POST localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"john","password":"secret123","nickname":"John"}'

# login and get a JWT
curl -X POST localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"john","password":"secret123"}'

# protected endpoint (requires "Authorization: Bearer <token>")
curl localhost:8080/api/me -H "Authorization: Bearer <token>"

# business component mounted under the web channel
curl localhost:8080/api/greet
```

Press `Ctrl+C` to shut down gracefully (components are stopped in reverse Run order).

## CLI

CLI applications are built on the platform itself, using the **parent-defined activation** primitive (`scope.Activate(child, args)` — a parent activates one of its direct children and arguments flow through `scope.Args`). The `components/cli` package provides the core: a **cli master** (parses the command line and activates the matching child command), a generic `cli.group` container for multi-level commands, and argument helpers. The optional `components/cli/commands` package adds platform commands (`cli.list` / `cli.new` / `cli.tree`) — import it only if you want them. A CLI is a full loong app, so it also mounts the `log` component for logging. `examples/cli` is the working template (master + log + custom business commands + multi-level groups).

```bash
go run ./examples/cli list                # component catalog of *this* app (platform + business)
go run ./examples/cli list --html
go run ./examples/cli new greet           # scaffold a component source file
go run ./examples/cli tree loong.yaml     # project config tree annotated with services
go run ./examples/cli greet john --loud   # a custom business command (logged)
go run ./examples/cli config show theme   # multi-level command: group "config" + "show"
```

```go
import (
    "github.com/zsying/loong/components/cli"            // master + cli.group
    _ "github.com/zsying/loong/components/cli/commands" // optional: list/new/tree
    _ "github.com/zsying/loong/components/log"          // logging, like any loong app
)
```

A CLI is just a loong app — mount the components in a config tree (embedded via `go:embed`, or as a file with `loong.LoadAndRun`), and main only starts the app and maps errors to exit codes. Both entry styles are shown in `examples/cli`:

```go
//go:embed cli.yaml
var cliYAML []byte

root, err := loong.Parse(cliYAML) // or loong.LoadAndRun("cli.yaml") for a file
if err != nil { os.Exit(1) }
k := loong.New()
if err := k.Assemble(root); err != nil { // the master runs the command here
    _ = k.Shutdown()
    os.Exit(2) // if errors.Is(err, cli.ErrUsage) — see examples/cli
}
_ = k.Shutdown()
```

Adding a command means registering a new component type and a node in `cli.yaml` — no dispatch code anywhere.

## Writing a component

```go
package main

import "github.com/zsying/loong"

type greet struct {
    loong.Base // no-op phases for free; override only what you need
}

func (g *greet) Build(ctx *loong.Scope) error {
    // 1. decode your own config block (strict: unknown keys fail)
    cfg, err := ctx.Config[greetConfig]()
    if err != nil {
        return err
    }
    // 2. wire dependencies / register routes through parent services
    if r := ctx.Get[*web.Router](); r != nil {
        r.Get("/api/greet", func(w http.ResponseWriter, req *http.Request) {
            web.WriteJSON(w, http.StatusOK, map[string]string{"msg": "greetings"})
        })
    }
    return nil
}

func init() {
    loong.RegisterComponent("biz.greet", func() loong.Component { return &greet{} },
        loong.WithConfig[greetConfig](), // declare your config struct (shown by Components())
        loong.WithDesc("sample business component"),
    )
}
```

Mount the business component under a web channel and add the platform
capabilities you want as components in your config tree (`loong.yaml`).
The root can be the kernel-provided `base` container — a no-op root
that needs no custom component declaration:

```yaml
type: base
config:
  name: myproject
children:
  - type: user
    id: users
  - type: auth                # session tokens (JWT): issue/verify + Guard
    id: sessions
    config:
      secret: ${JWT_SECRET}
  - type: web
    id: main
    config:
      listen: ":8080"
    children:
      - type: web.account     # optional: register / login / me over user + auth
        id: account
      - type: web.static      # optional: static hosting + SPA fallback
        id: site
        config:
          dir: ./dist
          spa: true
      - type: biz.greet       # the component above registers its own route
        config:
          route: /api/greet
```

## Documentation

- [Design document](docs/design.md) — architecture, component tree, config schema, event protocol, lifecycle and roadmap.
- [Changelog](CHANGELOG.md) — what changed in each release.

## Status

v0.1.0 — the component-tree kernel with log, user, auth, web and cli components, plus two working samples. Wechat channels, TUI and extension components (mail, unionid bridging, ...) are on the roadmap. Requires Go 1.27.
