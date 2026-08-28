package loong

// factories holds component type factories registered via init().
var factories = map[string]func() Component{}

// RegisterComponent declares a component type factory. Components call
// this from their init() so the kernel can instantiate them by type
// name found in the config tree.
func RegisterComponent(typeName string, factory func() Component) {
	factories[typeName] = factory
}
