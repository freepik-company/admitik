package template

// InjectedDataT represents all the fields that are injected in templates to be used by the user
type InjectedDataT struct {
	Operation string

	Object    map[string]any
	OldObject map[string]any

	Sources map[int][]map[string]any
	Vars    map[string]any
}

func (t *InjectedDataT) Initialize() {
	t.Object = make(map[string]any)
	t.OldObject = make(map[string]any)
	t.Sources = make(map[int][]map[string]any)
	t.Vars = make(map[string]any)
}

func (t *InjectedDataT) ToMap() *map[string]interface{} {
	tmp := make(map[string]interface{})

	tmp["operation"] = t.Operation
	tmp["object"] = t.Object
	tmp["oldObject"] = t.OldObject
	tmp["sources"] = t.Sources
	tmp["vars"] = t.Vars

	return &tmp
}
