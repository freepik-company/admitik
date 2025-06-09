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
	"reflect"
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
	modSelfYaml "freepik.com/admitik/internal/template/starlarkmods/yaml"
)

func EvaluateTemplateStarlark(template string, injectedData *InjectedDataT) (result string, err error) {

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
# Ref [Extra Libs: Official]: https://github.com/google/starlark-go/tree/master/lib
# Ref [Extra Libs: Starlet]: https://github.com/1set/starlet/blob/master/lib

operation = __rawOperation
oldObject = __rawOldObject
object = __rawObject
sources = __rawSources
vars = __rawVars

# -- End of previous actions
` + template

	//
	operationStarlark := starlark.String(injectedData.Operation)
	operationStarlark.Freeze()

	oldObjectStarlark, err := starletconv.GoToStarlarkViaJSON(injectedData.OldObject)
	if err != nil {
		return result, fmt.Errorf("error converting 'oldObject' into Starlark: %v", err)
	}
	oldObjectStarlark.Freeze()

	objectStarlark, err := starletconv.GoToStarlarkViaJSON(injectedData.Object)
	if err != nil {
		return result, fmt.Errorf("error converting 'object' into Starlark: %v", err)
	}
	objectStarlark.Freeze()

	sourcesStarlark, err := starletconv.GoToStarlarkViaJSON(injectedData.Sources)
	if err != nil {
		return result, fmt.Errorf("error converting 'sources' into Starlark: %v", err)
	}
	sourcesStarlark.Freeze()

	// Convert user-defined vars into Starlark types
	var convErr error
	var varsStarlark starlark.Value = starlark.NewDict(0)
	if !reflect.ValueOf(injectedData.Vars).IsZero() {
		varsStarlark, convErr = starletconv.Marshal(injectedData.Vars)
		if convErr != nil {
			return result, fmt.Errorf("failed converting injected 'vars' into Starlark: %v", convErr.Error())
		}
	}

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

	predeclared := starlark.StringDict{

		// Injected functions
		"math": modStarlarkMath.Module,
		"json": modStarlarkJson.Module,
		"time": modStarlarkTime.Module,

		//
		"base64":  starletBase64["base64"],
		"csv":     starletCsv["csv"],
		"hashlib": starletHashlib["hashlib"],
		"http":    starletHttp["http"],
		"log":     starletLog["log"],
		"net":     starletNet["net"],
		"random":  starletRandom["random"],
		"re":      starletRe["re"],
		"string":  starletString["string"],

		//
		// TODO: Implement missing modules for YAML, TOML, etc
		"yaml": modSelfYaml.Module,

		// Injected data
		"__rawOperation": operationStarlark, // Frozen
		"__rawOldObject": oldObjectStarlark, // Frozen
		"__rawObject":    objectStarlark,    // Frozen
		"__rawSources":   sourcesStarlark,   // Frozen
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
	// TODO: Review this conversions
	if executionGlobals.Has("vars") {

		var varsConvertedBack interface{}
		varsConvertedBack, convErr = starletconv.Unmarshal(executionGlobals["vars"])
		if convErr != nil {
			return result, fmt.Errorf("failed converting Starlark 'vars' global into Golang types: %v", convErr.Error())
		}

		varsUltraConvertedBack, ok := varsConvertedBack.(map[string]interface{})
		if !ok {
			return result, fmt.Errorf("failed converting Starlark 'vars' global into Golang types")
		}
		injectedData.Vars = varsUltraConvertedBack
	}

	executionOutput := strings.Join(starlarkPrints, "")
	return executionOutput, nil
}
