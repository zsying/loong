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
- **Lazy activation** — assembly registers the tree without instantiating anything; only required components are built and run at startup, while service components (unless `Eager()`) and nodes marked `lazy: true` are activated on first use.
- **Component catalog** — `loong.Components()` lists every registered type with its description, services, activation mode and emitted events, so consumers can discover and wire components without reading source.
- **Single-process monolith** — the whole tree runs in one process; simple to debug, zero network overhead between components.
- **Zero-framework web channel** — stdlib `net/http` (Go 1.22 routing patterns) + JWT.

## Repository layout

```
loong/
├── *.go            # kernel (package loong, the platform itself)
├── components/     # platform components: log, user, web
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

# business component mounted under the web channel (emits an event upward)
curl localhost:8080/api/greet
```

Press `Ctrl+C` to shut down gracefully (components are stopped in reverse Run order).

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
        r.Handle("GET", "/api/greet", func(w http.ResponseWriter, _ *http.Request) {
            _ = g.Emit("biz.greet.hello", "hi") // event flows upward to the parent
            w.Write([]byte("greetings\n"))
        })
    }
    return nil
}

func init() {
    loong.RegisterComponent("biz.greet", func() loong.Component { return &greet{} },
        loong.WithConfig[greetConfig](), // declare your config struct (shown by Components())
        loong.WithEvents("biz.greet.hello"),
        loong.WithDesc("sample business component"),
    )
}
```

Then declare the component in your config tree (`loong.yaml`):

```yaml
type: app
config:
  name: myproject
children:
  - type: web
    id: main
    config:
      listen: ":8080"
    children:
      - type: biz.greet
        config:
          route: /api/greet
```

## Documentation

- [Design document](docs/design.md) — architecture, component tree, config schema, event protocol, lifecycle and roadmap.

## Status

v0.1 — kernel, log/user/web components and a working sample. Wechat channels, TUI and extension components (mail, unionid bridging, ...) are on the roadmap.
