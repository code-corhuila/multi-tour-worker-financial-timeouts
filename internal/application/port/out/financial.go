// Package out holds the ports the job needs from the outside world.
package out

import "context"

// Proof is a payment proof past its 24 hour review window, as the list names it. It carries no name, amount or file.
type Proof struct {
	PaymentID string
}

// Financial is commercial-reservation as the job sees it: the application never learns that it is HTTP. Every list names
// candidates and every action is decided by the service at the call, so the job holds no deadline, penalty or calendar rule.
type Financial interface {
	// ListDueDecisions reads the ids of the first limit decisions whose 24 running hours ended and that are not frozen.
	ListDueDecisions(ctx context.Context, tenantID string, limit int) ([]string, error)
	// ApplyDefault asks for the default 100 % refund. It reports false when the service refuses it (frozen, not due or already decided).
	ApplyDefault(ctx context.Context, tenantID, decisionID string) (applied bool, err error)
	// ListDueRefunds reads the ids of the first limit authorized refunds whose 5 business days passed and that are not marked.
	ListDueRefunds(ctx context.Context, tenantID string, limit int) ([]string, error)
	// MarkOverdue asks for the overdue mark of a refund. It reports false when the service refuses it (not due or executed).
	MarkOverdue(ctx context.Context, tenantID, refundID string) (marked bool, err error)
	// ListOverdueProofs reads the first limit proofs that were not reviewed within 24 hours. The job never reviews them.
	ListOverdueProofs(ctx context.Context, tenantID string, limit int) ([]Proof, error)
}

// Alerts raises the alert of a proof not reviewed in time. Until the notification piece exists its adapter only writes a line
// without personal data.
type Alerts interface {
	ProofReviewOverdue(ctx context.Context, tenantID, paymentID string) error
}
