package projectdesign

import (
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// This file bridges the Manager's Activity results to Temporal's retry machinery.
// The ResourceAccess ports return framework-go layer errors; in-workflow Engine
// errors are mapped via fwmanager.MapError directly. framework-go's manager
// package owns the canonical error→Temporal ApplicationError mapping
// ([[the-method-layers]] "Temporal mapping"). The generic mapErr helper threads
// any Activity call's (T, error) result through fwmanager.MapError so non-retryable
// port failures surface to Temporal as terminal errors of the canonical Type().
//
// This is the Phase-2 twin of systemdesign/errors.go — identical body.

// mapErr is the ONE generic error-mapping helper used by every Activity method.
func mapErr[T any](v T, err error) (T, error) {
	return v, fwmanager.MapError(err)
}
