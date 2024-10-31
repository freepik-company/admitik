package xyz

import (
	"errors"
)

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
