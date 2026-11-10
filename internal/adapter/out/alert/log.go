// Package alert is the output adapter of the overdue-proof alert. Until the notification piece exists it only writes a line.
package alert

import (
	"context"
	"log/slog"

	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/port/out"
)

// Log is the double of the alert: tenant and proof, never a name, an e-mail or an amount.
type Log struct{ log *slog.Logger }

var _ out.Alerts = (*Log)(nil)

// NewLog returns the double over a logger.
func NewLog(log *slog.Logger) *Log { return &Log{log: log} }

// ProofReviewOverdue writes that a proof passed its 24 hour review window, so an authorized user has to review it.
func (l *Log) ProofReviewOverdue(ctx context.Context, tenantID, paymentID string) error {
	l.log.WarnContext(ctx, "alert: payment proof review overdue", "tenantId", tenantID, "paymentId", paymentID)
	return nil
}
