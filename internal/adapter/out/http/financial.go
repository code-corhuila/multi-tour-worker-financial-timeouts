// Package http is the output adapter that calls commercial-reservation on its internal routes.
package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/port/out"
)

const (
	tenantPath    = "/api/v1/commercial-reservation/internal/tenants/%s"
	decisionsPath = tenantPath + "/financial-decisions?due=true&page=1&limit=%d"
	defaultPath   = tenantPath + "/financial-decisions/%s/default"
	refundsPath   = tenantPath + "/refund-authorizations?due=true&page=1&limit=%d"
	markPath      = tenantPath + "/refund-authorizations/%s/overdue-mark"
	proofsPath    = tenantPath + "/payment-proofs?overdue=true&page=%d&limit=%d"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Financial calls commercial-reservation through the client of the runtime.
type Financial struct {
	client *httpclient.Client
}

var _ out.Financial = (*Financial)(nil)

// NewFinancial returns the adapter over a client whose base URL is commercial-reservation.
func NewFinancial(client *httpclient.Client) *Financial { return &Financial{client: client} }

type idItem struct {
	DecisionID string `json:"financialDecisionId"`
	RefundID   string `json:"refundAuthorizationId"`
	PaymentID  string `json:"paymentId"`
}

// ListDueDecisions reads the first page of GET .../financial-decisions?due=true.
func (f *Financial) ListDueDecisions(ctx context.Context, tenantID string, limit int) ([]string, error) {
	return f.ids(ctx, tenantID, limit, decisionsPath, func(i idItem) string { return i.DecisionID })
}

// ListDueRefunds reads the first page of GET .../refund-authorizations?due=true.
func (f *Financial) ListDueRefunds(ctx context.Context, tenantID string, limit int) ([]string, error) {
	return f.ids(ctx, tenantID, limit, refundsPath, func(i idItem) string { return i.RefundID })
}

// ListOverdueProofs reads one page of GET .../payment-proofs?overdue=true.
func (f *Financial) ListOverdueProofs(ctx context.Context, tenantID string, page, limit int) ([]out.Proof, error) {
	if page < 1 {
		return nil, errors.New("a positive page is required")
	}
	ids, err := f.idsAt(ctx, tenantID, limit, fmt.Sprintf(proofsPath, tenantID, page, limit), func(i idItem) string { return i.PaymentID })
	proofs := make([]out.Proof, 0, len(ids))
	for _, id := range ids {
		proofs = append(proofs, out.Proof{PaymentID: id})
	}
	return proofs, err
}

func (f *Financial) ids(ctx context.Context, tenantID string, limit int, path string, pick func(idItem) string) ([]string, error) {
	return f.idsAt(ctx, tenantID, limit, fmt.Sprintf(path, tenantID, limit), pick)
}

func (f *Financial) idsAt(ctx context.Context, tenantID string, limit int, url string, pick func(idItem) string) ([]string, error) {
	if !uuid.MatchString(tenantID) || limit < 1 {
		return nil, errors.New("a tenant uuid and a positive limit are required")
	}
	got, err := httpclient.List[idItem](ctx, f.client, url)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	ids := make([]string, 0, len(got.Data))
	for _, item := range got.Data {
		if id := pick(item); uuid.MatchString(id) {
			ids = append(ids, id)
		} else {
			return nil, errors.New("list: an item has no uuid")
		}
	}
	return ids, nil
}

// ApplyDefault asks for the default of a decision.
func (f *Financial) ApplyDefault(ctx context.Context, tenantID, decisionID string) (bool, error) {
	return f.act(ctx, tenantID, decisionID, defaultPath, "default-")
}

// MarkOverdue asks for the overdue mark of a refund.
func (f *Financial) MarkOverdue(ctx context.Context, tenantID, refundID string) (bool, error) {
	return f.act(ctx, tenantID, refundID, markPath, "overdue-mark-")
}

// act is a POST that changes nothing twice (the service answers a repeat with the stored result), so it carries a key that is the
// same for the same item and the runtime may repeat it after a network failure, a 429 or a 5xx. A 422 is the service saying that
// it is not due, frozen, already decided or executed: it is an answer, not a failure.
func (f *Financial) act(ctx context.Context, tenantID, id, path, keyPrefix string) (bool, error) {
	if !uuid.MatchString(tenantID) || !uuid.MatchString(id) {
		return false, errors.New("a tenant uuid and an item uuid are required")
	}
	err := f.client.Do(ctx, httpclient.Request{Method: http.MethodPost, Path: fmt.Sprintf(path, tenantID, id),
		IdempotencyKey: keyPrefix + id}, &struct{}{})
	var api *httpclient.APIError
	if errors.As(err, &api) && api.Status == http.StatusUnprocessableEntity {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ask the service: %w", err)
	}
	return true, nil
}
