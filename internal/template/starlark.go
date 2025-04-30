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
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	//
	admissionv1 "k8s.io/api/admission/v1"
	//
	starletconv "github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	starlarksyntax "go.starlark.net/syntax"

	//
	starlarklog "freepik.com/admitik/internal/template/starlarkmods/log"
	starlarkjson "go.starlark.net/lib/json"
	starlarkmath "go.starlark.net/lib/math"
	starlarktime "go.starlark.net/lib/time"
)

//var varsConverted starlark.Value = starlark.StringDict{}

func EvaluateTemplateStarlark(template string, injectedValues *map[string]interface{}) (result string, err error) {

	// As Starlark interpreter is super resource intensive in some injection-related scenarios
	// a garbage collection is triggered to free memory as fast as possible after evaluation
	defer func() {
		runtime.GC()
		debug.FreeOSMemory()
	}()

	// Following code is injected before user's declared code.
	// It's used to perform convenient actions such as conversions
	template = `
# -- Perform some actions in the beginning

# Ref [Specifications]: https://starlark-lang.org/spec.html
# Ref [Playground]: https://starlark-lang.org/playground.html
# Ref [Extra Libs]: https://github.com/google/starlark-go/tree/master/lib

# def safe_decode(raw):
#     if len(raw) == 0:
#         return None
#     return json.decode(raw)

operation = __rawOperation
oldObject = __rawOldObject
object = __rawObject
sources = __rawSources
vars = __rawVars

# -- End of previous actions
` + template

	//
	operation := (*injectedValues)["operation"].(string)

	var oldObjectStarlark starlark.Value = new(starlark.Dict)
	if admissionv1.Operation(operation) == admissionv1.Update {
		oldObjectStarlark, err = starletconv.GoToStarlarkViaJSON((*injectedValues)["oldObject"])
		if err != nil {
			return result, fmt.Errorf("error converting 'oldObject' into Starlak: %v", err)
		}
	}

	objectStarlark, err := starletconv.GoToStarlarkViaJSON((*injectedValues)["object"])
	if err != nil {
		return result, fmt.Errorf("error converting 'object' into Starlark: %v", err)
	}

	sourcesStarlark, err := starletconv.GoToStarlarkViaJSON((*injectedValues)["sources"])
	if err != nil {
		return result, fmt.Errorf("error converting 'sources' into Starlark: %v", err)
	}

	// Convert user-defined vars into Starlark types
	var convErr error
	var varsStarlark starlark.Value = new(starlark.Dict)
	if _, varsPresent := (*injectedValues)["vars"]; varsPresent {
		varsStarlark, convErr = starletconv.Marshal((*injectedValues)["vars"])
		if convErr != nil {
			return result, fmt.Errorf("failed converting injected 'vars' into Starlak: %v", convErr.Error())
		}
	}

	// This dictionary defines the pre-declared environment.
	predeclared := starlark.StringDict{
		// TODO: Implement modules for YAML, TOML, Logging, etc
		// Another potential approach is to load them using Starlet instead

		// Injected functions
		"math": starlarkmath.Module,
		"json": starlarkjson.Module,
		"time": starlarktime.Module,
		"log":  starlarklog.Module,

		// Injected data
		"__rawOperation": starlark.String(operation),
		"__rawOldObject": oldObjectStarlark,
		"__rawObject":    objectStarlark,
		"__rawSources":   sourcesStarlark,
		"__rawVars":      varsStarlark,
	}

	// Execute Starlark program in a file.
	// Printed stuff will be captured as the result
	var starlarkPrints []string
	thread := &starlark.Thread{
		Name: "template",
		Print: func(_ *starlark.Thread, msg string) {
			starlarkPrints = append(starlarkPrints, msg)
		},
	}

	executionGlobals, err := starlark.ExecFileOptions(&starlarksyntax.FileOptions{}, thread, "template.star", template, predeclared)
	if err != nil {
		var evalErr *starlark.EvalError

		if errors.As(err, &evalErr) {
			return result, fmt.Errorf("failed executing starlark code: %v", evalErr.Backtrace())
		}
		return result, fmt.Errorf("failed executing starlark code: %v", err.Error())
	}

	// Convert Starlark's global 'vars' back to Go types when present to store it in injectedValues
	if executionGlobals.Has("vars") {

		var varsConvertedBack interface{}
		varsConvertedBack, convErr = starletconv.Unmarshal(executionGlobals["vars"])
		if convErr != nil {
			return result, fmt.Errorf("failed converting Starlark 'vars' global into Golang types: %v", convErr.Error())
		}
		(*injectedValues)["vars"] = varsConvertedBack
	}

	executionOutput := strings.Join(starlarkPrints, "")
	return executionOutput, nil
}
