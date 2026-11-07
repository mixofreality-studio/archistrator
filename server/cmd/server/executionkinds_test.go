package main

import (
	"sort"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// TestExecutionKinds_EveryManagerExports pins Task 2.11d: each Manager owns the ExecutionKinds it
// runs (the generated composition roots hand them to the schedule registrar), and together they
// are exactly the table cmd/server/hooks.go used to hand-maintain. A Manager's kinds name its own
// task queue, and a workflow type equal to the kind.
func TestExecutionKinds_EveryManagerExports(t *testing.T) {
	cases := []struct {
		name  string
		got   map[messagebus.ExecutionKind]messagebus.KindBinding
		queue string
		want  []string
	}{
		{"billing", billing.ExecutionKinds(), billing.TaskQueue, []string{"billingCloseCycle", "billingShortfallSweep"}},
		{"delivery", delivery.ExecutionKinds(), delivery.TaskQueue, []string{"constructionPumpSweep", "deliveryRoundSweep"}},
		{"operations", operations.ExecutionKinds(), operations.TaskQueue, []string{"operationsDelinquencyEnforcement", "operationsReconcile"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var names []string
			for kind, b := range c.got {
				names = append(names, string(kind))
				if b.TaskQueue != c.queue {
					t.Errorf("%s: task queue = %q, want %q", kind, b.TaskQueue, c.queue)
				}
				if b.WorkflowType != string(kind) {
					t.Errorf("%s: workflow type = %q, want the kind", kind, b.WorkflowType)
				}
			}
			sort.Strings(names)
			if len(names) != len(c.want) || names[0] != c.want[0] || names[1] != c.want[1] {
				t.Errorf("kinds = %v, want %v", names, c.want)
			}
		})
	}
}
