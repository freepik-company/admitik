package strategicmerge

import (
	"errors"
	"fmt"
)

// ExtensionGvkNotFoundInSchemaError indicates the obvious message you can read
type ExtensionGvkNotFoundInSchemaError struct {
	error
}

func (e *ExtensionGvkNotFoundInSchemaError) Error() string {
	return fmt.Sprintf("extension '%v' not found in schema", extGroupVersionKind)
}

// PartialMergeError indicates that a merge operation completed,
// but some items could not be processed due to errors.
type PartialMergeError struct {
	Errors []error
}

func (e *PartialMergeError) Error() string {
	return errors.Join(e.Errors...).Error()
}
