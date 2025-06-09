package yaml

import (
	"fmt"
	starletconv "github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"gopkg.in/yaml.v3"
)

var Module = &starlarkstruct.Module{
	Name: "yaml",
	Members: starlark.StringDict{
		"encode": starlark.NewBuiltin("yaml.encode", yamlEncode),
		"decode": starlark.NewBuiltin("yaml.decode", yamlDecode),
	},
}

func yamlEncode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if args.Len() != 1 {
		return nil, fmt.Errorf("yaml.encode: expected 1 argument")
	}
	goVal, err := starletconv.Unmarshal(args.Index(0))
	if err != nil {
		return nil, fmt.Errorf("yaml.encode: cannot convert to Go: %w", err)
	}
	yamlBytes, err := yaml.Marshal(goVal)
	if err != nil {
		return nil, fmt.Errorf("yaml.encode: marshal error: %w", err)
	}
	return starlark.String(string(yamlBytes)), nil
}

func yamlDecode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if args.Len() != 1 {
		return nil, fmt.Errorf("yaml.decode: expected 1 argument")
	}
	yamlStr, ok := starlark.AsString(args.Index(0))
	if !ok {
		return nil, fmt.Errorf("yaml.decode: argument must be string")
	}
	var decoded interface{}
	err := yaml.Unmarshal([]byte(yamlStr), &decoded)
	if err != nil {
		return nil, fmt.Errorf("yaml.decode: unmarshal error: %w", err)
	}
	return starletconv.Marshal(decoded)
}
