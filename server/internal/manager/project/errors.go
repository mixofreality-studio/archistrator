package project

import (
	"errors"

	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// ProjectError is the typed façade error, an alias for fwmanager.Error so
// errors.As(&ProjectError) call sites and the framework's Temporal bridge keep
// working uniformly across Managers.
type ProjectError = fwmanager.Error

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}

// mapRAError translates a projectStateAccess error into the Manager façade error
// model, mirroring systemDesignManager's sync-path mapping. fwra.NotFound →
// NotFound (the addressed project has no row); fwra.ContractMisuse →
// ContractMisuse; everything else (incl. Conflict — a thin read/catalog Manager
// has no optimistic-concurrency loop to recover it) → Infrastructure with the
// original retryability preserved.
func mapRAError(err error) error {
	if err == nil {
		return nil
	}
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess")
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}
