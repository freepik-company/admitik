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

const (
	EngineCel          string = "cel"
	EngineGotmpl       string = "gotmpl"
	EnginePlain        string = "plain"
	EngineStarlark     string = "starlark"
	EnginePlainWithCel string = "plain+cel"
)

func EvaluateTemplate(engine string, template string, injectedData InjectedDataI) (result string, err error) {

	switch engine {
	case EngineCel:
		result, err = EvaluateTemplateCel(template, injectedData)
	case EngineGotmpl:
		result, err = EvaluateTemplateGotmpl(template, injectedData)
	case EnginePlain:
		result, err = EvaluateTemplatePlain(template, injectedData)
	case EngineStarlark:
		result, err = EvaluateTemplateStarlark(template, injectedData)

	// Separated as it's a compound type
	case EnginePlainWithCel:
		result, err = EvaluateAndReplaceCelExpressions(template, injectedData)

	// Default is repeated to make it super explicit
	default:
		result, err = EvaluateTemplateCel(template, injectedData)
	}

	// TODO: DEBUG: log incoming params?

	return result, err
}
