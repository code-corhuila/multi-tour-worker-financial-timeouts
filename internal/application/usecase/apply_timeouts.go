// Package usecase holds the jobs of the worker.
package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	workerruntime "github.com/code-corhuila/multi-tour-worker-runtime"

	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/port/out"
)

// maxPageSize is the most the lists of the contract give in one page.
const maxPageSize = 100

// ApplyTimeouts is job 2: per tenant it asks the service for the financial decisions past their 24 running hours and asks for
// the default of each, asks for the refunds past their 5 business days and asks for the overdue mark of each, and raises the alert
// of each proof past its 24 hour review. The service decides every one of them (HU-COM-016, HU-COM-020), so the job holds no rule.
type ApplyTimeouts struct {
	financial out.Financial
	alerts    out.Alerts
	tenants   []string
	log       *slog.Logger
	mu        sync.Mutex
	alerted   map[string]bool
}

var _ workerruntime.Job = (*ApplyTimeouts)(nil)

// NewApplyTimeouts wires the job over the tenants it sweeps.
func NewApplyTimeouts(financial out.Financial, alerts out.Alerts, tenants []string, log *slog.Logger) *ApplyTimeouts {
	return &ApplyTimeouts{financial: financial, alerts: alerts, tenants: tenants, log: log, alerted: map[string]bool{}}
}

// Name is how the runner logs the job.
func (*ApplyTimeouts) Name() string { return "financial-timeouts" }

// Run takes at most batch items from each list of each tenant. What is defaulted or marked leaves its list, so what does not fit
// waits for the next run. A list that fails counts once and the other lists and tenants are still swept.
func (j *ApplyTimeouts) Run(ctx context.Context, batch int) (processed, failed int) {
	limit := min(batch, maxPageSize)
	for _, tenant := range j.tenants {
		sweeps := []func() (int, int){
			func() (int, int) { return j.defaults(ctx, tenant, limit) },
			func() (int, int) { return j.marks(ctx, tenant, limit) },
			func() (int, int) { return j.proofs(ctx, tenant, limit) },
		}
		for _, sweep := range sweeps {
			if ctx.Err() != nil {
				return processed, failed
			}
			done, bad := sweep()
			processed, failed = processed+done, failed+bad
		}
	}
	return processed, failed
}

func (j *ApplyTimeouts) defaults(ctx context.Context, tenant string, limit int) (int, int) {
	ids, err := j.financial.ListDueDecisions(ctx, tenant, limit)
	if err != nil {
		return j.listFailed(ctx, "decisions", tenant, err)
	}
	return workerruntime.Each(ctx, j.log, ids, func(ctx context.Context, id string) error {
		applied, err := j.financial.ApplyDefault(ctx, tenant, id)
		if err != nil {
			return fmt.Errorf("default the decision %s of tenant %s: %w", id, tenant, err)
		}
		j.log.InfoContext(ctx, "default decision asked", "tenantId", tenant, "financialDecisionId", id, "applied", applied)
		return nil
	})
}

func (j *ApplyTimeouts) marks(ctx context.Context, tenant string, limit int) (int, int) {
	ids, err := j.financial.ListDueRefunds(ctx, tenant, limit)
	if err != nil {
		return j.listFailed(ctx, "refunds", tenant, err)
	}
	return workerruntime.Each(ctx, j.log, ids, func(ctx context.Context, id string) error {
		marked, err := j.financial.MarkOverdue(ctx, tenant, id)
		if err != nil {
			return fmt.Errorf("mark the refund %s of tenant %s: %w", id, tenant, err)
		}
		j.log.InfoContext(ctx, "overdue mark asked", "tenantId", tenant, "refundAuthorizationId", id, "marked", marked)
		return nil
	})
}

func (j *ApplyTimeouts) proofs(ctx context.Context, tenant string, limit int) (int, int) {
	proofs, err := j.financial.ListOverdueProofs(ctx, tenant, limit)
	if err != nil {
		return j.listFailed(ctx, "proofs", tenant, err)
	}
	var fresh []out.Proof
	for _, p := range proofs {
		if !j.wasAlerted(tenant, p) {
			fresh = append(fresh, p)
		}
	}
	return workerruntime.Each(ctx, j.log, fresh, func(ctx context.Context, p out.Proof) error { return j.alert(ctx, tenant, p) })
}

// The list repeats a proof until it is reviewed, so each one is alerted once while the worker lives; a failed alert is not
// remembered, and a restart may repeat one, so the receiver must be idempotent.
func (j *ApplyTimeouts) wasAlerted(tenant string, p out.Proof) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.alerted[tenant+"/"+p.PaymentID]
}

func (j *ApplyTimeouts) alert(ctx context.Context, tenant string, p out.Proof) error {
	key := tenant + "/" + p.PaymentID
	if err := j.alerts.ProofReviewOverdue(ctx, tenant, p.PaymentID); err != nil {
		return fmt.Errorf("alert the proof %s: %w", key, err)
	}
	j.mu.Lock()
	j.alerted[key] = true
	j.mu.Unlock()
	return nil
}

func (j *ApplyTimeouts) listFailed(ctx context.Context, what, tenant string, err error) (int, int) {
	j.log.WarnContext(ctx, "a list could not be read, the next run asks again", "list", what, "tenantId", tenant, "error", err)
	return 0, 1
}
