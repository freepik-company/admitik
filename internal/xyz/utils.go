package xyz

import (
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
)

// GetObjectMapFromRuntimeObject converts the runtime.Object to map[string]interface{}
func GetObjectMapFromRuntimeObject(obj *runtime.Object) (objectData map[string]interface{}, err error) {
	objectData, err = runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	return objectData, err
}

// GetObjectBasicData extracts 'name' and 'namespace' from the object
func GetObjectBasicData(object *map[string]interface{}) (objectData map[string]interface{}, err error) {

	metadata, ok := (*object)["metadata"].(map[string]interface{})
	if !ok {
		err = errors.New("metadata not found or not in expected format")
		return
	}

	objectData = make(map[string]interface{})
	objectData["name"] = metadata["name"]
	objectData["namespace"] = metadata["namespace"]

	return objectData, nil
}
