package designhealth

// layers.go holds the Method layer ranking the graph and chain rules share.
// Ranks encode the closed layered architecture: a call is legal DOWN the ranks
// (Client → Manager → Engine → ResourceAccess → Resource), sideways only when
// queued, and never up. Utilities are cross-cutting — any layer may call them and
// they carry no lines in the static view — so they are exempt from the direction
// rules (rankUtility is a sentinel the rules test for explicitly).
const (
	rankClient         = 0
	rankManager        = 1
	rankEngine         = 2
	rankResourceAccess = 3
	rankResource       = 4
	rankUtility        = -1
	rankUnknown        = -2
)

// kindRank maps a component kind (projectmodel SystemComponent.Kind, the kebab/
// camel wire kind) to its layer rank.
func kindRank(kind string) int {
	switch kind {
	case "client":
		return rankClient
	case "manager":
		return rankManager
	case "engine":
		return rankEngine
	case "resourceAccess":
		return rankResourceAccess
	case "resource":
		return rankResource
	case "utility":
		return rankUtility
	default:
		return rankUnknown
	}
}

// isDirectional reports whether both endpoints participate in the directional
// layering (i.e. neither is a cross-cutting utility and both are known kinds).
func isDirectional(fromKind, toKind string) bool {
	fr, tr := kindRank(fromKind), kindRank(toKind)
	return fr >= rankClient && tr >= rankClient
}
