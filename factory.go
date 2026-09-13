package loong

import (
	"reflect"
	"strings"
)

// serviceDecl pairs a declared service type with the accessor that
// pulls the value out of a component instance after it is built.
type serviceDecl struct {
	typ      reflect.Type
	get      func(Component) any
	optional bool // nil accessor result means "serves nothing", not an error
}

// regEntry describes one registered component type: its factory plus
// optional registration semantics (services, contributions, events,
// description).
type regEntry struct {
	factory     func() Component
	services    []serviceDecl  // services declared via WithService (may be several)
	contributes []reflect.Type // contribution kinds declared via WithContributes
	configType  reflect.Type   // config struct declared via WithConfig
	emits       []string       // event names this component emits (WithEvents)
	desc        string         // human description for the component catalog (WithDesc)
}

// factories holds component type factories registered via init().
var factories = map[string]regEntry{}

// ComponentOption tweaks how a component type is registered.
type ComponentOption func(*regEntry)

// WithService declares that instances of this component type expose a
// service of type T, and binds the accessor that extracts it from a
// built instance:
//
//	loong.RegisterComponent("user", factory,
//	    loong.WithService(func(c loong.Component) *Service { return c.(*Component).Service }),
//	)
//
// A component may declare several services (one WithService each), and
// one service type may be declared by several component types — the
// declaration is per type, resolution per node, so mounting two of them
// gives two providers rather than a conflict. Activation is
// orthogonal: like any component, a service component is activated at
// assembly unless its node is marked lazy in the config tree, in which
// case the first lookup (Get/GetFrom) activates it on demand. The
// accessor's return type is checked at compile time by the generic
// parameter, so no runtime type assertion is needed.
func WithService[T any](get func(Component) T) ComponentOption {
	return withService(get, false)
}

// WithOptionalService is WithService for a service that may not exist.
// The accessor has the same shape; what changes is what a nil value
// means. For a required service it is a mistake — the component
// returned before it was ready — and it fails the activation. For an
// optional one it is an answer: this node serves nothing of type T, and
// lookups skip it exactly as if it had declared nothing, so "no backend
// configured, no such capability" needs no sentinel value and no error
// field for consumers to unpack.
//
// A consumer of an optional service therefore resolves it with TryGet
// and treats the error as one of the outcomes (see WithContributes for
// the capabilities a node holds without a value at all).
func WithOptionalService[T any](get func(Component) T) ComponentOption {
	return withService(get, true)
}

func withService[T any](get func(Component) T, optional bool) ComponentOption {
	return func(e *regEntry) {
		e.services = append(e.services, serviceDecl{
			typ:      reflect.TypeOf((*T)(nil)).Elem(),
			get:      func(c Component) any { return get(c) },
			optional: optional,
		})
	}
}

// WithContributes declares that nodes of this component type contribute
// an item of kind T, named by the node's own id:
//
//	loong.RegisterComponent("tool", factory,
//	    loong.WithContributes[ToolName](),
//	)
//
// The kind is a Go type, exactly as a service key is, so two families of
// contributions never mix and a typo cannot pass for one; a type may
// declare several kinds. T is only a name — no value is involved, and
// that is the difference from WithService: a contribution is a fact
// about the tree, known from the skeleton, so listing what a subtree
// contributes neither builds nor runs anything that declares it. Names
// that cannot exist before the component runs (a remote server's tool
// list, say) are registered from inside Build with Scope.Provide.
func WithContributes[T any]() ComponentOption {
	return func(e *regEntry) {
		e.contributes = append(e.contributes, reflect.TypeOf((*T)(nil)).Elem())
	}
}

// WithConfig declares the config struct this component type accepts.
// The type is reflected into field metadata surfaced by Components(),
// so consumers can see which keys a config block may contain before
// writing the yaml. It does not change decoding behavior — components
// still decode their own config (see Scope.Config).
func WithConfig[T any]() ComponentOption {
	return func(e *regEntry) {
		e.configType = reflect.TypeOf((*T)(nil)).Elem()
	}
}

