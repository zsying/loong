# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **cli master**: `default` config — bare invocation activates the named child
  command instead of printing usage (TUI-first apps like owlet no longer need
  a custom master).
- **cli master**: `flags` config — global flag schema (`short` alias, `bool` /
  `value` kind) peeled anywhere in the argv, POSIX-style; undeclared tokens
  pass through untouched. A dangling value flag is a loud error.
- **cli master**: `pre` config — a lazy child activated once, before the first
  command, with `*cli.Globals` (peeled flags + rest). The componentized
  PersistentPreRunE: runs exactly once per process (activation cache), never
  on the usage path, is not selectable as a command, and its failure aborts
  the whole activation chain. Commands keep receiving `[]string`; `cli.group`
  unpacks either form.
- **cli master**: built-in help — `-h` / `--help` anywhere in the argv or a
  `help` command token print the usage tree and exit 0 (no pre run, no
  activation). Auto-yield: spellings claimed by the app's flag schema or a
  child command named "help" pass through untouched.
- **events**: `Scope.MustEmit` / `ErrNoSubscriber` — the strict Emit for
  hook-style events: a missing subscriber is a loud error instead of a silent
  drop. Plain `Emit` keeps the lenient notification semantics.

### Docs

- Event routing contract made explicit: routing is deliberately single-hop
  (child -> direct parent), no bubbling; relay upward by emitting a new event.

## [0.2.0] - 2026-09-09

Hardening release, driven by the owlet migration review.

### Changed

- **Breaking**: `RegisterComponent` now panics on a duplicate component type
  name, mirroring `database/sql.Register` and `flag` — a silent overwrite made
  the config tree resolve to whichever `init()` happened to run last.
- Service registration on a node whose id already provides the same service
  type now fails the activation instead of warn-and-overwrite. (Defensive:
  activation bookkeeping makes it unreachable today.)

### Docs

- Component authoring guide: added the `Scope` vs `context.Context` boundary
  rule — tree collaboration (services, config, events) goes through `Scope`;
  I/O cancellation and timeouts go through `context.Context`, with the cancel
  owned by the component instance and invoked in `Stop`.

## [0.1.0] - 2026-09-07

First published release. loong is a general-purpose platform: bring it into your
project, mount your own components on the tree, and get config, logging, users,
sessions and an HTTP channel instead of rebuilding the plumbing.

Requires **Go 1.27** (the kernel uses generic methods).

### Added

**Kernel** — `package loong`, the platform itself

- Component tree with a three-phase `Build` / `Run` / `Stop` lifecycle; a type is
  registered once with `loong.RegisterComponent(type, factory)`.
- Config tree with schema-per-component decoding: the kernel reads only the
  skeleton (`type` / `id` / `config` / `children`), each component decodes its own
  block with `scope.Config[T]()` — strict, so unknown fields are errors — with
  `${ENV}` expansion.
- Services declared at registration via `WithService[T](get)`; tree-scoped lookup
  with `scope.Get[T]()` (the sole provider, or the closest one on the parent
  chain) and `GetFrom[T](id)` to disambiguate. Several components may provide the
  same service type.
- Lazy activation: `lazy: true` in the config tree skips a node at startup, which
  then activates on first lookup or on `Activate(id)`. Activation is one rule —
  everything else is built at startup, whether or not it provides a service.
- Parent-defined child activation: `Scope.Activate(id, args)` plus `Scope.Args`,
  so a parent decides how its children are activated and what they receive. The
  whole CLI is built on this primitive.
- Events `{Name, Source, Payload}` delivered to the direct parent node, with
  `Scope.On` and the typed helper `Scope.OnTyped[T]`.
- `base`, a built-in container to use as the config-tree root.
- Bootstrap: `LoadAndRun(path, WithWait())` for the common case, with `LoadTree` /
  `New` / `Assemble` still public for embedding and tests.
- Component catalog: `loong.Components()` lists every registered type with its
  description, services, config fields and events.

**Web channel** — `components/web`

- `web` is a pure HTTP channel: `listen`, graceful shutdown, server timeouts and a
  `*web.Router` registration surface over the stdlib `ServeMux` (Go 1.22 method +
  path patterns). It knows nothing about sessions, accounts or static files.
- Router: `Handle` / `Get` / `Post` / `Put` / `Patch` / `Delete`,
  `Group(prefix, mws...)` for a prefix plus group middleware, `Use` for
  server-wide middleware, `Routes()` for the listing printed at startup.
  Middleware is plain `func(http.Handler) http.Handler`, so the whole net/http
  ecosystem plugs in.
- Middleware shipped with the channel: `Recover` (panic → 500), request logging
  (debug; `log: false` turns it off) and `CORS(cfg)`. CORS is opt-in — which
  origins may call an API is a business decision, not a platform default.
- Optional HTTPS: a `tls: {cert, key}` block serves TLS, an absent one serves
  plain HTTP, so a deployment behind a reverse proxy never touches certificates.
- `auth` — session tokens (JWT) as a cross-channel component: `Issue(sub)`,
  `Guard(next)`, `Identity(r)`. It depends only on `net/http`, so the same service
  can guard another channel.
- `web.account` (optional) — the standard `register` / `login` / `me` API over
  `user` + `auth`. Skip it when accounts work differently.
- `web.static` (optional) — static hosting with `dir`, `prefix`, an SPA fallback,
  and an `api` prefix that keeps unmatched API paths 404.

**CLI** — `components/cli`

- `cli` is a component like any other: the entry point is a plain loong app, with
  no wrapper around it. Multi-level commands are tree levels via the `cli.group`
  container.
- Optional command set in `components/cli/commands` (`list`, `new`, `tree`) —
  import it only when you want those commands.
- Argument helpers `HasFlag` / `Flag` / `Positional`, with `-x` and `--word`
  spellings bound to the dash-count convention.

**Log** — `components/log`

- `slog`-based logging with `console` (colored, auto-disabled when the output is
  not a TTY) and `json` formats, plus a configurable level.

**User** — `components/user`

- SQLite-backed users with bcrypt password hashes and openid-based creation.

**Samples and docs**

- `examples/hello`: log + user + auth + web + account + a business component
  mounted under the channel, started with `LoadAndRun`.
- `examples/cli`: a CLI app assembled entirely from components.
- `docs/design.md`: the reasoning behind the tree, the lifecycle, services and
  the channels.

### Fixed

- Duplicate route registrations and paths not rooted at `/` are reported through
  `Router.Err()` and fail assembly, instead of panicking inside the stdlib mux.
- A busy port — or an unreadable TLS certificate — fails assembly instead of
  leaving a process that listens but serves nothing.
- `Recover` and the request log stay transparent wrappers: `Flush` and `Hijack`
  are forwarded to the real writer, so SSE, chunked downloads and WebSocket
  upgrades keep working.
- Group routes no longer run server-wide middleware twice.
- `web.static`: the mount prefix is stripped before the file lookup, so
  `prefix` + `spa` serves real files instead of 404; the SPA fallback no longer
  masks unmatched `/api` paths.
- CLI: usage errors are printed instead of exiting silently; a bare invocation
  shows help and exits 0; argument parsing is nil-safe.
- Tokens: non-HMAC signing methods are rejected (algorithm confusion), a
  duplicate registration answers 409, and internal errors are not returned to
  clients.

### Notes

- v0.1 is a foundation release: the API may still change between minor versions
  until 1.0.
- The wechat/miniprogram channel, TUI and extension components (mail, unionid
  bridging) are not part of this release.
