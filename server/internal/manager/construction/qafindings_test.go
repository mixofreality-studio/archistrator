package construction

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// fakeQueryClient scripts QueryWorkflow with an error, for the F20 pre-phase read test.
// It embeds client.Client so any unimplemented method panics.
type fakeQueryClient struct {
	client.Client
	queryErr error
}

func (f *fakeQueryClient) QueryWorkflow(_ context.Context, _ string, _ string, _ string, _ ...interface{}) (converter.EncodedValue, error) {
	return nil, f.queryErr
}

// ---- F20: clean not-found altitude on the pre-phase construction read ------

// Before construction starts the pump workflow does not exist; Temporal's raw
// "workflow not found for ID: gtdapp:construction" must NOT reach the client. Map it to
// a clean, user-altitude NotFound.
func Test_GetSessionState_BeforeConstruction_CleanNotFound(t *testing.T) {
	fc := &fakeQueryClient{queryErr: fmt.Errorf("workflow not found for ID: gtdapp:construction")}
	m := newTestConstructionManager(fc)

	_, err := m.GetSessionState(testCtx(), ProjectID("gtdapp"), nil)
	e := asConstructionError(t, err)
	if e.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", e.Kind)
	}
	if strings.Contains(e.Detail, "workflow not found") || strings.Contains(e.Detail, "gtdapp:construction") {
		t.Fatalf("Temporal internals leaked to the client: %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "construction has not started") {
		t.Fatalf("want a user-altitude message, got %q", e.Detail)
	}
}
