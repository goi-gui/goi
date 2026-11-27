package setup

import (
	"reflect"
	"strings"
)

// bridgeDataValue turns a Go value into rows the interpreted converters read:
// a struct becomes a map of its design fields, a slice becomes []any and a
// string-keyed map keeps its keys. Scalars pass through. The generated code for
// a bridge declaration then builds the typed value it declared.
func bridgeDataValue(v any) any {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case string, bool, int, int8, int16, int32, int64,
		uint, uint16, uint32, uint64, uintptr, float32, float64:
		return v
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		m := make(map[string]any, rv.NumField())
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" {
				continue
			}
			if name := bridgeFieldName(f); name != "" {
				m[name] = bridgeDataValue(rv.Field(i).Interface())
			}
		}
		return m
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = bridgeDataValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
		m := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			m[iter.Key().String()] = bridgeDataValue(iter.Value().Interface())
		}
		return m
	}
	return v
}

// bridgeFieldName is the design-side name of a struct field: its `goi` tag when
// it has one, otherwise the field name with a lower first letter.
func bridgeFieldName(f reflect.StructField) string {
	if tag, ok := f.Tag.Lookup("goi"); ok {
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	if f.Name == "" {
		return ""
	}
	return strings.ToLower(f.Name[:1]) + f.Name[1:]
}
