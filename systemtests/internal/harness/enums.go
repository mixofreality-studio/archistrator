package harness

// The tables in httptransport.go decode a numeric wire ordinal into the name
// a Transport method expects (e.g. artifactKindName(0) -> "mission"), used
// there to decode RESPONSE bodies. The generated-table runner
// (systemtests/usecases) needs the SAME ordinal->name decode for step INPUTS:
// a System Test Plan step's numeric TestArg (e.g. `kind=0`, `decision=1`) must
// become the string a Transport method call expects. These wrappers expose
// that decode without duplicating the ordinal tables — one source of truth
// for the wire contract's enum orderings (mirrored from
// server/internal/manager/{systemdesign,projectdesign}/contract.gen.go).

// ArtifactKindName decodes a plan step's numeric ArtifactKind ordinal (0..16,
// shared by systemDesignManager and projectDesignManager) into the wire kind
// name a Transport method's `kind string` parameter expects.
func ArtifactKindName(ordinal int) string { return artifactKindName(ordinal) }

// ReviewDecisionName decodes a plan step's numeric ReviewDecision ordinal (0
// unknown,1 approve,2 reject,3 withdraw) into the decision name
// Transport.SubmitReview / SubmitProjectReview expect.
func ReviewDecisionName(ordinal int) string {
	for name, o := range reviewDecisionToOrdinal {
		if o == ordinal {
			return name
		}
	}
	return ""
}

// SDPDecisionName decodes a plan step's numeric SDPDecision ordinal (0
// unknown,1 commit,2 rejectAll) into the decision name
// Transport.SubmitSDPDecision expects.
func SDPDecisionName(ordinal int) string {
	for name, o := range sdpDecisionToOrdinal {
		if o == ordinal {
			return name
		}
	}
	return ""
}
