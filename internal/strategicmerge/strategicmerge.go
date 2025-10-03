package strategicmerge

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	//
	"k8s.io/client-go/discovery"
	"k8s.io/kube-openapi/pkg/util/proto"
)

const (
	// Extension keys
	extGroupVersionKind = "x-kubernetes-group-version-kind"
	extMapType          = "x-kubernetes-map-type"
	extListType         = "x-kubernetes-list-type"
	extListMapKeys      = "x-kubernetes-list-map-keys"
	extPatchStrategy    = "x-kubernetes-patch-strategy"
	extPatchMergeKey    = "x-kubernetes-patch-merge-key"

	// Strategy types
	strategyMerge   = "merge"
	strategyReplace = "replace"

	// List/Map types
	typeGranular = "granular"
	typeAtomic   = "atomic"
	typeMap      = "map"
	typeSet      = "set"
)

type StrategicMergePatcherDependencies struct {
	DiscoveryClient *discovery.DiscoveryClient
}
type StrategicMergePatcher struct {
	mu sync.RWMutex

	//
	discoveryClient *discovery.DiscoveryClient

	// carried stuff
	openApiModels       *proto.Models
	openApiSchemasByGVK map[schema.GroupVersionKind]*proto.Schema
}

func NewStrategicMergePatcher(deps *StrategicMergePatcherDependencies) (*StrategicMergePatcher, error) {

	smp := &StrategicMergePatcher{
		discoveryClient:     deps.DiscoveryClient,
		openApiSchemasByGVK: map[schema.GroupVersionKind]*proto.Schema{},
	}

	// Initial OpenAPI models parsing
	err := smp.updateOpenapiModels()
	if err != nil {
		return nil, err
	}

	// Update models periodically
	go smp.keepUpdatedOpenapiModels()

	return smp, nil
}

// updateOpenapiModels fetches the Kubernetes OpenAPI models from Kubernetes and stores them
// in the StrategicMergePatcher as a local cache for performing queries
func (r *StrategicMergePatcher) updateOpenapiModels() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	models, err := r.fetchOpenapiModels()
	if err != nil {
		return fmt.Errorf("failed getting OpenAPI models from Kubernetes: %v", err.Error())
	}

	r.openApiModels = models
	r.openApiSchemasByGVK, err = r.getMappedSchemasByGVK(models)
	if err != nil {
		return fmt.Errorf("failed getting schemas from OpenAPI models: %v", err.Error())
	}

	return nil
}

// keepUpdatedOpenapiModels updates local cache of Kubernetes OpenAPI models periodically
// This function intended to be executed as a goroutine
func (r *StrategicMergePatcher) keepUpdatedOpenapiModels() {
	for {
		err := r.updateOpenapiModels()
		if err != nil {
			log.Printf("%v", err.Error())
			goto takeANap
		}

	takeANap:
		time.Sleep(5 * time.Second)
	}
}

// fetchOpenapiModels returns Kubernetes OpenAPI models
func (r *StrategicMergePatcher) fetchOpenapiModels() (*proto.Models, error) {
	openapiSchema, err := r.discoveryClient.OpenAPISchema()
	if err != nil {
		return nil, err
	}

	models, err := proto.NewOpenAPIData(openapiSchema)
	if err != nil {
		return nil, err
	}

	return &models, nil
}

// getMappedSchemasByGVK receives the OpenAPI models and returns a map of Schemas with GVKs as indexes
func (r *StrategicMergePatcher) getMappedSchemasByGVK(models *proto.Models) (result map[schema.GroupVersionKind]*proto.Schema, err error) {

	result = map[schema.GroupVersionKind]*proto.Schema{}

	errorList := []error{}

	for _, modelName := range (*models).ListModels() {
		modelSchema := (*r.openApiModels).LookupModel(modelName)
		if modelSchema == nil {
			continue
		}

		// Extract as much GVKs as model has
		var gvkList []schema.GroupVersionKind
		gvkList, err = r.GetSchemaGVK(modelSchema)
		if err != nil {
			// Ignore 'GVK not found' errors as child schemas have NOT a GVK
			gvkNotFoundErr := &ExtensionGvkNotFoundInSchemaError{}
			if errors.As(err, &gvkNotFoundErr) {
				continue
			}

			//
			errorList = append(errorList, err)
			continue
		}

		for _, gvk := range gvkList {
			result[gvk] = &modelSchema
		}
	}

	return result, errors.Join(errorList...)
}

