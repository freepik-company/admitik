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

package template

import (
	"fmt"
	"regexp"
	"strings"

	//
	"github.com/google/cel-go/cel"
)

var (
	// CellBracketExpressionRegexCompiled represents a compiled regex to find {{cel: ...}} patterns
	CellBracketExpressionRegexCompiled = regexp.MustCompile(`{{cel:\s*(.*?)\s*}}`)
)

func EvaluateTemplateCel(template string, injectedData *InjectedDataT) (result string, err error) {

	// Create the environment
	env, err := cel.NewEnv(
		cel.Variable("operation", cel.StringType),
		cel.Variable("object", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("oldObject", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("sources", cel.MapType(cel.IntType, cel.AnyType)),
	)

	if err != nil {
		return "", fmt.Errorf("environment creation error: %s", err.Error())
	}

	// Compile and execute the code
	ast, issues := env.Compile(template)
	if issues != nil && issues.Err() != nil {
		return "", fmt.Errorf("type-check error: %s", issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return "", fmt.Errorf("program construction error: %s", err.Error())
	}

	// The `out` var contains the output of a successful evaluation.
	out, _, err := prg.Eval(map[string]interface{}{
		"operation": injectedData.Operation,
		"object":    injectedData.Object,
		"oldObject": injectedData.OldObject,
		"sources":   injectedData.Sources,
	})

	if err != nil {
		return "", fmt.Errorf("program evaluation error: %s", err.Error())
	}

	result = fmt.Sprintf("%v", out.Value())

	return result, nil
}

// EvaluateAndReplaceCelExpressions finds {{cel: ... }} patterns and evaluates each using EvaluateTemplateCel
// replacing them with their output
func EvaluateAndReplaceCelExpressions(input string, injectedData *InjectedDataT) (string, error) {

	// Find all CEL expression matches
	matches := CellBracketExpressionRegexCompiled.FindAllStringSubmatch(input, -1)

	// Evaluate each expression and replace in the input string
	result := input
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		expression := match[1]

		// Evaluate the CEL expression using your provided function
		evaluatedResult, err := EvaluateTemplateCel(expression, injectedData)
		if err != nil {
			return "", fmt.Errorf("error evaluating CEL expression '%s': %w", expression, err)
		}

		// Replace the entire match ({{cel: expression}}) with the evaluated result
		result = strings.Replace(result, match[0], evaluatedResult, 1)
	}

	return result, nil
}
