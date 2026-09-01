package loong

import (
	"log/slog"
	"reflect"
	"strings"
)

// serviceDecl pairs a declared service type with the accessor that
// pulls the value out of a component instance after it is built.
type serviceDecl struct {
	typ reflect.Type
	get func(Component) any
}

// regEntry describes one registered component type: its factory plus
// optional registration semantics (services, activation mode, events,
// description).
type regEntry struct {
	factory    func() Component
	services   []serviceDecl // services declared via WithService (may be several)
	configType reflect.Type  // config struct declared via WithConfig
	eager      bool          // activated at assembly instead of lazily (Eager)
	emits      []string      // event names this component emits (WithEvents)
	desc       string        // human description for the component catalog (WithDesc)
}

// factories holds component type factories registered via init().
var factories = map[string]regEntry{}

// serviceOwners tracks which component type declares each service
// type, to warn when two component types claim the same service type
// (the lookup index keeps only one of them).
var serviceOwners = map[reflect.Type]string{}

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
// A component may declare several services (one WithService each).
// Service components are activated lazily by default — their node is
// instantiated, built and run on the first lookup — unless Eager()
// marks them for startup activation. The accessor's return type is
// checked at compile time by the generic parameter, so no runtime type
// assertion is needed.
func WithService[T any](get func(Component) T) ComponentOption {
	return func(e *regEntry) {
		e.services = append(e.services, serviceDecl{
			typ: reflect.TypeOf((*T)(nil)).Elem(),
			get: func(c Component) any { return get(c) },
		})
	}
}

// Eager marks the component type for startup activation, overriding
// the default lazy activation of service components. Use it for
// channels and other components that must be running as soon as the
// tree is assembled even though they expose a service (e.g. web).
func Eager() ComponentOption {
	return func(e *regEntry) { e.eager = true }
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
// services (WithService), activation mode (Eager), emitted events
// (WithEvents) and a description (WithDesc).
func RegisterComponent(typeName string, factory func() Component, opts ...ComponentOption) {
	e := regEntry{factory: factory}
	for _, o := range opts {
		o(&e)
	}
	for _, sd := range e.services {
		if prev, ok := serviceOwners[sd.typ]; ok && prev != typeName {
			slog.Warn("loong: service type already declared by another component type",
				"type", sd.typ.String(), "prev", prev, "now", typeName)
		}
		serviceOwners[sd.typ] = typeName
	}
	factories[typeName] = e
}

// ComponentMeta is the discoverable metadata of one registered
// component type, returned by Components().
type ComponentMeta struct {
	Type         string        // type name used in the config tree
	Desc         string        // WithDesc description
	Service      bool          // exposes at least one service
	ServiceTypes []string      // declared service types, e.g. ["*user.Service"]
	Eager        bool          // activated at assembly (service components are lazy by default)
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
		out = append(out, ComponentMeta{
			Type:         name,
			Desc:         e.desc,
			Service:      len(e.services) > 0,
			ServiceTypes: svcTypes,
			Eager:        e.eager,
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
