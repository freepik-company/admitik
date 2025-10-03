package template

type InjectedDataI interface {
	// General methods
	Initialize()
	ToMap() map[string]any

	// Specific methods for some child objects
	// For those not needing them, following methods are implemented as no-op
	GetVar(key string) any
	GetVars() (vars map[string]any)
	SetVar(key string, value any)
	SetVars(vars map[string]any)
}

// TriggerInjectedDataT contains the base data injected into policy templates during evaluation.
// This data structure is common across both admission webhook requests and informer-generated events,
// allowing templates to access consistent context regardless of the trigger source.
// Policy templates can use these fields directly or extend this type with additional context-specific data.
type TriggerInjectedDataT struct {
	Operation string

	Object    map[string]any
	OldObject map[string]any
}

func (ida *TriggerInjectedDataT) Initialize() {
	ida.Object = make(map[string]any)
	ida.OldObject = make(map[string]any)
}

func (ida *TriggerInjectedDataT) ToMap() map[string]any {
	tmp := make(map[string]any)

	tmp["operation"] = ida.Operation
	tmp["object"] = ida.Object
	tmp["oldObject"] = ida.OldObject

	return tmp
}

func (ida *TriggerInjectedDataT) GetVar(key string) (value any)  { return nil }
func (ida *TriggerInjectedDataT) GetVars() (vars map[string]any) { return nil }
func (ida *TriggerInjectedDataT) SetVar(key string, value any)   {}
func (ida *TriggerInjectedDataT) SetVars(vars map[string]any)    {}

// PolicyEvaluationDataT extends TriggerInjectedDataT with additional context for policy evaluation.
// Provides access to source data collections and user-defined variables for use in conditions,
// mutations, and resource generation templates.
type PolicyEvaluationDataT struct {
	TriggerInjectedDataT

	// Sources contains indexed collections of data from external sources (APIs, databases, etc.)
	// that can be referenced in policy templates. Each source is identified by its index.
	Sources map[int][]map[string]any

	// Vars holds user-defined variables computed during policy evaluation,
	// available for reference in subsequent template expressions.
	Vars map[string]any
}

func (ida *PolicyEvaluationDataT) Initialize() {
	ida.TriggerInjectedDataT.Initialize()

	ida.Sources = make(map[int][]map[string]any)
	ida.Vars = make(map[string]any)
}

func (ida *PolicyEvaluationDataT) ToMap() map[string]any {
	tmp := ida.TriggerInjectedDataT.ToMap()

	tmp["sources"] = ida.Sources
	tmp["vars"] = ida.Vars

	return tmp
}

func (ida *PolicyEvaluationDataT) GetVar(key string) (value any) {
	if ida.Vars == nil {
		return nil
	}
	return ida.Vars[key]
}

func (ida *PolicyEvaluationDataT) GetVars() (vars map[string]any) {
	return ida.Vars
}

func (ida *PolicyEvaluationDataT) SetVar(key string, value any) {
	if ida.Vars == nil {
		ida.Vars = make(map[string]any)
	}
	ida.Vars[key] = value
}

func (ida *PolicyEvaluationDataT) SetVars(vars map[string]any) {
	ida.Vars = vars
}
