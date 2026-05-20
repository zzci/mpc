package mobileapi

import (
	"reflect"
	"testing"
)

// allowedKind reports whether t is a gomobile-safe flat type for the B-001
// surface: string, []byte, error / callback interface, or an opaque pointer to
// one of this package's handle structs. Anything else (a struct by value, a
// map, a numeric, a generic type parameter) fails the constraint
// "only string/[]byte/callback, no generics/complex structs exported".
func allowedKind(ty reflect.Type) bool {
	switch ty.Kind() {
	case reflect.String:
		return true
	case reflect.Slice:
		return ty.Elem().Kind() == reflect.Uint8 // []byte
	case reflect.Interface:
		return true // error or a *Callback interface
	case reflect.Pointer:
		// Opaque handle: *SDK / *SignSession (a Go-referenced object across
		// the bridge, not a value-exported struct).
		return ty.Elem() == reflect.TypeOf((*SDK)(nil)).Elem() ||
			ty.Elem() == reflect.TypeOf((*SignSession)(nil)).Elem()
	default:
		return false
	}
}

func assertFlatFunc(t *testing.T, name string, ft reflect.Type, skipRecv bool) {
	t.Helper()
	start := 0
	if skipRecv {
		start = 1 // method value's In(0) is the receiver
	}
	for i := start; i < ft.NumIn(); i++ {
		if !allowedKind(ft.In(i)) {
			t.Errorf("%s: param %d type %s violates the gomobile flat constraint", name, i, ft.In(i))
		}
	}
	for i := 0; i < ft.NumOut(); i++ {
		if !allowedKind(ft.Out(i)) {
			t.Errorf("%s: return %d type %s violates the gomobile flat constraint", name, i, ft.Out(i))
		}
	}
}

// TestExportedSurfaceIsFlat statically (via reflection) proves the whole
// exported B-001 API uses only flat gomobile-bindable types and has no
// generics or value-exported complex structs.
func TestExportedSurfaceIsFlat(t *testing.T) {
	// Constructor (no receiver).
	assertFlatFunc(t, "NewSDK", reflect.TypeOf(NewSDK), false)

	for _, h := range []reflect.Type{
		reflect.TypeOf((*SDK)(nil)),
		reflect.TypeOf((*SignSession)(nil)),
	} {
		for i := 0; i < h.NumMethod(); i++ {
			m := h.Method(i)
			assertFlatFunc(t, h.Elem().Name()+"."+m.Name, m.Type, true)
		}
	}

	// Callback interfaces: their methods are Go→host calls and must also be
	// flat (string/[]byte only). WireCallbacks joins KeyGen/Sign/Reshare
	// callbacks under DM-3 as the outbound bridge.
	for _, ci := range []reflect.Type{
		reflect.TypeOf((*KeyGenCallback)(nil)).Elem(),
		reflect.TypeOf((*SignCallback)(nil)).Elem(),
		reflect.TypeOf((*ReshareCallback)(nil)).Elem(),
		reflect.TypeOf((*WireCallbacks)(nil)).Elem(),
	} {
		for i := 0; i < ci.NumMethod(); i++ {
			m := ci.Method(i)
			assertFlatFunc(t, ci.Name()+"."+m.Name, m.Type, false)
		}
	}
}
