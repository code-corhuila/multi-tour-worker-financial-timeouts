package alert_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"

	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/adapter/out/alert"
)

// HU-WRK-002 AC2: until the notification piece exists the alert is a line with the tenant and the proof, and the run's correlation id.
func TestLog_writes_the_tenant_and_the_proof_and_nothing_personal(t *testing.T) {
	var out bytes.Buffer
	l := alert.NewLog(logging.New(&out))
	ctx := correlation.WithID(context.Background(), "run-0000-aaaa")

	err := l.ProofReviewOverdue(ctx, "tenant-1", "payment-1")

	line := out.String()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"tenant-1", "payment-1", "run-0000-aaaa", "overdue"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q lacks %q", line, want)
		}
	}
}
