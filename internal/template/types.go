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

// TODO -----------------------------------------------------

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

// TODO -----------------------------------------------------

type ConditionsInjectedDataT struct {
	TriggerInjectedDataT

	Sources map[int][]map[string]any
	Vars    map[string]any
}

func (ida *ConditionsInjectedDataT) Initialize() {
	ida.TriggerInjectedDataT.Initialize()

	ida.Sources = make(map[int][]map[string]any)
	ida.Vars = make(map[string]any)
}

func (ida *ConditionsInjectedDataT) ToMap() map[string]any {
	tmp := ida.TriggerInjectedDataT.ToMap()

	tmp["sources"] = ida.Sources
	tmp["vars"] = ida.Vars

	return tmp
}

func (ida *ConditionsInjectedDataT) GetVar(key string) (value any) {
	if ida.Vars == nil {
		return nil
	}
	return ida.Vars[key]
}

func (ida *ConditionsInjectedDataT) GetVars() (vars map[string]any) {
	return ida.Vars
}

func (ida *ConditionsInjectedDataT) SetVar(key string, value any) {
	if ida.Vars == nil {
		ida.Vars = make(map[string]any)
	}
	ida.Vars[key] = value
}

func (ida *ConditionsInjectedDataT) SetVars(vars map[string]any) {
	ida.Vars = vars
}
