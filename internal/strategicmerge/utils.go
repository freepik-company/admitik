package strategicmerge

import (
	"bytes"
	"encoding/gob"
	"reflect"
)

func init() {
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
}

// DeepCopyMap performs a deep copy of the given map m.
func DeepCopyMap(m map[string]interface{}) (map[string]interface{}, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)
	err := enc.Encode(m)
	if err != nil {
		return nil, err
	}

	var tmpCopy map[string]interface{}
	err = dec.Decode(&tmpCopy)
	if err != nil {
		return nil, err
	}

	return tmpCopy, nil
}

// isMap return whether an interface is a map under the hood
func isMap(val interface{}) bool {
	_, ok := val.(map[string]interface{})
	return ok
}

// isList return whether an interface is a list under the hood
func isList(val interface{}) bool {
	_, ok := val.([]interface{})
	return ok
}

// SanitizeNils recursively traverses a map and replaces both nil slices and nil maps
// with their empty, non-nil equivalents ({} and []).
// This is useful for preparing data structures for JSON serialization where 'null' is undesirable for empty collections.
func SanitizeNils(m map[string]interface{}) map[string]interface{} {
	for k, v := range m {
		// If the value is a raw nil interface{}, we cannot know its original intended type
		// (map or slice). We leave it as is, and it will serialize to 'null'. This is an
		// edge case and typically doesn't happen with well-formed JSON/YAML input.
		if v == nil {
			continue
		}

		// Use reflect to inspect the underlying type of the interface{}
		t := reflect.TypeOf(v)
		val := reflect.ValueOf(v)

		switch t.Kind() {
		case reflect.Map:
			if val.IsNil() {
				m[k] = make(map[string]interface{})
			} else {
				m[k] = SanitizeNils(v.(map[string]interface{}))
			}

		case reflect.Slice:
			if val.IsNil() {
				m[k] = []interface{}{}
			} else {
				// If not nil, iterate over its elements to find nested maps
				// that might also need sanitization.
				subList := v.([]interface{})
				for i, item := range subList {
					if itemMap, ok := item.(map[string]interface{}); ok {
						subList[i] = SanitizeNils(itemMap)
					}
				}
			}
		}
	}
	return m
}