// WithEvents declares the event names this component emits. Names may
// end in ".*" to denote a domain prefix (e.g. "user.*"). The list is
// surfaced by Components() for discoverability.
func WithEvents(names ...string) ComponentOption {
	return func(e *regEntry) { e.emits = names }
}

// WithDesc attaches a one-line human description of the component,
// surfaced by Components() for discoverability.
func WithDesc(desc string) ComponentOption {
	return func(e *regEntry) { e.desc = desc }
}

// RegisterComponent declares a component type factory. Components call
// this from their init() so the kernel can instantiate them by type
// name found in the config tree. Optional ComponentOptions declare
// services (WithService), the config struct (WithConfig), emitted
// events (WithEvents) and a description (WithDesc).
//
// It panics on a duplicate type name, mirroring database/sql.Register
// and flag: a silent overwrite would make the config tree resolve to
// whichever init() happened to run last.
func RegisterComponent(typeName string, factory func() Component, opts ...ComponentOption) {
	if _, ok := factories[typeName]; ok {
		panic("loong: component type already registered: " + typeName)
	}
	e := regEntry{factory: factory}
	for _, o := range opts {
		o(&e)
	}
	factories[typeName] = e
}

// RegisterFunc registers a run-only component type from a single
// function. It is sugar over RegisterComponent: the factory returns a
// fresh Func each call, which is stateless, so several nodes of the same
// type are safe. Use it for command nodes that act only when run and
// need no Build/Stop phases; components that decode config or register
// services at build time keep a struct embedding Base.
func RegisterFunc(typeName string, run func(*Scope) error, opts ...ComponentOption) {
	RegisterComponent(typeName, func() Component { return Func(run) }, opts...)
}

// ComponentMeta is the discoverable metadata of one registered
// component type, returned by Components().
type ComponentMeta struct {
	Type         string        // type name used in the config tree
	Desc         string        // WithDesc description
	Service      bool          // exposes at least one service
	ServiceTypes []string      // declared service types, e.g. ["*user.Service"]
	Contributes  []string      // contribution kinds declared via WithContributes
	Emits        []string      // event names declared via WithEvents
	ConfigType   string        // declared config struct name (WithConfig)
	ConfigFields []ConfigField // config keys with types and optionality
}

// ConfigField describes one key of a component's config block.
type ConfigField struct {
	Name     string // yaml key as written in the config tree
	Type     string // Go type name
	Optional bool   // yaml tag carries omitempty
}

// Describe returns the WithDesc description registered for the given
// component type, or "" when the type is unknown or undescribed. It is
// the type-level fallback for node-level descriptions (Node.Desc).
func Describe(componentType string) string {
	return factories[componentType].desc
}

// Components returns metadata for every init()-registered component
// type. It is the discovery entry point for using loong at scale:
// pick a type name for the config tree, inspect its config keys and
// services, then go doc the exported service types for their
// interfaces.
func Components() []ComponentMeta {
	out := make([]ComponentMeta, 0, len(factories))
	for name, e := range factories {
		cfgType, cfgFields := describeConfig(e.configType)
		svcTypes := make([]string, 0, len(e.services))
		for _, sd := range e.services {
			svcTypes = append(svcTypes, sd.typ.String())
		}
		kinds := make([]string, 0, len(e.contributes))
		for _, kind := range e.contributes {
			kinds = append(kinds, kind.String())
		}
		out = append(out, ComponentMeta{
			Type:         name,
			Desc:         e.desc,
			Service:      len(e.services) > 0,
			ServiceTypes: svcTypes,
			Contributes:  kinds,
			Emits:        e.emits,
			ConfigType:   cfgType,
			ConfigFields: cfgFields,
		})
	}
	return out
}

// describeConfig reflects a config struct type into field metadata.
// Nil or non-struct types yield empty output (component takes no
// declared config).
func describeConfig(typ reflect.Type) (string, []ConfigField) {
	if typ == nil || typ.Kind() == reflect.Ptr && typ.Elem().Kind() != reflect.Struct {
		return "", nil
	}
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return "", nil
	}
	fields := make([]ConfigField, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields = append(fields, ConfigField{
			Name:     name,
			Type:     f.Type.String(),
			Optional: strings.Contains(opts, "omitempty"),
		})
	}
	return typ.String(), fields
}
