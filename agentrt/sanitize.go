package agentrt

import (
	"math"
	"reflect"
)

// sanitizeFloats recursively walks through the given value and replaces
// any NaN or Inf float64/float32 values with 0. This prevents json.Marshal
// from returning "unsupported value: NaN" errors.
// See: https://github.com/openITCOCKPIT/openitcockpit-agent-go/issues/88
func sanitizeFloats(v interface{}) interface{} {
	if v == nil {
		return v
	}
	return sanitizeValue(reflect.ValueOf(v)).Interface()
}

func sanitizeValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return reflect.Zero(v.Type())
		}

	case reflect.Map:
		if v.IsNil() {
			return v
		}
		newMap := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, key := range v.MapKeys() {
			newMap.SetMapIndex(key, sanitizeValue(v.MapIndex(key)))
		}
		return newMap

	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		newSlice := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			newSlice.Index(i).Set(sanitizeValue(v.Index(i)))
		}
		return newSlice

	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		newVal := reflect.New(v.Type().Elem())
		newVal.Elem().Set(sanitizeValue(v.Elem()))
		return newVal

	case reflect.Struct:
		newStruct := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			field := newStruct.Field(i)
			if field.CanSet() {
				field.Set(sanitizeValue(v.Field(i)))
			}
		}
		return newStruct

	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		sanitized := sanitizeValue(v.Elem())
		newIface := reflect.New(v.Type()).Elem()
		newIface.Set(sanitized)
		return newIface
	}

	return v
}
