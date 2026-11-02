package pubsub

import (
	"errors"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

var (
	errBlankAudience = errors.New("pubsub: audience must not be blank")
	errBlankKind     = errors.New("pubsub: event kind must not be blank")
)

// pubSub is the in-process, best-effort, at-most-once publisher. The contract
// exposes no subscribing side, so there is never a registered subscriber to
// reach; publishing to an audience with no subscribers is a success.
type pubSub struct{}

func newPubSub() PubSub {
	return &pubSub{}
}

// Publish hands the publication off without ever waiting on a subscriber.
// The only errors are terminal: a blank audience or a blank kind.
func (p *pubSub) Publish(_ fwra.Context, publication Publication) error {
	if strings.TrimSpace(string(publication.Audience)) == "" {
		return errBlankAudience
	}
	if strings.TrimSpace(string(publication.Kind)) == "" {
		return errBlankKind
	}
	return nil
}
