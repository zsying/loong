package loong

import "reflect"

// Get returns the service registered for type T, or the zero value
// if no such service is provided. The Go type itself is the service
// key — no string indirection.
func Get[T any](k *Kernel) T {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()
	if v, ok := k.services[t]; ok {
		return v.(T)
	}
	return zero
}
