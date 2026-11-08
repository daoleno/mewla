package stats

import (
	"encoding/json"
	"fmt"
	"reflect"
	"unsafe"
)

// The aggregates a stats refresh builds keep their fields unexported. The
// per-file cache that outlives the process needs them on disk, and a written
// out copy of each field would have to be edited with every field the
// aggregates gain. encodeUsage and decodeUsage walk the value instead, so a new
// field is cached with no edit; TestUsageWireRoundTripsEveryField fills every
// field of the aggregates and fails for a kind of field the walk cannot take.

// encodeUsage turns v into a tree of maps, slices and scalars that
// encoding/json writes exactly.
func encodeUsage(v reflect.Value) (any, error) {
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Pointer:
		if v.IsNil() {
			return nil, nil
		}
		return encodeUsage(v.Elem())
	case reflect.Struct:
		out := make(map[string]any, v.NumField())
		for i := 0; i < v.NumField(); i++ {
			item, err := encodeUsage(v.Field(i))
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", v.Type(), v.Type().Field(i).Name, err)
			}
			out[v.Type().Field(i).Name] = item
		}
		return out, nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key %s", v.Type().Key())
		}
		if v.IsNil() {
			return nil, nil
		}
		out := make(map[string]any, v.Len())
		for _, key := range v.MapKeys() {
			item, err := encodeUsage(v.MapIndex(key))
			if err != nil {
				return nil, err
			}
			out[key.String()] = item
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil, nil
		}
		out := make([]any, v.Len())
		for i := range out {
			item, err := encodeUsage(v.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	}
	return nil, fmt.Errorf("kind %s", v.Kind())
}

// decodeUsage fills v from what encodeUsage wrote, read by json.Decoder with
// UseNumber. A field the data lacks stays as it is.
func decodeUsage(data any, v reflect.Value) error {
	v = usageField(v)
	if data == nil {
		v.SetZero()
		return nil
	}
	switch v.Kind() {
	case reflect.Bool:
		value, ok := data.(bool)
		if !ok {
			return fmt.Errorf("bool from %T", data)
		}
		v.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number, ok := data.(json.Number)
		if !ok {
			return fmt.Errorf("int from %T", data)
		}
		value, err := number.Int64()
		if err != nil {
			return err
		}
		v.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number, ok := data.(json.Number)
		if !ok {
			return fmt.Errorf("uint from %T", data)
		}
		var value uint64
		if _, err := fmt.Sscan(number.String(), &value); err != nil {
			return err
		}
		v.SetUint(value)
	case reflect.Float32, reflect.Float64:
		number, ok := data.(json.Number)
		if !ok {
			return fmt.Errorf("float from %T", data)
		}
		value, err := number.Float64()
		if err != nil {
			return err
		}
		v.SetFloat(value)
	case reflect.String:
		value, ok := data.(string)
		if !ok {
			return fmt.Errorf("string from %T", data)
		}
		v.SetString(value)
	case reflect.Pointer:
		target := reflect.New(v.Type().Elem())
		if err := decodeUsage(data, target.Elem()); err != nil {
			return err
		}
		v.Set(target)
	case reflect.Struct:
		fields, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("struct from %T", data)
		}
		for i := 0; i < v.NumField(); i++ {
			if item, present := fields[v.Type().Field(i).Name]; present {
				if err := decodeUsage(item, v.Field(i)); err != nil {
					return fmt.Errorf("%s.%s: %w", v.Type(), v.Type().Field(i).Name, err)
				}
			}
		}
	case reflect.Map:
		items, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("map from %T", data)
		}
		out := reflect.MakeMapWithSize(v.Type(), len(items))
		for key, item := range items {
			element := reflect.New(v.Type().Elem()).Elem()
			if err := decodeUsage(item, element); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(key).Convert(v.Type().Key()), element)
		}
		v.Set(out)
	case reflect.Slice:
		items, ok := data.([]any)
		if !ok {
			return fmt.Errorf("slice from %T", data)
		}
		out := reflect.MakeSlice(v.Type(), len(items), len(items))
		for i, item := range items {
			if err := decodeUsage(item, out.Index(i)); err != nil {
				return err
			}
		}
		v.Set(out)
	case reflect.Array:
		items, ok := data.([]any)
		if !ok || len(items) != v.Len() {
			return fmt.Errorf("array of %d from %T", v.Len(), data)
		}
		for i, item := range items {
			if err := decodeUsage(item, v.Index(i)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("kind %s", v.Kind())
	}
	return nil
}

// usageField makes an unexported field of an addressable struct settable (a
// field is read through its kind, which reflect allows for unexported fields
// too); any other value is returned as it is.
func usageField(v reflect.Value) reflect.Value {
	if v.CanSet() || !v.CanAddr() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}