// GetModelSchema returns a Schema for the given model name
// Example: 'io.k8s.api.core.v1.Volume' -> proto.Schema
func (r *StrategicMergePatcher) GetModelSchema(modelName string) proto.Schema {

	r.mu.RLock()
	defer r.mu.RUnlock()

	return (*r.openApiModels).LookupModel(modelName)
}

// resolveSchemaReference receives a Schema and returns it back.
// When the Schema is a reference under the hood, it returns the dereferenced Schema instead
// This function only resolves one level of reference
func (r *StrategicMergePatcher) resolveSchemaReference(schema proto.Schema) (proto.Schema, error) {

	if schema == nil {
		return schema, fmt.Errorf("nil schema provided")
	}

	ref, schemaIsAReference := schema.(*proto.Ref)
	if !schemaIsAReference {
		return schema, nil
	}

	modelName := filepath.Base(ref.Reference())
	resolvedSchema := r.GetModelSchema(modelName)

	if resolvedSchema != nil {
		return resolvedSchema, nil
	}

	return nil, fmt.Errorf("schema not found for model '%v'", modelName)
}

// GetSchemaGVK returns the list of GVKs for the given Schema
func (r *StrategicMergePatcher) GetSchemaGVK(modelSchema proto.Schema) (result []schema.GroupVersionKind, err error) {

	var errorList []error

	// Extract as much GVKs as model has
	modelSchemaGVK, modelSchemaHasGvk := modelSchema.GetExtensions()[extGroupVersionKind]
	if !modelSchemaHasGvk {
		return nil, &ExtensionGvkNotFoundInSchemaError{}
	}

	rawGVKListConverted, rawGVKListConversionOk := modelSchemaGVK.([]interface{})
	if !rawGVKListConversionOk {
		return nil, fmt.Errorf("impossible to assert GVK list for schema")
	}

	for _, rawGVKItem := range rawGVKListConverted {

		gvkMap, ok := rawGVKItem.(map[interface{}]interface{})
		if !ok {
			errorList = append(errorList, fmt.Errorf("failed asserting GVK item for schema"))
			continue
		}

		//
		groupRaw, groupOk := gvkMap["group"]
		versionRaw, versionOk := gvkMap["version"]
		kindRaw, kindOk := gvkMap["kind"]

		if !groupOk || !versionOk || !kindOk {
			errorList = append(errorList, fmt.Errorf("incomplete GVK in schema"))
			continue
		}

		//
		group, groupStringOk := groupRaw.(string)
		version, versionStringOk := versionRaw.(string)
		kind, kindStringOk := kindRaw.(string)

		if !groupStringOk || !versionStringOk || !kindStringOk {
			errorList = append(errorList, fmt.Errorf("GVK fields are not strings in schema"))
			continue
		}

		//
		gvk := schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    kind,
		}

		result = append(result, gvk)
	}

	return result, errors.Join(errorList...)
}

// GetSchemaByGVK returns the Schema for the given GVK
func (r *StrategicMergePatcher) GetSchemaByGVK(gvk schema.GroupVersionKind) proto.Schema {

	r.mu.Lock()
	defer r.mu.Unlock()

	tmpSchema, tmpSchemaFound := r.openApiSchemasByGVK[gvk]
	if !tmpSchemaFound {
		return nil
	}

	return *tmpSchema
}

// schemaPatchStrategyInfo represents the strategy used to merge maps or lists
// and the relevant keys used during the process.
type schemaPatchStrategyInfo struct {
	Strategy  string
	MergeKeys []string
}

