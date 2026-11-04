// Package messagebus is the in-process, fire-and-forget change-notice bus (Utility).
package messagebus

import (
	"errors"
	"sync/atomic"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

var (
	// errInvalidMessage is returned (terminal) for an empty topic or scope.
	errInvalidMessage = errors.New("messagebus: invalid message: topic and scope must be non-empty")
	// errBusUnavailable is returned (transient) when the bus is shut down or cannot accept the notice.
	errBusUnavailable = errors.New("messagebus: bus unavailable")
)

// messageBus is the in-process, fire-and-forget bus. No subscribers exist in
// this release, so accepted notices are dropped.
type messageBus struct {
	closed atomic.Bool
}

func newMessageBus() MessageBus {
	return &messageBus{}
}

func (b *messageBus) Publish(_ fwra.Context, message Message) error {
	if message.Topic == "" || message.Scope == "" {
		return errInvalidMessage
	}
	if b.closed.Load() {
		return errBusUnavailable
	}
	return nil
}
