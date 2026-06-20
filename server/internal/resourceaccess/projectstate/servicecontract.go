package projectstate

// servicecontract.go holds the typed service-contract corpus model for the
// construction head-state (Task: typed ServiceContract model). It mirrors the
// ActivityConstructionStatus precedent exactly: plain Go structs, NO json tags,
// stored in Project.ServiceContracts (keyed by component name), populated only
// when seeded (nil until first use).
//
// DESIGN: the ServiceContract types are extracted from the real contract
// markdown corpus (implementation/contracts/*.md). They are typed head-state,
// not rendered blobs — the webClient renders from these typed fields so the
// SPA can project the contract corpus without markdown parsing.

// GoField is one field in a Go struct shown in the "Code/interface" view.
type GoField struct {
	Name string
	Type string
	Note string
}

// ContractStruct is one Go struct (request or response) carried by an op.
type ContractStruct struct {
	Name   string
	Fields []GoField
}

// ContractOp is one operation/method on the service contract.
type ContractOp struct {
	Signature  string
	Stereotype string // Manager Temporal kind / Engine purity / RA idempotency / Client entry
	Note       string
	Inputs     []ContractStruct // request struct(s) — the op's input type(s) + fields
	Outputs    []ContractStruct // response struct(s) — the op's output type(s) + fields
}

// ContractParty is one caller or callee in the service contract.
// How is populated for outbound parties only (e.g. "Activity-wrapped · …",
// "direct, by value"); it is empty for inbound callers.
type ContractParty struct {
	Name  string
	Layer string
	How   string // outbound only; empty for inbound
}

// ContractRevision records one re-cut of the service contract.
type ContractRevision struct {
	Rev        string // r1, r2, ...
	At         string // date
	By         string // author role
	ByActivity string // which activity re-cut it (D-CW, C-CW review, ...)
	Summary    string
}

// ServiceContract is the typed model for one component's service contract,
// extracted from the real contract markdown. One per component, keyed by
// component name in Project.ServiceContracts. Additive, nil until seeded.
type ServiceContract struct {
	Component     string
	Layer         string // Client | Manager | Engine | ResourceAccess | Utility
	Stereotype    string // the C4-code stereotype banner
	Volatility    string // the encapsulated volatility
	Status        string // FROZEN | IN-DESIGN
	Inbound       []ContractParty
	Outbound      []ContractParty
	Ops           []ContractOp
	DataContracts []string
	ErrorModel    string
	Idempotency   string
	Revisions     []ContractRevision
}