// getPatchStrategyFromSchema inspects a field's Schema and determines its patching strategy.
// It prioritizes 'x-kubernetes-map-type' and 'x-kubernetes-list-type' definitions (common in CRDs for map and list topology)
// over 'x-kubernetes-patch-strategy' (for traditional Strategic Merge Patch).
// Ref: https://github.com/kubernetes/apiextensions-apiserver/blob/master/pkg/apis/apiextensions/types_jsonschema.go#L110-L152
// Ref: https://kubernetes.io/docs/reference/using-api/server-side-apply/#merge-strategy
func getPatchStrategyFromSchema(schema proto.Schema) schemaPatchStrategyInfo {
	// If no schema is provided, the default behavior for a field is "replace".
	if schema == nil {
		return schemaPatchStrategyInfo{Strategy: strategyReplace}
	}

	extensions := schema.GetExtensions()

	// Priority 1: Check for x-kubernetes-map-type, which describes the map's topology.
	if mapType, ok := extensions[extMapType].(string); ok {
		switch mapType {
		case typeGranular:
			// For "granular" maps, they can be merged as usual
			return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: []string{}}

		case typeAtomic:
			// For "atomic" maps, the entire map is always replaced.
			return schemaPatchStrategyInfo{Strategy: strategyReplace}
		}
	}

	// Priority 2: Check for x-kubernetes-list-type, which describes the list's topology.
	if listType, ok := extensions[extListType].(string); ok {
		switch listType {

		case typeMap:

			// For "map" lists, look for x-kubernetes-list-map-keys to identify merge keys.
			if mapKeysRaw, ok := extensions[extListMapKeys].([]interface{}); ok {
				mergeKeys := make([]string, 0, len(mapKeysRaw))
				for _, key := range mapKeysRaw {
					if s, isString := key.(string); isString {
						mergeKeys = append(mergeKeys, s)
					}
				}

				if len(mergeKeys) > 0 {
					return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: mergeKeys}
				}
			}

			// For map-keys being absent or invalid, the merge behavior is undefined.
			// Default to "replace" for safety.
			return schemaPatchStrategyInfo{Strategy: strategyReplace}

		case typeSet:
			// For "set" lists, elements are unique by value. The strategy is "merge",
			// but no explicit merge keys are used (the entire item value acts as its key).
			return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: []string{}}

		case typeAtomic:
			// For "atomic" lists, the entire list is always replaced.
			return schemaPatchStrategyInfo{Strategy: strategyReplace}
		}
	}

	// Priority 3: If no specific x-kubernetes-list-type was defined or relevant,
	// check for x-kubernetes-patch-strategy for traditional Strategic Merge Patch behavior.
	if strategy, ok := extensions[extPatchStrategy].(string); ok && strings.Contains(strategy, strategyMerge) {

		// If the strategy is "merge", look for x-kubernetes-patch-merge-key.
		if mergeKey, ok := extensions[extPatchMergeKey].(string); ok {
			return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: []string{mergeKey}}
		}

		// If it's "merge" but there's no patch-merge-key (e.g., for scalar lists that merge by appending unique items).
		return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: []string{}}
	}

	// Priority 4: Detect free maps
	// These are objects not specifying any strategy extensions to merged items, just objects with 'additionalProperties' field.
	// These are maps with arbitrary keys: labels, annotations, data, etc.
	if _, ok := schema.(*proto.Map); ok {
		return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: []string{}}
	}

	// Priority 5: Detect free structs
	// These are objects not specifying any strategy extensions to merged items, but with properties DO defined
	if _, ok := schema.(*proto.Kind); ok {
		return schemaPatchStrategyInfo{Strategy: strategyMerge, MergeKeys: []string{}}
	}

	// Default behavior: If no explicit merge strategy or list type is found,
	// the field is entirely replaced.
	return schemaPatchStrategyInfo{Strategy: strategyReplace}
}

// generateCompositeKey creates a stable, unique string key from an item's specified merge fields.
func generateCompositeKey(item map[string]interface{}, mergeKeys []string) (string, error) {
	var parts []string
	for _, key := range mergeKeys {

		value, exists := item[key]
		if !exists {
			return "", fmt.Errorf("merge key field '%s' is missing in item: %v", key, item)
		}

		valString := fmt.Sprintf("%v", value)

		parts = append(parts, valString)
	}

	// Use a separator unlikely to appear in actual field values.
	return strings.Join(parts, "::"), nil
}

