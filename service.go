package loong

import "reflect"

// Get returns the service registered for type T, or the zero value
// if no such service is provided. The Go type itself is the service
// key — no string indirection. Declared as a method on Kernel (generic
// methods are supported from Go 1.27); call k.Get[T]().
func (k *Kernel) Get[T any]() T {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()
	if v, ok := k.services[t]; ok {
		return v.(T)
	}
	return zero
}
