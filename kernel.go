package loong

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Kernel assembles and runs the component tree in a single process.
type Kernel struct {
	factories map[string]regEntry
	services  map[reflect.Type]map[string]any // service type -> node id -> value
	idIndex   map[string]*Node                // node id -> node
	root      *Node

	// mu guards the runtime tables below and the service table.
	// Activation happens outside it.
	mu sync.Mutex

	// provided holds the contributions registered at runtime
	// (Scope.Provide), per kind, in registration order. Structural
	// contributions (WithContributes) need no storage: they are the
	// node's own declaration.
	provided map[reflect.Type][]contribution
	// skipped holds the nodes whose optional service produced nothing, so
	// a declaration that serves nothing is not offered as a provider.
	skipped map[reflect.Type]map[string]bool
	// consumers traces, per service type, the nodes that resolved it.
	consumers map[reflect.Type]map[string]bool
}

// contribution is one contributed name and the node it came from.
type contribution struct {
	Name string
	Node string
}

// New returns a kernel seeded with all init()-registered factories.
func New() *Kernel {
	f := make(map[string]regEntry, len(factories))
	for name, e := range factories {
		f[name] = e
	}
	return &Kernel{
		factories: f,
		services:  make(map[reflect.Type]map[string]any),
		idIndex:   make(map[string]*Node),
		provided:  make(map[reflect.Type][]contribution),
		skipped:   make(map[reflect.Type]map[string]bool),
		consumers: make(map[reflect.Type]map[string]bool),
	}
}

// Assemble registers the tree and activates everything the tree declared
// on: indexing first (ids, parents, types — no instantiation), then one
// activation of the root. That second step is deliberately not a special
// path: the root is activated exactly the way a lazy subtree is, so
// assembly is one activation and `lazy: true` is exactly the part of the
// tree it leaves out. If Build or Run fails, the nodes it built are
// stopped in reverse order so acquired resources (db connections,
// servers) are released — see ensureActive.
func (k *Kernel) Assemble(root *Node) error {
	k.root = root
	if err := k.indexNode(root, nil); err != nil {
		return err
	}
	if err := k.checkUniqueIDs(root); err != nil {
		return err
	}
	return k.ensureActive(root, nil)
}

// indexNode walks the tree, filling default ids, setting parent
// pointers, validating component types and building the by-id index.
// Nothing is instantiated here — this phase is cheap and always runs
// fully.
func (k *Kernel) indexNode(n *Node, parent *Node) error {
	n.parent = parent
	if n.ID == "" {
		n.ID = n.Type
	}
	if _, ok := k.factories[n.Type]; !ok {
		return fmt.Errorf("loong: unknown component type %q (node %q)", n.Type, n.ID)
	}
	k.idIndex[n.ID] = n
	for _, c := range n.Children {
		if err := k.indexNode(c, n); err != nil {
			return err
		}
	}
	return nil
}

// buildNode instantiates one node, runs its Build phase and registers the
// services declared via WithService. Run is deliberately kept separate so
// activation can wire a whole subtree before any of it starts serving.
// args is the activation argument, handed to the component through
// Scope.Args; it is non-nil only for the node an activation was asked
// for, never for the descendants that came up with it.
//
// This is the only place a component is instantiated, so it is also the
// only place that has to refuse a second Build of a node whose Build is
// still on the stack — see beginBuild.
func (k *Kernel) buildNode(n *Node, args any) error {
	if err := n.beginBuild(); err != nil {
		return err
	}
	defer func() { n.building = false }()
	n.component = k.factories[n.Type].factory()
	if err := n.component.Build(&Scope{Kernel: k, Node: n, Raw: n.Config, Args: args}); err != nil {
		return err
	}
	return k.registerService(n)
}

