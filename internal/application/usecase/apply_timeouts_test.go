package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	workerruntime "github.com/code-corhuila/multi-tour-worker-runtime"

	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/port/out"
	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/usecase"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

// fake is commercial-reservation as the job sees it, and its calls are recorded in order.
type fake struct {
	mu        sync.Mutex
	decisions map[string][]string
	refunds   map[string][]string
	proofs    map[string][]out.Proof
	listErr   map[string]error
	notDue    map[string]bool // ids the service refuses with "not due or frozen"
	failOn    map[string]error
	limits    []int
	calls     []string
}

func (f *fake) record(call string, limit int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if limit > 0 {
		f.limits = append(f.limits, limit)
	}
}

func (f *fake) ListDueDecisions(_ context.Context, tenant string, limit int) ([]string, error) {
	f.record("list-decisions "+tenant, limit)
	return f.decisions[tenant], f.listErr[tenant]
}

func (f *fake) ApplyDefault(_ context.Context, tenant, id string) (bool, error) {
	f.record("default "+tenant+" "+id, 0)
	return !f.notDue[id], f.failOn[id]
}

func (f *fake) ListDueRefunds(_ context.Context, tenant string, limit int) ([]string, error) {
	f.record("list-refunds "+tenant, limit)
	return f.refunds[tenant], nil
}

func (f *fake) MarkOverdue(_ context.Context, tenant, id string) (bool, error) {
	f.record("mark "+tenant+" "+id, 0)
	return !f.notDue[id], f.failOn[id]
}

func (f *fake) ListOverdueProofs(_ context.Context, tenant string, limit int) ([]out.Proof, error) {
	f.record("list-proofs "+tenant, limit)
	return f.proofs[tenant], nil
}

type alerts struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (a *alerts) ProofReviewOverdue(_ context.Context, tenant, paymentID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, tenant+" "+paymentID)
	return a.err
}

