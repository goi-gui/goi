package schema

// StateNames are the keys a `state { ... }` modifier accepts, in the order the
// completion list shows them. `default` is the fallback when no other state is
// active; the rest follow the element's interaction and control state.
var StateNames = []string{
	"default",
	"hover",
	"active",
	"pressed",
	"focus",
	"disabled",
	"enabled",
	"checked",
	"unchecked",
	"selected",
	"expanded",
	"collapsed",
}

// StateName reports whether name is a state key.
func StateName(name string) bool {
	for _, s := range StateNames {
		if s == name {
			return true
		}
	}
	return false
}