// beginBuild claims a node for one Build. A node already being built is
// refused instead of built twice, because the second Build would be
// running while the first is still on the stack: some component's Build
// asked for a service from a node whose own Build is waiting for that
// component to be built. No activation order can satisfy that, and the
// symptom of getting it wrong is a component silently instantiated twice
// (or, when it provides a service, a duplicate provider) — so it is
// reported as what it is.
//
// The cycle is the tree's to break, and declaration order is how: a node
// is built before its siblings, so the provider of a service that a
// sibling's subtree needs has to be declared above that subtree (owlet
// mounts its tools above the coordinator that dispatches through them).
func (n *Node) beginBuild() error {
	if n.building {
		return fmt.Errorf("loong: node %q is already being built: a Build needs the service it provides, so this node's Build cannot have finished first (a build-time cycle — declare the provider above the consumer in the tree)", n.ID)
	}
	n.building = true
	return nil
}

// registerService evaluates every WithService accessor declared by the
// node's component type and stores the values in the service table,
// keyed by type and node id, so multiple providers of the same type
// (e.g. two web instances) coexist. A nil value fails the activation —
// the accessor returned before the component was ready — unless the
// declaration was made with WithOptionalService, in which case nil is
// an answer: the node serves nothing of that type, and is recorded as
// such so no lookup offers it afterwards.
func (k *Kernel) registerService(n *Node) error {
	decls := k.factories[n.Type].services
	if len(decls) == 0 {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, sd := range decls {
		v := sd.get(n.component)
		if isNilValue(v) {
			if sd.optional {
				m := k.skipped[sd.typ]
				if m == nil {
					m = make(map[string]bool)
					k.skipped[sd.typ] = m
				}
				m[n.ID] = true
				continue
			}
			return fmt.Errorf("loong: node %q provides nil service for %s", n.ID, sd.typ)
		}
		m := k.services[sd.typ]
		if m == nil {
			m = make(map[string]any)
			k.services[sd.typ] = m
		}
		if _, exists := m[n.ID]; exists {
			// Unreachable today (a node is built exactly once — see
			// beginBuild), but fail loudly if the activation bookkeeping
			// ever regresses: a silent overwrite here would corrupt the
			// service table.
			return fmt.Errorf("loong: node %q already provides a service for %s", n.ID, sd.typ)
		}
		m[n.ID] = v
	}
	return nil
}

// ensureActive activates a lazy node exactly once, running the full
// lifecycle (Build + Run) outside any kernel-wide lock so component
// code may itself call Get/GetFrom for other lazy services without
// deadlocking. The per-node mutex serializes concurrent activations of
// the same node and caches the outcome for later callers. args is
// exposed to the component through Scope.Args (nil for startup and
// service activation) — the value a parent passes via Scope.Activate.
//
// Activating the node activates the subtree it anchors: every non-lazy
// descendant is built too, parent before child, and only once the whole
// subtree is built does any of it run. That is the order a child wired to
// what its parent built needs — a router before the routes registered on
// it — and it is why the atom of activation is a subtree: the tree
// declared the whole thing off, so bringing up part of it would be a
// state the config never described. A descendant marked lazy is a
// decision of its own and stays off until it is activated in turn.
//
// args belong to the anchor alone: the subtree inherits the activation,
// not the argument.
func (k *Kernel) ensureActive(n *Node, args any) error {
	n.actMu.Lock()
	defer n.actMu.Unlock()
	if n.actDone {
		return n.actErr
	}

	var built []*Node
	if err := k.walkActive(n, func(m *Node) error {
		if m.actDone {
			return nil
		}
		var a any
		if m == n {
			a = args
		}
		if err := k.buildNode(m, a); err != nil {
			return fmt.Errorf("loong: build %q: %w", m.ID, err)
		}
		built = append(built, m)
		return nil
	}); err != nil {
		// The anchor caches the outcome; whatever the activation did build
		// is stopped in reverse, so a subtree that failed to come up does
		// not leave half of itself running.
		n.actDone, n.actErr = true, errors.Join(err, k.stopReverse(built))
		return n.actErr
	}

	if err := k.walkActive(n, func(m *Node) error {
		if m.actDone {
			return nil
		}
		sc := &Scope{Kernel: k, Node: m, Raw: m.Config}
		if m == n {
			sc.Args = args
		}
		if err := m.component.Run(sc); err != nil {
			return fmt.Errorf("loong: run %q: %w", m.ID, err)
		}
		return nil
	}); err != nil {
		n.actDone, n.actErr = true, errors.Join(err, k.stopReverse(built))
		return n.actErr
	}
	// Every node this activation brought up is active from here on, so a
	// second activation of the anchor (or of one of its descendants) is a
	// no-op rather than a second instance.
	for _, m := range built {
		m.actDone = true
	}
	return nil
}

// Activate manually activates a lazy node at runtime — useful for a
// component declared lazy in the config tree that is needed on demand
// but does not provide a service. It is idempotent: activating an
// already active node is a no-op returning its cached outcome.
func (k *Kernel) Activate(id string) error {
	k.mu.Lock()
	n := k.idIndex[id]
	k.mu.Unlock()
	if n == nil {
		return fmt.Errorf("loong: unknown node %q", id)
	}
	return k.ensureActive(n, nil)
}

// NodeInfo is the instance-level view of one mounted node, returned
// by Kernel.Node. It powers tooling such as the cli component's
// multi-level command dispatch and help rendering (children recurse).
//
// Desc is the node's own description, empty when the node declares
// none: the component type's WithDesc is reachable through
// Describe(info.Type) and stays the reader's fallback, so this view
// never reports a description the tree did not ask for. ParentID is the
// id of the node this one is mounted under, empty at the root — enough
// to walk upward, and deliberately not a parent pointer, which would
// turn the view into a graph.
//
// Services and Contributes are what the node offers: the service types
// its component type declares (WithService/WithOptionalService) and the
// names it holds among the tree's contributions (WithContributes, plus
// whatever it registered through Scope.Provide). Both are declarations
// read from the skeleton, not runtime state: a service the node turned
// out not to have is still listed, because the tree is what asked for
// it.
type NodeInfo struct {
	ID          string
	Type        string
	Desc        string
	Lazy        bool
	ParentID    string
	Services    []string    // declared service types, in declaration order
	Contributes []string    // contributed names (structural name is the node id)
	Children    []*NodeInfo // child nodes, in declaration order
}

// Node returns instance metadata for the node with the given id, or
// an error when the id is unknown. It never activates the node.
func (k *Kernel) Node(id string) (*NodeInfo, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := k.idIndex[id]
	if n == nil {
		return nil, fmt.Errorf("loong: unknown node %q", id)
	}
	return k.nodeInfoOf(n), nil
}

// Component returns the active component instance of the node with
// the given id, or an error when the id is unknown or the node has
// not been activated yet. Tooling (e.g. the cli master's dispatch)
// uses it to reach into a mounted component.
func (k *Kernel) Component(id string) (Component, error) {
	k.mu.Lock()
	n := k.idIndex[id]
	k.mu.Unlock()
	if n == nil {
		return nil, fmt.Errorf("loong: unknown node %q", id)
	}
	if n.component == nil {
		return nil, fmt.Errorf("loong: node %q not activated", id)
	}
	return n.component, nil
}

// Root returns instance metadata for the assembled tree's root node.
func (k *Kernel) Root() (*NodeInfo, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.root == nil {
		return nil, fmt.Errorf("loong: no tree assembled")
	}
	return k.nodeInfoOf(k.root), nil
}

// nodeInfoOf renders a node (and recursively its children) into
// public metadata. Callers hold k.mu (the view reads the runtime
// contribution table).
func (k *Kernel) nodeInfoOf(n *Node) *NodeInfo {
	info := &NodeInfo{ID: n.ID, Type: n.Type, Desc: n.Desc, Lazy: n.Lazy}
	if n.parent != nil {
		info.ParentID = n.parent.ID
	}
	for _, sd := range k.factories[n.Type].services {
		info.Services = append(info.Services, sd.typ.String())
	}
	info.Contributes = k.contributesOf(n)
	for _, c := range n.Children {
		info.Children = append(info.Children, k.nodeInfoOf(c))
	}
	return info
}

// providersOf returns every node whose component type declares a
// service of type t, in tree declaration order.
//
// It walks the tree instead of consulting a per-type index, because a
// service type may be declared by several component types and every
// declaring node is a candidate: an index keyed by type would have to
// drop all but one of them, and a lookup would then miss a provider
// that exists (or, worse, pick one that is nowhere near the caller).
//
// The walk reads only the tree skeleton and the registered factories,
// both fixed by the end of Assemble, and never activates anything.
// Callers needing a consistent view of the service table hold k.mu.
func (k *Kernel) providersOf(t reflect.Type) []*Node {
	if k.root == nil {
		return nil
	}
	var out []*Node
	_ = k.walk(k.root, func(n *Node) error {
		if k.nodeProvides(n, t) {
			out = append(out, n)
		}
		return nil
	})
	return out
}

// nearestProvider returns the closest node on the chain from the given
// node upward (itself included) whose component type declares service
// type t, or nil when the chain holds no such node.
func (k *Kernel) nearestProvider(from *Node, t reflect.Type) *Node {
	for n := from; n != nil; n = n.parent {
		if k.nodeProvides(n, t) {
			return n
		}
	}
	return nil
}

// nodeProvides reports whether the node offers service type t: its
// component type declares it via WithService, and the node did not
// already turn out to serve nothing (an optional declaration whose
// accessor returned nil). Callers hold k.mu.
func (k *Kernel) nodeProvides(n *Node, t reflect.Type) bool {
	if k.skipped[t][n.ID] {
		return false
	}
	for _, sd := range k.factories[n.Type].services {
		if sd.typ == t {
			return true
		}
	}
	return false
}

// Contributions returns the names of kind T the tree holds: the
// structural ones — every node whose component type declares the kind,
// named by its own id — in tree declaration order, then the ones
// registered at runtime through Scope.Provide, in registration order.
//
// It answers from the skeleton: a declaring node is listed before
// anything activated it, and asking activates nothing, which is what
// lets a consumer resolve what a subtree offers without depending on
// the order the tree is built in. An empty result means the tree holds
// no names of that kind; that is an answer, not an error.
func (k *Kernel) Contributions[T any]() []string {
	t := reflect.TypeOf((*T)(nil)).Elem()
	k.mu.Lock()
	defer k.mu.Unlock()
	entries := k.contributionsOf(t)
	names := make([]string, 0, len(entries))
	for _, c := range entries {
		names = append(names, c.Name)
	}
	return names
}

// contributionsOf returns every name of kind t this tree holds, with
// the node it came from: the structural contributions in tree order,
// then the runtime ones in registration order. Callers hold k.mu.
func (k *Kernel) contributionsOf(t reflect.Type) []contribution {
	var out []contribution
	if k.root != nil {
		_ = k.walk(k.root, func(n *Node) error {
			if k.nodeContributes(n, t) {
				out = append(out, contribution{Name: n.ID, Node: n.ID})
			}
			return nil
		})
	}
	return append(out, k.provided[t]...)
}

// nodeContributes reports whether the node's component type declares
// contribution kind t via WithContributes.
func (k *Kernel) nodeContributes(n *Node, t reflect.Type) bool {
	for _, kind := range k.factories[n.Type].contributes {
		if kind == t {
			return true
		}
	}
	return false
}

// contributesOf lists the names one node holds: its own id when its
// type declares any contribution kind, then the names it registered at
// runtime, sorted. The kinds are not part of the view — a reader that
// needs them asks Contributions[T]. Callers hold k.mu.
func (k *Kernel) contributesOf(n *Node) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(k.factories[n.Type].contributes) > 0 {
		add(n.ID)
	}
	var runtime []string
	for _, list := range k.provided {
		for _, c := range list {
			if c.Node == n.ID {
				runtime = append(runtime, c.Name)
			}
		}
	}
	// One node may hold names in several kinds; ordering across kinds
	// must not be map order.
	sort.Strings(runtime)
	for _, name := range runtime {
		add(name)
	}
	return out
}

