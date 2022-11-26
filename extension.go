package json_ext

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// userTypeInfo stores the information associated with a type the user has handed
// to the package. It's computed once and stored in a map keyed by reflection
// type.
type userTypeInfo struct {
	user  reflect.Type // the type the user handed us
	base  reflect.Type // the base type after all indirections
	indir int          // number of indirections to reach the base type
}

var userTypeCache sync.Map // map[reflect.Type]*userTypeInfo

// validType returns, and saves, the information associated with user-provided type rt.
// If the user type is not valid, err will be non-nil. To be used when the error handler
// is not set up.
func validUserType(rt reflect.Type) (*userTypeInfo, error) {
	if ui, ok := userTypeCache.Load(rt); ok {
		return ui.(*userTypeInfo), nil
	}

	// Construct a new userTypeInfo and atomically add it to the userTypeCache.
	// If we lose the race, we'll waste a little CPU and create a little garbage
	// but return the existing value anyway.

	ut := new(userTypeInfo)
	ut.base = rt
	ut.user = rt
	// A type that is just a cycle of pointers (such as type T *T) cannot
	// be represented in gobs, which need some concrete data. We use a
	// cycle detection algorithm from Knuth, Vol 2, Section 3.1, Ex 6,
	// pp 539-540.  As we step through indirections, run another type at
	// half speed. If they meet up, there's a cycle.
	slowpoke := ut.base // walks half as fast as ut.base
	for {
		pt := ut.base
		if pt.Kind() != reflect.Pointer {
			break
		}
		ut.base = pt.Elem()
		if ut.base == slowpoke { // ut.base lapped slowpoke
			// recursive pointer type.
			return nil, errors.New("can't represent recursive pointer type " + ut.base.String())
		}
		if ut.indir%2 == 0 {
			slowpoke = slowpoke.Elem()
		}
		ut.indir++
	}

	ui, _ := userTypeCache.LoadOrStore(rt, ut)
	return ui.(*userTypeInfo), nil
}

// implementsInterface reports whether the type implements the
// gobEncoder/gobDecoder interface.
// It also returns the number of indirections required to get to the
// implementation.
func implementsInterface(typ, gobEncDecType reflect.Type) (success bool, indir int8) {
	if typ == nil {
		return
	}
	rt := typ
	// The type might be a pointer and we need to keep
	// dereferencing to the base type until we find an implementation.
	for {
		if rt.Implements(gobEncDecType) {
			return true, indir
		}
		if p := rt; p.Kind() == reflect.Pointer {
			indir++
			if indir > 100 { // insane number of indirections
				return false, 0
			}
			rt = p.Elem()
			continue
		}
		break
	}
	// No luck yet, but if this is a base type (non-pointer), the pointer might satisfy.
	if typ.Kind() != reflect.Pointer {
		// Not a pointer, but does the pointer work?
		if reflect.PointerTo(typ).Implements(gobEncDecType) {
			return true, -1
		}
	}
	return false, 0
}

func error_(err error) {
	panic(jsonError{err})
}

// userType returns, and saves, the information associated with user-provided type rt.
// If the user type is not valid, it calls error.
func userType(rt reflect.Type) *userTypeInfo {
	ut, err := validUserType(rt)
	if err != nil {
		error_(err)
	}
	return ut
}

var (
	nameToConcreteType sync.Map // map[string]reflect.Type
	concreteTypeToName sync.Map // map[reflect.Type]string
)

// RegisterName is like Register but uses the provided name rather than the
// type's default.
func RegisterName(name string, value any) {
	if name == "" {
		// reserved for nil
		panic("attempt to register empty name")
	}

	ut := userType(reflect.TypeOf(value))

	// Check for incompatible duplicates. The name must refer to the
	// same user type, and vice versa.

	// Store the name and type provided by the user....
	if t, dup := nameToConcreteType.LoadOrStore(name, reflect.TypeOf(value)); dup && t != ut.user {
		panic(fmt.Sprintf("json: registering duplicate types for %q: %s != %s", name, t, ut.user))
	}

	// but the flattened type in the type table, since that's what decode needs.
	if n, dup := concreteTypeToName.LoadOrStore(ut.base, name); dup && n != name {
		nameToConcreteType.Delete(name)
		panic(fmt.Sprintf("json: registering duplicate names for %s: %q != %q", ut.user, n, name))
	}
}

// Register records a type, identified by a value for that type, under its
// internal type name. That name will identify the concrete type of a value
// sent or received as an interface variable. Only types that will be
// transferred as implementations of interface values need to be registered.
// Expecting to be used only during initialization, it panics if the mapping
// between types and names is not a bijection.
func Register(value any) {
	// Default to printed representation for unnamed types
	rt := reflect.TypeOf(value)
	name := rt.String()

	// But for named types (or pointers to them), qualify with import path (but see inner comment).
	// Dereference one pointer looking for a named type.
	star := ""
	if rt.Name() == "" {
		if pt := rt; pt.Kind() == reflect.Pointer {
			star = "*"
			// NOTE: The following line should be rt = pt.Elem() to implement
			// what the comment above claims, but fixing it would break compatibility
			// with existing jsons.
			//
			// Given package p imported as "full/p" with these definitions:
			//     package p
			//     type T1 struct { ... }
			// this table shows the intended and actual strings used by json to
			// name the types:
			//
			// Type      Correct string     Actual string
			//
			// T1        full/p.T1          full/p.T1
			// *T1       *full/p.T1         *p.T1
			//
			// The missing full path cannot be fixed without breaking existing json decoders.
			rt = pt
		}
	}
	if rt.Name() != "" {
		if rt.PkgPath() == "" {
			name = star + rt.Name()
		} else {
			name = star + rt.PkgPath() + "." + rt.Name()
		}
	}

	RegisterName(name, value)
}
