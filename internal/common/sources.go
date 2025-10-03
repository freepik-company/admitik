/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/registry/sources"
	"github.com/freepik-company/admitik/internal/template"
)

// FetchPolicySources TODO
func FetchPolicySources[T policystore.PolicyResourceI](
	sourcesReg *sources.SourcesRegistry,
	policy T,
	injectedData template.InjectedDataI, // TODO: This can be present, or not
) (results map[int][]map[string]any, err error) {

	var tmpErrors []error

	results = make(map[int][]map[string]any)

	for sourceIndex, sourceItem := range policy.GetSources() {

		gvrString := strings.Join([]string{sourceItem.Group, sourceItem.Version, sourceItem.Resource}, "/")
		allResources := sourcesReg.GetResources(gvrString)

		// Deep copy to avoid mutating the original policy (full of pointers, dude)
		sourceItemCopy := sourceItem.DeepCopy()

		// Resolve CEL expressions in filters
		if localErr := resolveInlineCelExpressionsRecursive(reflect.ValueOf(sourceItemCopy.Filters), injectedData); localErr != nil {
			tmpErrors = append(tmpErrors, fmt.Errorf("failed to resolve CEL in filters for GVR '%v': %v", gvrString, localErr))
			results[sourceIndex] = []map[string]any{}
			continue
		}

		// Filter resources
		filtered, localErr := filterResources(allResources, sourceItemCopy.Filters)
		if localErr != nil {
			tmpErrors = append(tmpErrors, fmt.Errorf("failed to filter resources for GVR '%v': %v", gvrString, localErr))
			results[sourceIndex] = []map[string]any{}
			continue
		}

		for _, resource := range filtered {
			results[sourceIndex] = append(results[sourceIndex], *resource)
		}
	}

	return results, errors.Join(tmpErrors...)
}

// resolveInlineCelExpressionsRecursive walks filters structure and resolves CEL in all strings
func resolveInlineCelExpressionsRecursive(v reflect.Value, injectedData template.InjectedDataI) error {
	if !v.IsValid() || (v.Kind() == reflect.Ptr && v.IsNil()) {
		return nil
	}

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			resolved, err := template.EvaluateAndReplaceCelExpressions(v.String(), injectedData)
			if err != nil {
				return err
			}
			v.SetString(resolved)
		}

	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := resolveInlineCelExpressionsRecursive(v.Index(i), injectedData); err != nil {
				return err
			}
		}

	case reflect.Map:
		for iter := v.MapRange(); iter.Next(); {
			if iter.Value().Kind() == reflect.String {
				resolved, err := template.EvaluateAndReplaceCelExpressions(iter.Value().String(), injectedData)
				if err != nil {
					return err
				}
				v.SetMapIndex(iter.Key(), reflect.ValueOf(resolved))
			}
		}

	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := resolveInlineCelExpressionsRecursive(v.Field(i), injectedData); err != nil {
				return err
			}
		}
	}

	return nil
}

// filterResources applies SourceGroupFiltersT to a list of resources
func filterResources(resources []*map[string]any, filters *v1alpha1.SourceGroupFiltersT) ([]*map[string]any, error) {
	if filters == nil {
		return resources, nil
	}

	// Pre-compile regexes once
	var namespaceRegex, nameRegex *regexp.Regexp
	var err error

	if filters.Namespace != nil && filters.Namespace.MatchRegex != nil {
		namespaceRegex, err = regexp.Compile(filters.Namespace.MatchRegex.Expression)
		if err != nil {
			return nil, fmt.Errorf("invalid namespace regex expression %q: %w",
				filters.Namespace.MatchRegex.Expression, err)
		}
	}
	if filters.Name != nil && filters.Name.MatchRegex != nil {
		nameRegex, err = regexp.Compile(filters.Name.MatchRegex.Expression)
		if err != nil {
			return nil, fmt.Errorf("invalid name regex expression %q: %w",
				filters.Name.MatchRegex.Expression, err)
		}
	}

	results := make([]*map[string]any, 0, len(resources))

	for _, resource := range resources {
		if matchesFilters(resource, filters, namespaceRegex, nameRegex) {
			results = append(results, resource)
		}
	}

	return results, nil
}

// matchesFilters checks if a resource matches all specified filters
func matchesFilters(resource *map[string]any, filters *v1alpha1.SourceGroupFiltersT, nsRegex, nameRegex *regexp.Regexp) bool {
	metadata, ok := (*resource)["metadata"].(map[string]any)
	if !ok {
		return false
	}

	// Namespace filter
	if filters.Namespace != nil {
		namespace, _ := metadata["namespace"].(string)
		if !matchesNamespaceFilter(namespace, filters.Namespace, nsRegex) {
			return false
		}
	}

	// Name filter
	if filters.Name != nil {
		name, _ := metadata["name"].(string)
		if !matchesNameFilter(name, filters.Name, nameRegex) {
			return false
		}
	}

	// Metadata filter (labels/annotations)
	if filters.Metadata != nil {
		if !matchesMetadataFilter(metadata, filters.Metadata) {
			return false
		}
	}

	return true
}

// matchesNamespaceFilter checks if namespace matches the filter criteria
func matchesNamespaceFilter(namespace string, filter *v1alpha1.SourceGroupFiltersNamespaceT, regex *regexp.Regexp) bool {
	// MatchList has precedence
	if len(filter.MatchList) > 0 {
		return slices.Contains(filter.MatchList, namespace)
	}

	// MatchRegex
	if filter.MatchRegex != nil && regex != nil {
		matches := regex.MatchString(namespace)
		if filter.MatchRegex.Negative {
			return !matches
		}
		return matches
	}

	return true
}

// matchesNameFilter checks if name matches the filter criteria
func matchesNameFilter(name string, filter *v1alpha1.SourceGroupFiltersNameT, regex *regexp.Regexp) bool {
	if len(filter.MatchList) > 0 {
		return slices.Contains(filter.MatchList, name)
	}

	if filter.MatchRegex != nil && regex != nil {
		matches := regex.MatchString(name)
		if filter.MatchRegex.Negative {
			return !matches
		}
		return matches
	}

	return true
}

// matchesMetadataFilter checks if labels and annotations match the filter criteria
func matchesMetadataFilter(metadata map[string]any, filter *v1alpha1.SourceGroupFiltersMetadataT) bool {
	// Labels
	if len(filter.MatchLabels) > 0 {
		labels, _ := metadata["labels"].(map[string]any)
		if !isMapSubset(labels, filter.MatchLabels) {
			return false
		}
	}

	// Annotations
	if len(filter.MatchAnnotations) > 0 {
		annotations, _ := metadata["annotations"].(map[string]any)
		if !isMapSubset(annotations, filter.MatchAnnotations) {
			return false
		}
	}

	return true
}

// isMapSubset checks if all expected key-value pairs exist in source map
func isMapSubset(source map[string]any, expected map[string]string) bool {
	for k, v := range expected {
		val, ok := source[k].(string)
		if !ok || val != v {
			return false
		}
	}
	return true
}
