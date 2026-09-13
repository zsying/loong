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
- **config tree**: `desc` node field — a per-node one-line description shown
  by CLI usage listings (a generic cli.group node can say "Manage the
  background daemon"); falls back to the component type's WithDesc.
- **cli master**: usage listing is a two-column table — bold command names
  (with inline subcommands for groups) and gray descriptions, color enabled
  only on an interactive stdout.
- **events**: `Scope.MustEmit` / `ErrNoSubscriber` — the strict Emit for
  hook-style events: a missing subscriber is a loud error instead of a silent
  drop. Plain `Emit` keeps the lenient notification semantics.
- **kernel**: `Providers[T]()` — the ids of every node that can serve T, in tree
  declaration order. The discovery half of `GetFrom[T](id)`: with two providers
  of one type mounted, this is how a caller learns what there is to name. Purely
  structural — lazy nodes are listed before activation, and asking activates
  nothing.
- **kernel**: `NodeInfo` (from `Root()` / `Node(id)`) now carries the node's own
  `Desc` and its `ParentID`, so a tree view renders descriptions and paths from
  the public view instead of reaching into the internal `*Node`.
- **kernel**: `WithContributes[Kind]()` / `Scope.Provide[Kind](name)` /
  `Kernel.Contributions[Kind]()` — tree-declared capabilities. A type whose
  nodes contribute a named item declares it once and the name is the node's own
  id, so what a subtree offers is read from the skeleton: no activation, and no
  dependency on the order the tree happens to be built in (a consumer no longer
  has to resolve capabilities through whatever has already registered them).
  `Provide` adds names that cannot exist before the component runs — a remote
  server's tool list, say. One name has one owner: re-affirming a name is a
  no-op, and a second node claiming it fails where it claims.
- **kernel**: `WithOptionalService[T](get)` — a service that may not exist.
  A nil accessor result means this node serves nothing of type T, and every
  lookup skips it as if it had declared nothing, so "no backend configured, no
  such capability" needs no sentinel value for consumers to unpack. A required
  service still fails the activation on nil, which is what tells a component it
  returned too early.
- **kernel**: `Kernel.Consumers[T]()` — the ids of the nodes that actually
  resolved T, in tree order. `Providers[T]()` says who can serve a service;
  this says who used it. It is a trace, not a declaration: a node that never
  ran and a lookup that found nothing both record nothing.
- **kernel**: `NodeInfo` now also carries `Services` (the service types the
  node's component type declares) and `Contributes` (the names the node holds),
  so a `tree` view renders what a node offers from the same walk that renders
  the tree, without joining `Components()` by type afterwards.
- **web**: the listen address may arrive as the activation argument — the shape
  a node needs when its address only exists after the application has read its
  own configuration (the tree cannot spell it, the parent holding it can).
  `Scope.Activate(id, "127.0.0.1:8080")` on a web node whose config leaves
  `listen` empty binds there. Naming the address twice — config and argument —
  is reported rather than resolved by precedence, and an unsupported argument
  type is an error instead of a silently unused one.

### Changed

- **Assembly is the root's activation.** `Assemble` had its own copy of the
  build/run walk; it now indexes the tree and activates the root, which is the
  same two-phase walk a lazy subtree gets (the whole active tree Build, parent
  before child, before any Run). One build path means one set of rules — a
  component is instantiated in exactly one place — and the activation
  bookkeeping now covers assembly too: a node the tree brought up counts as
  active, so a later on-demand activation of it is a no-op instead of a second
  instance (which used to happen silently for components that provide no
  service).
- **A Build that needs a service from a node still building is reported.** Since
  activating a node activates its subtree, a service lookup made during a Build
  can start a subtree whose own Build needs the node that asked for it — a
  build-time cycle (owlet's shape: the coordinator's Build needs the tool
  registry, and the registry's dispatch child needs the coordinator). Neither
  Build can finish first, so the node that was already being built is now named
  rather than built again, and the error says what resolves it: declare the
  provider above the consumer in the tree.
- **`lazy` marks a subtree, not a node.** `lazy: true` turns off everything
  mounted under the node, and activating the node brings that subtree up in the
  two phases assembly uses: every Build, parent before child, before any Run.
  The marker used to be read per node, so a non-lazy child of a lazy node was
  built at startup — a tree could report a subsystem as off while part of it was
  already running, and the child's Build ran without the parent its wiring
  assumes. A descendant marked lazy remains a decision of its own and is
  activated separately; activation is still idempotent per node (the anchor
  caches the outcome, descendants are marked active with it), a subtree that
  fails to come up stops what it did build, in reverse, and a failed build now
  names the node inside the subtree that failed. Activation arguments still
  belong to the node they were passed to: the subtree inherits the activation,
  not the argument. Views that describe the tree rather than run it — `Shutdown`,
  `Contributions[T]`, `NodeInfo` — still walk dormant subtrees, which is what
  lets them report what is there before anything activated it.
- **The cli usage listing spells out subcommands only for a group.** The listing
  appended ` <child, child>` to every node that had children, which reads as
  subcommands that can be invoked. A `cli.group` is the one command shape that
  dispatches to a child by name, so it is the one shape whose children are
  commands; a command that mounts components under itself — a dashboard mounting
  the HTTP channel it starts — is now a single line instead of advertising names
  the argv cannot reach.

### Fixed

- The "node already provides a service" branch of service registration unlocked
  the kernel mutex by hand and again through its `defer`, so reaching it panicked
  with `fatal error: sync: unlock of unlocked mutex` instead of returning the
  error — which is what happened the first time a build-time cycle got there.
  The branch is only reachable through such a bug, and it now reports one.
- Inactive-provider lookup chose the component *type* to activate from a
  map-ordered index built at `New()`. When one service type was declared by
  several component types, that index kept an arbitrary one of them, so the same
  tree could answer differently between processes: the lookup missed a provider
  that existed (`no node of service type "..."`), or silently returned a
  provider that was not the caller's nearest ancestor. Candidates are now the
  mounted nodes themselves — tree order, nearest ancestor wins — and the two
  type-keyed indexes (`serviceIdx`, `nodesByType`) are gone. The empty-lookup
  error also separates "nothing declares this service" from "the declaring
  types have no node mounted", naming the types in the second case.

### Removed

- The registration-time warning about two component types declaring the same
  service type. It is a supported arrangement — resolution is per node — so the
  warning described a limitation that no longer exists.

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
