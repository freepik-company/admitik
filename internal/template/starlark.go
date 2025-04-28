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
	"encoding/json"
	"errors"
	"fmt"
	admissionv1 "k8s.io/api/admission/v1"
	"strings"

	//
	starlarklog "freepik.com/admitik/internal/template/starlarkmods/log"
	starlarkjson "go.starlark.net/lib/json"
	starlarkmath "go.starlark.net/lib/math"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	starlarksyntax "go.starlark.net/syntax"
)

func EvaluateTemplateStarlark(template string, injectedValues *map[string]interface{}) (result string, err error) {

	// Following code is injected before user's declared code.
	// It's used to perform convenient actions such as conversions
	template = `
# -- Perform some actions in the beginning
# Ref [Specifications]: https://starlark-lang.org/spec.html
# Ref [Playground]: https://starlark-lang.org/playground.html
# Ref [Extra Libs]: https://github.com/google/starlark-go/tree/master/lib

def safe_decode(raw):
    if len(raw) == 0:
        return None
    return json.decode(raw)

operation = __rawOperation
oldObject = safe_decode(__rawOldObject)
object = json.decode(__rawObject)
sources = json.decode(__rawSources)

# -- End of previous actions
` + template

	//
	operation := (*injectedValues)["operation"].(string)

	var oldObjectJsonBytes []byte
	if admissionv1.Operation(operation) == admissionv1.Update {
		oldObjectJsonBytes, err = json.Marshal((*injectedValues)["oldObject"])
		if err != nil {
			return result, fmt.Errorf("error converting old object to JSON: %v", err)
		}
	}

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
		// TODO: Implement modules for YAML, TOML, Logging, etc
		// Injected functions
		"math": starlarkmath.Module,
		"json": starlarkjson.Module,
		"time": starlarktime.Module,
		"log":  starlarklog.Module,

		// Injected data
		"__rawOperation": starlark.String(operation),
		"__rawOldObject": starlark.String(oldObjectJsonBytes),
		"__rawObject":    starlark.String(objectJsonBytes),
		"__rawSources":   starlark.String(sourcesJsonBytes),
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

	_, err = starlark.ExecFileOptions(&starlarksyntax.FileOptions{}, thread, "template.star", template, predeclared)
	if err != nil {
		var evalErr *starlark.EvalError

		if errors.As(err, &evalErr) {
			return result, fmt.Errorf("failed executing starlark code: %v", evalErr.Backtrace())
		}
		return result, fmt.Errorf("failed executing starlark code: %v", err.Error())
	}

	// TODO: Future feature: Populate user-specified vars through executions in a safe way
	//if executionGlobals.Has("vars") {
	//	_ = executionGlobals["vars"]
	//}

	executionOutput := strings.Join(starlarkPrints, "")
	return executionOutput, nil
}
