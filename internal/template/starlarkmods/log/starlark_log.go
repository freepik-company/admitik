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

package log

import (
	"fmt"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"log"
)

// Module log is a Starlark module with simple logging functions.
//
//	log = module(
//	   printf,
//	)
//
// def printf(format, *args):
//
// The printf function writes to standard output.
// It accepts a format string and zero or more arguments.
// If arguments are provided, they are formatted according to the format string.
var Module = &starlarkstruct.Module{
	Name: "log",
	Members: starlark.StringDict{
		"printf": starlark.NewBuiltin("log.printf", printf),
	},
}

func printf(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}

	if len(args) == 0 {
		return nil, fmt.Errorf("%s: missing format string", b.Name())
	}

	formatVal, ok := args[0].(starlark.String)
	if !ok {
		return nil, fmt.Errorf("%s: format must be a string, got %s", b.Name(), args[0].Type())
	}
	format := string(formatVal)

	// Convert remaining args to Go values
	goArgs := make([]interface{}, len(args)-1)
	for i, arg := range args[1:] {
		goArgs[i] = starlarkValueToString(arg)
	}

	log.Printf(format, goArgs...)
	return starlark.None, nil
}

func starlarkValueToString(v starlark.Value) string {
	switch v := v.(type) {
	case starlark.String:
		return string(v)
	case starlark.Int:
		return v.String()
	case starlark.Float:
		return v.String()
	case starlark.Bool:
		if v {
			return "true"
		}
		return "false"
	case starlark.NoneType:
		return "None"
	default:
		return v.String()
	}
}
