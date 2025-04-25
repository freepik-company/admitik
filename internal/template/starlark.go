package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	//
	starlarkjson "go.starlark.net/lib/json"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	starlarksyntax "go.starlark.net/syntax"
)

func EvaluateTemplateStarlark(template string, injectedValues *map[string]interface{}) (result string, err error) {

	// Following code is injected before user's declared code.
	// It's used to perform convenient actions such as conversions
	template = `
# -- Perform some actions in the beginning

sources = json.decode(rawSources)
object = json.decode(rawObject)

# -- End of previous actions
` + template

	//
	objectJsonBytes, err := json.Marshal((*injectedValues)["object"])
	if err != nil {
		return result, fmt.Errorf("error converting object to JSON: %v", err)
	}

	sourcesJsonBytes, err := json.Marshal((*injectedValues)["sources"])
	if err != nil {
		return result, fmt.Errorf("error converting sources to JSON: %v", err)
	}

	// This dictionary defines the pre-declared environment.
	predeclared := starlark.StringDict{
		// Injected functions
		"json": starlarkjson.Module,
		"time": starlarktime.Module,

		// Injected data
		"eventType":  starlark.String((*injectedValues)["eventType"].(string)),
		"rawObject":  starlark.String(objectJsonBytes),
		"rawSources": starlark.String(sourcesJsonBytes),
	}

	// Execute Starlark program in a file.
	// Printed stuff will be captured as the result
	var starlarkPrints []string
	thread := &starlark.Thread{
		Name: "condition",
		Print: func(_ *starlark.Thread, msg string) {
			starlarkPrints = append(starlarkPrints, msg)
		},
	}

	_, err = starlark.ExecFileOptions(&starlarksyntax.FileOptions{}, thread, "condition.star", template, predeclared)
	if err != nil {
		var evalErr *starlark.EvalError

		if errors.As(err, &evalErr) {
			return result, fmt.Errorf("failed executing starlark code: %v", evalErr.Backtrace())
		}
		return result, fmt.Errorf("failed executing starlark code: %v", err.Error())
	}

	executionOutput := strings.Join(starlarkPrints, "")
	return executionOutput, nil
}
