package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	financialhttp "github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/adapter/out/http"
)

const (
	tenant   = "11111111-1111-4111-8111-111111111111"
	decision = "55555555-5555-4555-8555-555555555555"
	refund   = "66666666-6666-4666-8666-666666666666"
	payment  = "77777777-7777-4777-8777-777777777777"
	token    = "service-token-value"
	base     = "/api/v1/commercial-reservation/internal/tenants/" + tenant
)

type seen struct {
	method, path, query, auth, correlation, key string
}

// stub is commercial-reservation: it records each request and answers with the next scripted reply.
type stub struct {
	mu      sync.Mutex
	seen    []seen
	replies []func(w http.ResponseWriter)
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"),
		r.Header.Get(correlation.Header), r.Header.Get("Idempotency-Key")})
	reply := s.replies[min(len(s.seen), len(s.replies))-1]
	s.mu.Unlock()
	reply(w)
}

func write(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func adapter(t *testing.T, replies ...func(http.ResponseWriter)) (*financialhttp.Financial, *stub) {
	t.Helper()
	s := &stub{replies: replies}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	client := httpclient.New(srv.URL, config.Secret(token), httpclient.WithAttempts(3),
		httpclient.WithSleeper(func(context.Context, time.Duration) error { return nil }))
	return financialhttp.NewFinancial(client), s
}

func run() context.Context { return correlation.WithID(context.Background(), "run-0000-aaaa") }

const meta = `"meta":{"page":1,"limit":100,"total":1,"totalPages":1}`

func credentials(t *testing.T, r seen) {
	t.Helper()
	if r.auth != "Bearer "+token || r.correlation != "run-0000-aaaa" {
		t.Errorf("credentials = %q, correlation %q", r.auth, r.correlation)
	}
}

// HU-WRK-002 AC1: it reads the due decisions of the tenant of the path, and asks for the default of each.
func TestDecisions_are_listed_due_and_defaulted_with_the_token_and_the_correlation_id(t *testing.T) {
	a, s := adapter(t, write(200, `{"data":[{"financialDecisionId":"`+decision+`","kind":"CUSTOMER_CANCELLATION"}],`+meta+`}`),
		write(200, `{"refundAuthorizationId":"`+refund+`","authorizationMode":"AUTOMATIC"}`))

	ids, err := a.ListDueDecisions(run(), tenant, 50)
	applied, err2 := a.ApplyDefault(run(), tenant, decision)

	if err != nil || err2 != nil || len(ids) != 1 || ids[0] != decision || !applied {
		t.Fatalf("got %v %v, %v %v", ids, err, applied, err2)
	}
	if r := s.seen[0]; r.method != "GET" || r.path != base+"/financial-decisions" || r.query != "due=true&page=1&limit=50" {
		t.Errorf("list = %+v", r)
	}
	if r := s.seen[1]; r.method != "POST" || r.path != base+"/financial-decisions/"+decision+"/default" || r.key == "" {
		t.Errorf("default = %+v", r)
	}
	credentials(t, s.seen[0])
	credentials(t, s.seen[1])
}

// HU-WRK-002 AC2: it reads the refunds whose mark is due and marks each one.
func TestRefunds_are_listed_due_and_marked_overdue(t *testing.T) {
	a, s := adapter(t, write(200, `{"data":[{"refundAuthorizationId":"`+refund+`","overdue":false}],`+meta+`}`),
		write(200, `{"refundAuthorizationId":"`+refund+`","overdue":true}`))

	ids, err := a.ListDueRefunds(run(), tenant, 100)
	marked, err2 := a.MarkOverdue(run(), tenant, refund)

	if err != nil || err2 != nil || len(ids) != 1 || ids[0] != refund || !marked {
		t.Fatalf("got %v %v, %v %v", ids, err, marked, err2)
	}
	if r := s.seen[0]; r.path != base+"/refund-authorizations" || r.query != "due=true&page=1&limit=100" {
		t.Errorf("list = %+v", r)
	}
	if r := s.seen[1]; r.method != "POST" || r.path != base+"/refund-authorizations/"+refund+"/overdue-mark" || r.key == "" {
		t.Errorf("mark = %+v", r)
	}
	credentials(t, s.seen[1])
}

// HU-WRK-002 AC2: the proofs past 24 hours come from the list of the service; the job never writes on a proof.
func TestProofs_are_listed_overdue_and_only_read(t *testing.T) {
	a, s := adapter(t, write(200, `{"data":[{"paymentId":"`+payment+`","validationStatus":"IN_VALIDATION","reviewOverdue":true}],`+meta+`,"overdueCount":1}`))

	got, err := a.ListOverdueProofs(run(), tenant, 20)

	if err != nil || len(got) != 1 || got[0].PaymentID != payment {
		t.Fatalf("got %+v, %v", got, err)
	}
	if r := s.seen[0]; r.method != "GET" || r.path != base+"/payment-proofs" || r.query != "overdue=true&page=1&limit=20" {
		t.Errorf("list = %+v", r)
	}
	credentials(t, s.seen[0])
}

// The service decides at the call: a 422 (not due, frozen, already decided, executed) is "not applied", never a failure.
func TestAnswer_422_means_not_applied(t *testing.T) {
	a, s := adapter(t, write(422, `{"error":"BUSINESS_RULE_VIOLATION","message":"frozen","traceId":"t1"}`))

	applied, err := a.ApplyDefault(run(), tenant, decision)
	marked, err2 := a.MarkOverdue(run(), tenant, refund)

	if applied || marked || err != nil || err2 != nil || len(s.seen) != 2 {
		t.Fatalf("got %v %v, %v %v after %d calls", applied, err, marked, err2, len(s.seen))
	}
}

// Annex D: 5xx and 429 are retried up to the limit with the same key; any other 4xx is not.
func TestPosts_retry_a_5xx_and_a_429_but_no_other_4xx(t *testing.T) {
	a, s := adapter(t, write(503, `{"error":"UNAVAILABLE","message":"x","traceId":"t1"}`),
		write(429, `{"error":"TOO_MANY","message":"x","traceId":"t2"}`), write(200, `{}`))

	applied, err := a.ApplyDefault(run(), tenant, decision)

	if err != nil || !applied || len(s.seen) != 3 || s.seen[0].key == "" || s.seen[0].key != s.seen[2].key {
		t.Fatalf("applied %v err %v after %d calls, keys %q %q", applied, err, len(s.seen), s.seen[0].key, s.seen[len(s.seen)-1].key)
	}
	for _, status := range []int{401, 403, 404} {
		a, s := adapter(t, write(status, `{"error":"X","message":"no","traceId":"t"}`))
		if _, err := a.MarkOverdue(run(), tenant, refund); err == nil || len(s.seen) != 1 {
			t.Errorf("%d: error %v after %d calls, want one call and a failure", status, err, len(s.seen))
		}
	}
}

// An id that is not a UUID never reaches a URL path.
func TestAdapter_refuses_an_id_that_is_not_a_uuid_before_calling(t *testing.T) {
	a, s := adapter(t, write(200, `{}`))

	_, err1 := a.ApplyDefault(run(), tenant, "../x")
	_, err2 := a.MarkOverdue(run(), "acme", refund)
	_, err3 := a.ListDueDecisions(run(), tenant, 0)

	if err1 == nil || err2 == nil || err3 == nil || len(s.seen) != 0 {
		t.Errorf("errors %v %v %v, calls %d, want three refusals and no call", err1, err2, err3, len(s.seen))
	}
}

// A malformed 2xx is an error that does not echo the body.
func TestLists_a_body_that_is_not_the_contract_is_an_error(t *testing.T) {
	a, _ := adapter(t, write(200, `{"data":[{"financialDecisionId":"nope"}],"meta":{}}`))

	_, err := a.ListDueDecisions(run(), tenant, 10)

	if err == nil || strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want a refusal that does not echo the body", err)
	}
}
