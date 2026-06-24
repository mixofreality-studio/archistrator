package billing

import (
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// This file bridges the Manager's Activity results to Temporal's retry machinery.
// framework-go's manager package owns the canonical error→Temporal ApplicationError
// mapping ([[the-method-layers]] "Temporal mapping"). The generic mapErr helper
// threads any Activity call's (T, error) result through fwmgr.MapError so
// non-retryable port failures surface to Temporal as terminal errors of the
// canonical Type(), and retryable kinds stay retryable.

// mapErr is the ONE generic error-mapping helper used by every Activity method.
func mapErr[T any](v T, err error) (T, error) {
	return v, fwmgr.MapError(err)
}