func job(f *fake, a *alerts, tenants ...string) *usecase.ApplyTimeouts {
	return usecase.NewApplyTimeouts(f, a, tenants, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var _ workerruntime.Job = (*usecase.ApplyTimeouts)(nil)

// HU-WRK-002 AC1: a decision past its 24 running hours gets the default; the service decides whether it is due.
func TestHU_WRK_002_AC1_applies_the_default_to_every_due_decision(t *testing.T) {
	f := &fake{decisions: map[string][]string{tenantA: {"d1", "d2"}}}

	processed, failed := job(f, &alerts{}, tenantA).Run(context.Background(), 100)

	if processed != 2 || failed != 0 {
		t.Fatalf("processed %d failed %d, want 2 and 0", processed, failed)
	}
	if f.calls[1] != "default "+tenantA+" d1" || f.calls[2] != "default "+tenantA+" d2" {
		t.Errorf("calls = %v", f.calls)
	}
}

// HU-WRK-002 AC1/AC3: frozen, not yet due or already decided by a person is the service's answer, not a failure.
func TestHU_WRK_002_AC1_a_decision_the_service_refuses_is_not_a_failure(t *testing.T) {
	f := &fake{decisions: map[string][]string{tenantA: {"d1"}}, notDue: map[string]bool{"d1": true}}

	processed, failed := job(f, &alerts{}, tenantA).Run(context.Background(), 100)

	if processed != 1 || failed != 0 {
		t.Fatalf("processed %d failed %d, want 1 and 0", processed, failed)
	}
}

// HU-WRK-002 AC2: a refund not executed in 5 business days gets the overdue mark.
func TestHU_WRK_002_AC2_marks_every_due_refund_as_overdue(t *testing.T) {
	f := &fake{refunds: map[string][]string{tenantA: {"f1", "f2"}}, notDue: map[string]bool{"f2": true}}

	processed, failed := job(f, &alerts{}, tenantA).Run(context.Background(), 100)

	if processed != 2 || failed != 0 {
		t.Fatalf("processed %d failed %d, want 2 and 0 (f2 was refused by the service)", processed, failed)
	}
	if f.calls[2] != "mark "+tenantA+" f1" || f.calls[3] != "mark "+tenantA+" f2" {
		t.Errorf("calls = %v", f.calls)
	}
}

// HU-WRK-002 AC2: a proof not reviewed in 24 hours raises the alert once per proof while the worker lives, and never decides on it.
func TestHU_WRK_002_AC2_alerts_each_overdue_proof_once_and_decides_nothing(t *testing.T) {
	f := &fake{proofs: map[string][]out.Proof{tenantA: {{PaymentID: "p1"}, {PaymentID: "p2"}}}}
	a := &alerts{}
	j := job(f, a, tenantA)

	first, _ := j.Run(context.Background(), 100)
	second, _ := j.Run(context.Background(), 100)

	if first != 2 || second != 0 || len(a.sent) != 2 || a.sent[0] != tenantA+" p1" {
		t.Fatalf("handled %d then %d, alerts %v, want 2 then 0 and one alert each", first, second, a.sent)
	}
	for _, c := range f.calls {
		if c != "list-decisions "+tenantA && c != "list-refunds "+tenantA && c != "list-proofs "+tenantA {
			t.Errorf("unexpected call %q: the job only lists proofs", c)
		}
	}
}

// An alert that failed is not remembered, so the next run raises it again.
func TestHU_WRK_002_AC2_a_failed_alert_is_raised_again_by_the_next_run(t *testing.T) {
	f := &fake{proofs: map[string][]out.Proof{tenantA: {{PaymentID: "p1"}}}}
	a := &alerts{err: errors.New("no receiver")}
	j := job(f, a, tenantA)

	_, failed := j.Run(context.Background(), 100)
	a.err = nil
	processed, _ := j.Run(context.Background(), 100)

	if failed != 1 || processed != 1 || len(a.sent) != 2 {
		t.Fatalf("failed %d then processed %d, alerts %v", failed, processed, a.sent)
	}
}

// Annex D: the run asks for exactly BATCH_SIZE in each list, at most the 100 of the page.
func TestHU_WRK_002_AC1_asks_for_exactly_the_batch_size_up_to_the_page_limit(t *testing.T) {
	f := &fake{}
	job(f, &alerts{}, tenantA).Run(context.Background(), 7)
	job(f, &alerts{}, tenantA).Run(context.Background(), 500)

	want := []int{7, 7, 7, 100, 100, 100}
	if len(f.limits) != len(want) {
		t.Fatalf("limits = %v, want %v", f.limits, want)
	}
	for i := range want {
		if f.limits[i] != want[i] {
			t.Fatalf("limits = %v, want %v", f.limits, want)
		}
	}
}

// Annex D: a failed item is counted and logged and the batch goes on.
func TestHU_WRK_002_AC1_a_failed_item_does_not_stop_the_batch(t *testing.T) {
	f := &fake{decisions: map[string][]string{tenantA: {"d1", "d2", "d3"}}, failOn: map[string]error{"d2": errors.New("boom")}}

	processed, failed := job(f, &alerts{}, tenantA).Run(context.Background(), 100)

	if processed != 2 || failed != 1 {
		t.Fatalf("processed %d failed %d, want 2 and 1", processed, failed)
	}
}

// A list that fails is counted once, and the other lists and tenants are still swept.
func TestHU_WRK_002_AC2_a_list_that_fails_does_not_stop_the_other_sweeps_or_tenants(t *testing.T) {
	f := &fake{
		listErr:   map[string]error{tenantA: errors.New("down")},
		decisions: map[string][]string{tenantA: {"dx"}, tenantB: {"d9"}},
		refunds:   map[string][]string{tenantA: {"f1"}},
	}

	processed, failed := job(f, &alerts{}, tenantA, tenantB).Run(context.Background(), 100)

	if failed != 1 || processed != 2 {
		t.Fatalf("processed %d failed %d, want 2 (the refund of A and the decision of B) and 1 (the decisions of A)", processed, failed)
	}
}

// Annex D: a run stops when RUN_TIMEOUT expires.
func TestHU_WRK_002_AC1_a_done_context_stops_the_run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fake{decisions: map[string][]string{tenantA: {"d1"}}}

	processed, failed := job(f, &alerts{}, tenantA).Run(ctx, 100)

	if processed != 0 || failed != 0 || len(f.calls) != 0 {
		t.Fatalf("processed %d failed %d calls %v, want nothing", processed, failed, f.calls)
	}
}
