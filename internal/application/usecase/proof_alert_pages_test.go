package usecase_test

import (
	"context"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/port/out"
)

// HU-WRK-002 AC2 (spec 010 DP-05, derived mark): the overdue list repeats a proof until it is reviewed, so once the first page
// holds only proofs that were already alerted, a new overdue proof must still be reached and alerted.
func TestHU_WRK_002_AC2_a_new_overdue_proof_is_alerted_when_the_first_page_holds_only_alerted_ones(t *testing.T) {
	const limit = 3
	f := &fake{proofs: map[string][]out.Proof{tenantA: {{PaymentID: "p1"}, {PaymentID: "p2"}, {PaymentID: "p3"}}}}
	a := &alerts{}
	j := job(f, a, tenantA)
	j.Run(context.Background(), limit)

	f.proofs[tenantA] = append(f.proofs[tenantA], out.Proof{PaymentID: "p4"})
	j.Run(context.Background(), limit)

	if len(a.sent) != 4 || a.sent[3] != tenantA+" p4" {
		t.Fatalf("alerts = %v, want one per proof and p4 included", a.sent)
	}
}

// The memory of the alerts holds what is still overdue and nothing else: a reviewed proof leaves the list and is forgotten.
func TestHU_WRK_002_AC2_a_reviewed_proof_is_forgotten_so_the_memory_does_not_grow(t *testing.T) {
	f := &fake{proofs: map[string][]out.Proof{tenantA: {{PaymentID: "p1"}}}}
	a := &alerts{}
	j := job(f, a, tenantA)
	j.Run(context.Background(), 5)

	f.proofs[tenantA] = nil // p1 was reviewed
	j.Run(context.Background(), 5)
	f.proofs[tenantA] = []out.Proof{{PaymentID: "p1"}} // overdue again later: a new alert is right
	j.Run(context.Background(), 5)

	if len(a.sent) != 2 {
		t.Fatalf("alerts = %v, want p1 alerted again after it left the list", a.sent)
	}
}