// provide records a runtime contribution of kind t for node n.
//
// One name has one owner: a node re-affirming a name it already holds
// is a no-op, while a second node claiming the same name is an error
// where it happens — an ambiguous contribution is the same mistake as a
// duplicate node id, and the caller is the only one who can tell the
// two apart (it chose the name).
func (k *Kernel) provide(n *Node, t reflect.Type, name string) error {
	if name == "" {
		return fmt.Errorf("loong: node %q contributes an empty name", n.ID)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, c := range k.contributionsOf(t) {
		if c.Name != name {
			continue
		}
		if c.Node == n.ID {
			return nil
		}
		return fmt.Errorf("loong: contribution %q is already held by node %q", name, c.Node)
	}
	k.provided[t] = append(k.provided[t], contribution{Name: name, Node: n.ID})
	return nil
}

// noteConsumer records that a node resolved a service of type t. It is
// called after the lookup returned, never under k.mu.
func (k *Kernel) noteConsumer(from *Node, t reflect.Type) {
	if from == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	m := k.consumers[t]
	if m == nil {
		m = make(map[string]bool)
		k.consumers[t] = m
	}
	m[from.ID] = true
}

// isNilValue reports whether v is nil, including a typed nil boxed
// into an interface (e.g. a nil *T stored in an any).
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

func (k *Kernel) checkUniqueIDs(root *Node) error {
	seen := make(map[string]bool)
	return k.walk(root, func(n *Node) error {
		if seen[n.ID] {
			return fmt.Errorf("loong: duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
		return nil
	})
}

func (k *Kernel) walk(n *Node, fn func(*Node) error) error {
	if err := fn(n); err != nil {
		return err
	}
	for _, c := range n.Children {
		if err := k.walk(c, fn); err != nil {
			return err
		}
	}
	return nil
}

// walkActive visits n and the descendants that take part in its
// activation, stopping at a lazy node: `lazy: true` turns off the subtree
// under it, so a walk that reaches one of those children would be walking
// into something the tree declared off. Both Assemble and ensureActive
// traverse this way, which is what makes the rule one rule — what stays
// off at startup is exactly what comes up when the node above it is
// activated.
//
// k.walk remains the traversal for views that describe the tree rather
// than run it (Shutdown, contributions, NodeInfo): those must see the
// dormant subtrees too.
func (k *Kernel) walkActive(n *Node, fn func(*Node) error) error {
	if err := fn(n); err != nil {
		return err
	}
	for _, c := range n.Children {
		if c.Lazy {
			continue
		}
		if err := k.walkActive(c, fn); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown stops the tree in reverse Run order (children before
// parents) so dependents stop before their dependencies. Lazy nodes
// that were never activated have no instance and are skipped. All Stop
// calls are attempted; errors are aggregated. Safe to call before
// Assemble (no-op) or after a failed assembly (stops built nodes).
func (k *Kernel) Shutdown() error {
	if k.root == nil {
		return nil
	}
	var nodes []*Node
	_ = k.walk(k.root, func(n *Node) error {
		nodes = append(nodes, n)
		return nil
	})
	return k.stopReverse(nodes)
}

// stopReverse calls Stop on the given nodes in reverse order and
// aggregates all errors. Nodes without an instance (never activated)
// are skipped.
func (k *Kernel) stopReverse(nodes []*Node) error {
	var errs []error
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		if n.component == nil {
			continue
		}
		if err := n.component.Stop(&Scope{Kernel: k, Node: n}); err != nil {
			errs = append(errs, fmt.Errorf("loong: stop %q: %w", n.ID, err))
		}
	}
	return errors.Join(errs...)
}

// emit routes an event from a node to its direct parent handler table.
// The root has no parent; unmatched event names are dropped. Routing
// is deliberately single-hop — see Event for the design contract.
func (k *Kernel) emit(from *Node, e Event) error {
	if from.parent == nil {
		return nil
	}
	if h, ok := from.parent.handlers[e.Name]; ok {
		return h(e)
	}
	return nil
}

// emitStrict is the MustEmit backing: an event without a subscriber on
// the direct parent (or emitted at the root, which has no parent) is a
// loud ErrNoSubscriber instead of a silent drop.
func (k *Kernel) emitStrict(from *Node, e Event) error {
	if from.parent == nil {
		return fmt.Errorf("%w: %q emitted at the root", ErrNoSubscriber, e.Name)
	}
	if h, ok := from.parent.handlers[e.Name]; ok {
		return h(e)
	}
	return fmt.Errorf("%w: %q from %q on parent %q", ErrNoSubscriber, e.Name, from.ID, from.parent.ID)
}
