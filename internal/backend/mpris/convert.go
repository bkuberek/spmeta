package mpris

import "reflect"

// valuer is implemented by dbus.Variant. Matching on the method keeps godbus
// out of the files that build on every operating system.
type valuer interface{ Value() any }

// unwrapProperties converts a GetAll reply (a{sv}) to Properties.
func unwrapProperties[V any](m map[string]V) Properties {
	p := make(Properties, len(m))
	for k, v := range m {
		p[k] = unwrap(v)
	}
	return p
}

// unwrap turns D-Bus values into the plain Go values documented on
// Properties: variants are opened, object paths become strings, a{sv} becomes
// map[string]any, and arrays of strings become []string. Scalars, []string
// and []byte pass through.
func unwrap(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case valuer:
		return unwrap(x.Value())
	case string, []string, []byte:
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Map:
		return unwrapMap(rv, v)
	case reflect.Slice:
		return unwrapSlice(rv)
	default:
		return v
	}
}

func unwrapMap(rv reflect.Value, orig any) any {
	if rv.Type().Key().Kind() != reflect.String {
		return orig
	}
	out := make(map[string]any, rv.Len())
	for it := rv.MapRange(); it.Next(); {
		out[it.Key().String()] = unwrap(it.Value().Interface())
	}
	return out
}

func unwrapSlice(rv reflect.Value) any {
	if rv.Type().Elem().Kind() == reflect.String {
		out := make([]string, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).String()
		}
		return out
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = unwrap(rv.Index(i).Interface())
	}
	return out
}
