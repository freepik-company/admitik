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
	starletconv "github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	starlarksyntax "go.starlark.net/syntax"

	// Starlark official modules
	// Ref: https://github.com/google/starlark-go/blob/master/lib
	modStarlarkJson "go.starlark.net/lib/json"
	modStarlarkMath "go.starlark.net/lib/math"
	modStarlarkTime "go.starlark.net/lib/time"

	// Starlet modules
	// Ref: https://github.com/1set/starlet/blob/master/lib
	modStarletBase64 "github.com/1set/starlet/lib/base64"
	modStarletCsv "github.com/1set/starlet/lib/csv"
	modStarletHashlib "github.com/1set/starlet/lib/hashlib"
	modStarletHttp "github.com/1set/starlet/lib/http"
	modStarletLog "github.com/1set/starlet/lib/log"
	modStarletNet "github.com/1set/starlet/lib/net"
	modStarletRandom "github.com/1set/starlet/lib/random"
	modStarletRe "github.com/1set/starlet/lib/re"
	modStarletString "github.com/1set/starlet/lib/string"

	// own modules
	modSelfYaml "github.com/freepik-company/admitik/internal/template/starlarkmods/yaml"
)

func EvaluateTemplateStarlark(template string, injectedData InjectedDataI) (result string, err error) {

	// As Starlark interpreter is super resource intensive in some injection-related scenarios
	// a garbage collection is triggered to free memory as fast as possible after evaluation
	defer func() {
		runtime.GC()
		debug.FreeOSMemory()
	}()

	injectedDataMap := injectedData.ToMap()

	// Build the predeclared variables dynamically from the map
	predeclaredData := starlark.StringDict{}

	for key, value := range injectedDataMap {
		var starlarkValue starlark.Value
		var convErr error

		// Convert Go value to Starlark
		starlarkValue, convErr = starletconv.GoToStarlarkViaJSON(value)
		if convErr != nil {
			return "", fmt.Errorf("error converting '%s' into Starlark: %v", key, convErr)
		}

		if key != "vars" {
			starlarkValue.Freeze()
		}

		// Store with __raw prefix for the template initialization
		predeclaredData["__raw_"+key] = starlarkValue
	}

	// Build the initialization code dynamically
	var initLines []string
	for key := range injectedDataMap {
		initLines = append(initLines, fmt.Sprintf("%s = __raw_%s", key, key))
	}

	// Following code is injected before user's declared code.
	// It's used to perform convenient actions such as conversions
	template = `
# -- Perform some actions in the beginning
# Ref [Specifications]: https://starlark-lang.org/spec.html
# Ref [Playground]: https://starlark-lang.org/playground.html

` + strings.Join(initLines, "\n") + "\n\n" + template

	// This dictionary defines the pre-declared environment (injected libraries included).
	starletBase64, _ := modStarletBase64.LoadModule()
	starletCsv, _ := modStarletCsv.LoadModule()
	starletHashlib, _ := modStarletHashlib.LoadModule()
	starletHttp, _ := modStarletHttp.LoadModule()
	starletLog, _ := modStarletLog.LoadModule()
	starletNet, _ := modStarletNet.LoadModule()
	starletRandom, _ := modStarletRandom.LoadModule()
	starletRe, _ := modStarletRe.LoadModule()
	starletString, _ := modStarletString.LoadModule()

	// Add modules to predeclared
	predeclaredData["math"] = modStarlarkMath.Module
	predeclaredData["json"] = modStarlarkJson.Module
	predeclaredData["time"] = modStarlarkTime.Module
	predeclaredData["base64"] = starletBase64["base64"]
	predeclaredData["csv"] = starletCsv["csv"]
	predeclaredData["hashlib"] = starletHashlib["hashlib"]
	predeclaredData["http"] = starletHttp["http"]
	predeclaredData["log"] = starletLog["log"]
	predeclaredData["net"] = starletNet["net"]
	predeclaredData["random"] = starletRandom["random"]
	predeclaredData["re"] = starletRe["re"]
	predeclaredData["string"] = starletString["string"]
	predeclaredData["yaml"] = modSelfYaml.Module

	// Execute Starlark program in a file.
	// Printed stuff will be captured as the result
	var starlarkPrints []string
	thread := &starlark.Thread{
		Name: "template",
		Print: func(_ *starlark.Thread, msg string) {
			starlarkPrints = append(starlarkPrints, msg)
		},
	}

	executionGlobals, err := starlark.ExecFileOptions(&starlarksyntax.FileOptions{}, thread, "template.star", template, predeclaredData)
	if err != nil {
		var evalErr *starlark.EvalError
		if errors.As(err, &evalErr) {
			return "", fmt.Errorf("failed executing starlark code: %v", evalErr.Backtrace())
		}
		return "", fmt.Errorf("failed executing starlark code: %v", err.Error())
	}

	// Handle vars mutations
	if executionGlobals.Has("vars") {
		varsConvertedBack, convErr := starletconv.Unmarshal(executionGlobals["vars"])
		if convErr != nil {
			return "", fmt.Errorf("failed converting Starlark 'vars' global into Golang types: %v", convErr.Error())
		}

		if varsMap, ok := varsConvertedBack.(map[string]interface{}); ok {
			injectedData.SetVars(varsMap)
		}
	}

	return strings.Join(starlarkPrints, ""), nil
}
