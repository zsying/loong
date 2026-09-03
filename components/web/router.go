package web

import "net/http"

// Middleware wraps a handler, net/http style. The type stays stdlib
// compatible so the whole net/http middleware ecosystem plugs in.
type Middleware = func(http.Handler) http.Handler

// Route is one registered endpoint, as seen by Routes: method is empty
// for catch-all registrations (any method, e.g. static "/").
type Route struct {
	Method string
	Path   string
}

// Router is the registration surface a web channel hands to mounted
// components. It wraps the stdlib ServeMux behind friendly verbs and
// groups, so business code does not touch ServeMux patterns:
//
//	r := ctx.Get[*web.Router]()
//	r.Get("/api/books", h)
//	r.Group("/admin", guard).Get("/users", h)
//
// A root Router (NewRouter, or the one a web component builds) applies
// its Use middlewares server-wide when served; groups apply their
// middlewares to the handlers registered through them. Go 1.22 path
// values are available via r.PathValue("id").
type Router struct {
	mux    *http.ServeMux
	prefix string
	mws    []Middleware // server-wide (root) or registration wrap (groups)
	root   bool
	routes *[]Route // registration log, shared by root and its groups
}

// NewRouter creates an empty root Router. The web component builds one
// in its own Build; standalone creation supports tests and embedding.
func NewRouter() *Router {
	routes := &[]Route{}
	return &Router{mux: http.NewServeMux(), root: true, routes: routes}
}

// Handle registers h for the given method and path (e.g. "GET",
// "/api/x"). An empty method registers the path for every method —
// used by catch-all mounts such as static "/". Patterns are full
// paths; group prefixes are applied automatically.
func (r *Router) Handle(method, path string, h http.Handler) {
	r.register(method, path, h)
}

// HandleFunc is Handle for http.HandlerFunc values.
func (r *Router) HandleFunc(method, path string, h http.HandlerFunc) {
	r.register(method, path, h)
}

// Get, Post, Put, Patch and Delete are Handle with the method filled in.
func (r *Router) Get(path string, h http.HandlerFunc)    { r.register(http.MethodGet, path, h) }
func (r *Router) Post(path string, h http.HandlerFunc)   { r.register(http.MethodPost, path, h) }
func (r *Router) Put(path string, h http.HandlerFunc)    { r.register(http.MethodPut, path, h) }
func (r *Router) Patch(path string, h http.HandlerFunc)  { r.register(http.MethodPatch, path, h) }
func (r *Router) Delete(path string, h http.HandlerFunc) { r.register(http.MethodDelete, path, h) }

// Use appends server-wide middlewares on the root router (applied when
// the router is served), or group middlewares on a group (applied to
// every handler registered through the group afterwards).
func (r *Router) Use(mws ...Middleware) { r.mws = append(r.mws, mws...) }

// Group returns a sub-router scoped to a path prefix with its own
// middlewares: every handler registered on the group is mounted under
// prefix and wrapped by the group middlewares. Groups compose and share
// the root mux. Registration-level middlewares are inherited from a
// parent group only; server-wide middlewares on the root (Use) stay on
// the root and are applied by ServeHTTP for every request — they are
// never copied into groups, or group routes would run them twice.
func (r *Router) Group(prefix string, mws ...Middleware) *Router {
	g := &Router{mux: r.mux, prefix: r.prefix + prefix, routes: r.routes}
	if !r.root {
		g.mws = append(g.mws, r.mws...)
	}
	g.mws = append(g.mws, mws...)
	return g
}

// ServeHTTP implements http.Handler on the root router: it applies the
// server-wide middlewares around the mux. Group routers are not served
// directly.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h := http.Handler(r.mux)
	for i := len(r.mws) - 1; i >= 0; i-- {
		h = r.mws[i](h)
	}
	h.ServeHTTP(w, req)
}

// Routes returns the registered endpoints in registration order
// (groups already expanded into full paths). The web channel prints
// them at startup so an app's effective surface is visible at a glance.
func (r *Router) Routes() []Route {
	return append([]Route(nil), (*r.routes)...)
}

// register mounts one handler on the shared mux. Group middlewares are
// applied at registration time so each handler carries exactly the
// guards of the groups it was registered through; root middlewares are
// applied by ServeHTTP instead.
func (r *Router) register(method, path string, h http.Handler) {
	if !r.root {
		for i := len(r.mws) - 1; i >= 0; i-- {
			h = r.mws[i](h)
		}
	}
	pattern := r.prefix + path
	if method != "" {
		pattern = method + " " + pattern
	}
	*r.routes = append(*r.routes, Route{Method: method, Path: r.prefix + path})
	r.mux.Handle(pattern, h)
}
