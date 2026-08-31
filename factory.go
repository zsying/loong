package loong

import "reflect"

// regEntry describes one registered component type: its factory plus
// optional registration semantics.
type regEntry struct {
	factory     func() Component
	service     bool         // declared as a lazily-activated service component
	serviceType reflect.Type // service value type declared via AsService[T]
}

// factories holds component type factories registered via init().
var factories = map[string]regEntry{}

// ComponentOption tweaks how a component type is registered.
type ComponentOption func(*regEntry)

// AsService declares that instances of this component type expose a
// service of type T through the ServiceProvider interface, and that
// the node is activated lazily: it is instantiated, built and run on
// the first Get[T]() (or Kernel.Activate) instead of during assembly.
// Only one node of a service component type may be mounted, because
// service lookup is global and unique per Go type.
func AsService[T any]() ComponentOption {
	return func(e *regEntry) {
		e.service = true
		e.serviceType = reflect.TypeOf((*T)(nil)).Elem()
	}
}

// RegisterComponent declares a component type factory. Components call
// this from their init() so the kernel can instantiate them by type
// name found in the config tree. Optional ComponentOptions (e.g.
// AsService[T]) adjust the registration semantics.
func RegisterComponent(typeName string, factory func() Component, opts ...ComponentOption) {
	e := regEntry{factory: factory}
	for _, o := range opts {
		o(&e)
	}
	factories[typeName] = e
}
