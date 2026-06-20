package web

import (
	"github.com/davidmarne/archistrator/server/internal/manager/project"
	ps "github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// servicecontracts.go projects the typed service-contract corpus onto camelCase
// wire DTOs. Honest-empty: returns nil when the source map is empty/nil (so the
// serviceContracts field is omitted from the JSON envelope via omitempty).
// Pure projection — no business logic.

type contractPartyDTO struct {
	Name  string `json:"name"`
	Layer string `json:"layer"`
	How   string `json:"how,omitempty"` // outbound only; empty for inbound
}

type goFieldDTO struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Note string `json:"note,omitempty"`
}

type contractStructDTO struct {
	Name   string       `json:"name"`
	Fields []goFieldDTO `json:"fields"`
}

type contractOpDTO struct {
	Signature  string              `json:"signature"`
	Stereotype string              `json:"stereotype"`
	Note       string              `json:"note,omitempty"`
	Inputs     []contractStructDTO `json:"inputs,omitempty"`
	Outputs    []contractStructDTO `json:"outputs,omitempty"`
}

type contractRevisionDTO struct {
	Rev        string `json:"rev"`
	At         string `json:"at"`
	By         string `json:"by"`
	ByActivity string `json:"byActivity,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

type serviceContractDTO struct {
	Component     string                `json:"component"`
	Layer         string                `json:"layer"`
	Stereotype    string                `json:"stereotype,omitempty"`
	Volatility    string                `json:"volatility,omitempty"`
	Status        string                `json:"status,omitempty"`
	Inbound       []contractPartyDTO    `json:"inbound,omitempty"`
	Outbound      []contractPartyDTO    `json:"outbound,omitempty"`
	Ops           []contractOpDTO       `json:"ops,omitempty"`
	DataContracts []string              `json:"dataContracts,omitempty"`
	ErrorModel    string                `json:"errorModel,omitempty"`
	Idempotency   string                `json:"idempotency,omitempty"`
	Revisions     []contractRevisionDTO `json:"revisions,omitempty"`
}

// serviceContractsFromState projects the typed service-contract map onto the wire
// DTO map. Returns nil when the source map is empty/nil (omitted on the wire via
// omitempty — the honest-empty convention).
func serviceContractsFromState(s project.ProjectState) map[string]serviceContractDTO {
	if len(s.ServiceContracts) == 0 {
		return nil
	}
	out := make(map[string]serviceContractDTO, len(s.ServiceContracts))
	for name, sc := range s.ServiceContracts {
		inbound := make([]contractPartyDTO, 0, len(sc.Inbound))
		for _, p := range sc.Inbound {
			inbound = append(inbound, contractPartyDTO{Name: p.Name, Layer: p.Layer, How: p.How})
		}
		outbound := make([]contractPartyDTO, 0, len(sc.Outbound))
		for _, p := range sc.Outbound {
			outbound = append(outbound, contractPartyDTO{Name: p.Name, Layer: p.Layer, How: p.How})
		}
		ops := make([]contractOpDTO, 0, len(sc.Ops))
		for _, op := range sc.Ops {
			ops = append(ops, contractOpDTO{
				Signature:  op.Signature,
				Stereotype: op.Stereotype,
				Note:       op.Note,
				Inputs:     nilIfEmpty(contractStructsToDTO(op.Inputs)),
				Outputs:    nilIfEmpty(contractStructsToDTO(op.Outputs)),
			})
		}
		revisions := make([]contractRevisionDTO, 0, len(sc.Revisions))
		for _, rev := range sc.Revisions {
			revisions = append(revisions, contractRevisionDTO{
				Rev:        rev.Rev,
				At:         rev.At,
				By:         rev.By,
				ByActivity: rev.ByActivity,
				Summary:    rev.Summary,
			})
		}
		out[name] = serviceContractDTO{
			Component:     sc.Component,
			Layer:         sc.Layer,
			Stereotype:    sc.Stereotype,
			Volatility:    sc.Volatility,
			Status:        sc.Status,
			Inbound:       nilIfEmpty(inbound),
			Outbound:      nilIfEmpty(outbound),
			Ops:           nilIfEmpty(ops),
			DataContracts: sc.DataContracts,
			ErrorModel:    sc.ErrorModel,
			Idempotency:   sc.Idempotency,
			Revisions:     nilIfEmpty(revisions),
		}
	}
	return out
}

// contractStructsToDTO projects a slice of ContractStruct model values onto their
// wire DTO equivalents. Returns nil when the input is nil/empty (caller uses
// nilIfEmpty to ensure honest-empty omission on the wire).
func contractStructsToDTO(structs []ps.ContractStruct) []contractStructDTO {
	if len(structs) == 0 {
		return nil
	}
	out := make([]contractStructDTO, 0, len(structs))
	for _, cs := range structs {
		fields := make([]goFieldDTO, 0, len(cs.Fields))
		for _, f := range cs.Fields {
			fields = append(fields, goFieldDTO{Name: f.Name, Type: f.Type, Note: f.Note})
		}
		out = append(out, contractStructDTO{Name: cs.Name, Fields: fields})
	}
	return out
}

// nilIfEmpty returns nil when s is an empty (zero-length) slice, so omitempty
// JSON tags omit the field rather than encoding []. The honest-empty convention.
func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