// mergeScalarList handles merging of lists without explicit merge keys (e.g., lists of strings/ints, or "set" lists).
// It adds unique elements from the patch to the original.
func mergeScalarList(original, patch []interface{}) []interface{} {
	seen := make(map[interface{}]struct{})
	result := make([]interface{}, 0, len(original)+len(patch))

	// Add all original unique items
	for _, item := range original {
		if _, exists := seen[item]; !exists {
			result = append(result, item)
			seen[item] = struct{}{}
		}
	}

	// Add unique items from patch that are not already in original
	for _, item := range patch {
		if _, exists := seen[item]; !exists {
			result = append(result, item)
			seen[item] = struct{}{}
		}
	}
	return result
}

// mergeLists merges two lists based on the provided patch strategy information.
func (r *StrategicMergePatcher) mergeLists(original, patch []interface{}, patchInfo schemaPatchStrategyInfo, listSchema proto.Schema) ([]interface{}, error) {
	// Strategy is not "merge": replace the original list with the patch list.
	if patchInfo.Strategy != strategyMerge {
		return patch, nil
	}

	// Strategy is "merge" but no merge keys are provided (e.g., for "set" lists or scalar lists),
	// we perform a simple append of unique elements.
	if len(patchInfo.MergeKeys) == 0 {
		return mergeScalarList(original, patch), nil
	}

	// Get item schema for recursive merging of list elements.
	var itemSchema proto.Schema
	if arraySchema, ok := listSchema.(*proto.Array); ok {
		// itemSchema can be a reference. We don't need to resolve it as this function will call
		// StrategicMerge() for merging maps, which is always resolving them in the beginning
		itemSchema = arraySchema.SubType
	}

	// Create an index of patch items by their composite key for efficient lookup.
	patchIndex := make(map[string]map[string]interface{})
	for _, item := range patch {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			// Skip non-map items in a merge-keyed list
			continue
		}

		compositeKey, err := generateCompositeKey(itemMap, patchInfo.MergeKeys)
		if err != nil {
			return nil, fmt.Errorf("failed to generate composite key for patch item: %v (error: %v)", item, err)
		}

		// Key generation failed (e.g., missing required key fields)
		if compositeKey == "" {
			return nil, fmt.Errorf("missing required key fields")
		}
		patchIndex[compositeKey] = itemMap
	}

	var resultList []interface{}
	var partialProcessingErrors []error

	// Iterate through the original list to merge or keep items.
	for originalItemIndex, originalItem := range original {
		originalItemMap, ok := originalItem.(map[string]interface{})
		if !ok {
			partialProcessingErrors = append(partialProcessingErrors, fmt.Errorf("original list item at index %d is not an object; cannot merge by key. Item: %v", originalItemIndex, originalItem))

			// If original item is not a map, it cannot be merged by key; append it as is.
			resultList = append(resultList, originalItem)
			continue
		}

		compositeKey, err := generateCompositeKey(originalItemMap, patchInfo.MergeKeys)
		if err != nil {
			partialProcessingErrors = append(partialProcessingErrors, fmt.Errorf("failed to generate composite key for original list item at index %d: %w. Item will be appended without merge attempt", originalItemIndex, err))

			// If original item's key cannot be generated, append it as is.
			resultList = append(resultList, originalItem)
			continue
		}

		if compositeKey == "" {
			partialProcessingErrors = append(partialProcessingErrors, fmt.Errorf("empty composite key generated for original list item at index %d, indicating missing required fields. Item will be appended without merge attempt", originalItemIndex))
			resultList = append(resultList, originalItem)
			continue
		}

		// If a matching item exists in the patch, merge it.
		if patchItemMap, inPatch := patchIndex[compositeKey]; inPatch {
			mergedItem, mergeErr := r.StrategicMerge(originalItemMap, patchItemMap, itemSchema)
			if mergeErr != nil {
				partialErr := &PartialMergeError{}
				if errors.As(mergeErr, &partialErr) {
					partialProcessingErrors = append(partialProcessingErrors, partialErr.Errors...)
				} else {
					return nil, mergeErr
				}
			}

			resultList = append(resultList, mergedItem)

			// Remove from index as it's been processed
			delete(patchIndex, compositeKey)
		} else {
			// If no matching item in patch, keep the original.
			resultList = append(resultList, originalItem)
		}
	}

	// Add any remaining items from the patch (new items).
	newKeys := make([]string, 0, len(patchIndex))
	for k := range patchIndex {
		newKeys = append(newKeys, k)
	}

	// Ensure deterministic order for new items
	sort.Strings(newKeys)

	for _, k := range newKeys {
		resultList = append(resultList, patchIndex[k])
	}

	if len(partialProcessingErrors) > 0 {
		return resultList, &PartialMergeError{Errors: partialProcessingErrors}
	}

	return resultList, nil
}

