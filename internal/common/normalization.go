package common

import (
	"fmt"
)

const (
	// TODO: Move this to a more suitable place
	NormalizedOperationCreate = "CREATE"
	NormalizedOperationUpdate = "UPDATE"
	NormalizedOperationDelete = "DELETE"
)

var (
	// TODO: Move this to a more suitable place
	normalizedOperationsMap = map[string]string{
		"CREATE": NormalizedOperationCreate,
		"ADDED":  NormalizedOperationCreate,

		"UPDATE":   NormalizedOperationUpdate,
		"MODIFIED": NormalizedOperationUpdate,

		"DELETE":  NormalizedOperationDelete,
		"DELETED": NormalizedOperationDelete,
	}
)

func GetNormalizedOperation(operation any) (opResult string) {

	opResult = fmt.Sprintf("%s", operation)

	//
	if normalized, exists := normalizedOperationsMap[opResult]; exists {
		return normalized
	}
	return opResult
}
