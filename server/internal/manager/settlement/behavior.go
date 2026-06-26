package settlement

// This file holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract enum (e.g. the canonical
// name lookup that would otherwise be a String() method) lives here as a free
// function. This is the operations/behavior.go precedent (a contract-value-type
// method becomes a free function so the generated scalar/enum carries no behavior;
// contractstrip refuses to strip an owned type that still has a method).

// derefString returns the pointed-to string, or "" for nil. The generated
// GatewayReversalEvent.ReversesGatewayEventID is optional (`,omitempty` ⇒ *string);
// the revenueLedgerAccess seam carries it as a plain string (empty ⇒ absent).
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// routingDirectiveName returns the canonical name for the façade RoutingDirective.
// Kept as a FREE FUNCTION (not a RoutingDirective method) so the generated enum is
// pure data. NOTE: the deps.go RoutingDirectiveSeam (the Engine's mirror) keeps its
// own String() — that seam is NOT part of the generated contract surface.
func routingDirectiveName(d RoutingDirective) string {
	switch d {
	case RoutingDirectiveNoAction:
		return "NoAction"
	case RoutingDirectivePayout:
		return "Payout"
	case RoutingDirectiveCharge:
		return "Charge"
	default:
		return "Unknown"
	}
}