// StrategicMerge performs a strategic merge on two map[string]interface{} objects.
func (r *StrategicMergePatcher) StrategicMerge(original, patch map[string]interface{}, schema proto.Schema) (map[string]interface{}, error) {
	var err error

	// 1. Resolve Schema references if applicable.
	// This ensures we always work with a concrete schema for field lookups.
	schema, err = r.resolveSchemaReference(schema)
	if err != nil {
		return nil, fmt.Errorf("failed resolving schema reference: %v", err.Error())
	}

	result, err := DeepCopyMap(original)
	if err != nil {
		return nil, fmt.Errorf("failed deep-copying original object: %w", err)
	}

	result = SanitizeNils(result)

	// 2. Iterate over fields in 'patch' looking for them in the 'original' object.
	// Depending on the type of field (map, list or primitive), perform the most suitable type of merge,
	// then overwrite the entire field in the original object
	var mergeErrors []error // Collect errors during the merge operation

	for patchKey, patchValue := range patch {

		originalValue, originalExists := result[patchKey]

		// Get schema for the current field being processed.
		// TODO: Can we resolve here and avoid the first transformation? lets see in later iterations
		var fieldSchema proto.Schema
		if kindSchema, ok := schema.(*proto.Kind); ok {
			fieldSchema = kindSchema.Fields[patchKey]

			// Fail when 'patch' have unknown fields. Patch must make sense like perfection of a smooth ice-cream
			if fieldSchema == nil {
				return nil, fmt.Errorf("patch is malformed. Invalid field '%v' in '%v'", patchKey, kindSchema.BaseSchema.Path.String())

				// TODO: Ignore fields instead?
				//continue
			}

			fieldSchema, err = r.resolveSchemaReference(fieldSchema)
			if err != nil {
				return nil, fmt.Errorf("failed resolving child schema reference for patch key '%v': %v", patchKey, err.Error())
			}
		}

		switch {
		case isMap(patchValue) && isMap(originalValue):
			patchInfo := getPatchStrategyFromSchema(fieldSchema)
			if patchInfo.Strategy == strategyReplace {
				// The map is "atomic", so we replace it entirely.
				result[patchKey] = patchValue
			} else {

				// The map is "granular" (or no strategy specified), perform a recursive strategic merge.
				merged, mergeErr := r.StrategicMerge(originalValue.(map[string]interface{}), patchValue.(map[string]interface{}), fieldSchema)
				if mergeErr != nil {
					partialErr := &PartialMergeError{}
					if errors.As(mergeErr, &partialErr) {
						// Accumulate partial errors
						mergeErrors = append(mergeErrors, partialErr.Errors...)
					} else {
						return nil, mergeErr
					}
				}
				result[patchKey] = merged
			}

		case isList(patchValue):

			// Patch value is a list, apply list merging strategy.
			patchInfo := getPatchStrategyFromSchema(fieldSchema)

			originalList := []interface{}{} // Initialize empty list if original doesn't exist or isn't a list
			if originalExists {
				if ol, ok := originalValue.([]interface{}); ok {
					originalList = ol
				}
			}

			mergedList, mergeErr := r.mergeLists(originalList, patchValue.([]interface{}), patchInfo, fieldSchema)
			if mergeErr != nil {
				partialErr := &PartialMergeError{}
				if errors.As(mergeErr, &partialErr) {
					// Accumulate partial errors
					mergeErrors = append(mergeErrors, partialErr.Errors...)
				} else {
					return nil, mergeErr
				}
			}
			result[patchKey] = mergedList

		default:
			// For primitive types or type mismatches, replace if different or new.
			if !originalExists || !reflect.DeepEqual(originalValue, patchValue) {
				result[patchKey] = patchValue
			}
		}
	}

	if len(mergeErrors) > 0 {
		return result, &PartialMergeError{Errors: mergeErrors}
	}

	return result, nil
}
