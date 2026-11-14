package financialtimeouts_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"
	"github.com/code-corhuila/multi-tour-worker-runtime/runner"

	alertlog "github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/adapter/out/alert"
	financialhttp "github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/usecase"
)

// service is commercial-reservation as the worker meets it over HTTP: one list per route and the status it answers to each action.
type service struct {
	mu      sync.Mutex
	lists   map[string]string // route suffix -> items of the list
	status  map[string]int    // id -> status of the action on it (0 is 200)
	actions []string          // METHOD path of every non-GET call
	bodies  []string
}

func (s *service) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		for suffix, items := range s.lists {
			if strings.Contains(r.URL.Path, suffix) {
				n := strconv.Itoa(strings.Count(items, "{"))
				_, _ = w.Write([]byte(`{"data":[` + items + `],"meta":{"page":1,"limit":100,"total":` + n + `,"totalPages":1}}`))
				return
			}
		}
		_, _ = w.Write([]byte(`{"data":[],"meta":{"page":1,"limit":100,"total":0,"totalPages":0}}`))
		return
	}
	b, _ := io.ReadAll(r.Body)
	s.actions = append(s.actions, r.Method+" "+strings.TrimPrefix(r.URL.Path, base))
	s.bodies = append(s.bodies, string(b))
	for id, status := range s.status {
		if strings.Contains(r.URL.Path, id) && status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"NOT_APPLICABLE","message":"x","traceId":"t-1"}`))
			return
		}
	}
	_, _ = w.Write([]byte(`{}`))
}

func (s *service) runner(t *testing.T, logs io.Writer, extra ...runner.Options) *runner.Runner {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(api.Close)
	log := logging.New(logs)
	client := httpclient.New(api.URL, runtimeconfig.Secret(token), httpclient.WithAttempts(1))
	job := usecase.NewApplyTimeouts(financialhttp.NewFinancial(client), alertlog.NewLog(log), []string{tenant}, log)
	opts := runner.Options{Interval: time.Hour, BatchSize: 100, RunTimeout: 10 * time.Second, Logger: log}
	if len(extra) > 0 {
		opts.Interval, opts.Ticks = extra[0].Interval, extra[0].Ticks
	}
	r, err := runner.New(job, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// HU-WRK-002 AC1 and AC2 (first half): the 24 hours of a decision and the 5 business days of a refund are the service's (spec 016 AC-16.8,
// AC-16.11). The worker holds no clock or calendar: it acts on every item the list names whatever dates the item carries and sends no date.
func TestHU_WRK_002_AC2_the_business_days_and_the_24_hours_are_the_services_the_worker_sends_no_date(t *testing.T) {
	s := &service{lists: map[string]string{
		"/financial-decisions":   `{"financialDecisionId":"` + decision + `","resumedDueAt":"2099-01-01T00:00:00Z"}`,
		"/refund-authorizations": `{"refundAuthorizationId":"` + refund + `","executionDueAt":"2099-01-01T00:00:00Z","status":"AUTHORIZED"}`,
	}}

	res := s.runner(t, io.Discard).RunOnce(t.Context())

	if len(s.actions) != 2 || s.actions[0] != "POST /financial-decisions/"+decision+"/default" ||
		s.actions[1] != "POST /refund-authorizations/"+refund+"/overdue-mark" {
		t.Fatalf("actions = %v, want the default and the overdue mark of what the lists name", s.actions)
	}
	for _, b := range s.bodies {
		if strings.TrimSpace(b) != "" {
			t.Errorf("an action carries a body %q: the worker must send no date, the service decides", b)
		}
	}
	if res.Processed != 2 || res.Failed != 0 {
		t.Errorf("processed %d failed %d, want 2 and 0", res.Processed, res.Failed)
	}
}

// HU-WRK-002 AC2 (second half, decision spec 010 DP-05): the "overdue" mark of a proof not reviewed in 24 hours is derived by the service when
// it reads the proof (reviewOverdue); it is not stored and no route sets it. So the job only READS the overdue list and raises the alert,
// once per proof however many runs pass, and never calls a write route on a proof (it neither approves, rejects nor marks).
func TestHU_WRK_002_AC2_an_overdue_proof_is_only_read_and_alerted_once_never_written(t *testing.T) {
	s := &service{lists: map[string]string{"/payment-proofs": `{"paymentId":"` + payment + `","reviewOverdue":true}`}}
	var logs bytes.Buffer
	r := s.runner(t, &logs)

	first := r.RunOnce(t.Context())
	second := r.RunOnce(t.Context())

	if len(s.actions) != 0 {
		t.Errorf("the job wrote to the service %v: a proof is only read, the mark is derived by the service", s.actions)
	}
	if n := strings.Count(logs.String(), "alert: payment proof review overdue"); n != 1 {
		t.Errorf("alerts = %d over two runs, want exactly 1", n)
	}
	if first.Failed != 0 || second.Failed != 0 {
		t.Errorf("failed %d then %d, want 0", first.Failed, second.Failed)
	}
}

// HU-WRK-002 AC3 (spec 016 AC-16.9, HU-COM-020): the freeze of the decisions is the service's. With the tenant inactive it lists no decision, so
// no default is asked; a decision frozen after it was listed is refused with a 422, which is an answer and not a failure.
func TestHU_WRK_002_AC3_a_frozen_decision_gets_no_default_and_no_failure(t *testing.T) {
	inactive := &service{}
	res := inactive.runner(t, io.Discard).RunOnce(t.Context())
	if len(inactive.actions) != 0 || res.Failed != 0 {
		t.Errorf("inactive tenant: actions %v failed %d, want none", inactive.actions, res.Failed)
	}

	frozen := &service{lists: map[string]string{"/financial-decisions": `{"financialDecisionId":"` + decision + `"}`},
		status: map[string]int{decision: http.StatusUnprocessableEntity}}
	res = frozen.runner(t, io.Discard).RunOnce(t.Context())
	if len(frozen.actions) != 1 || res.Failed != 0 || res.Processed != 1 {
		t.Errorf("frozen: actions %v processed %d failed %d, want one refused call and no failure", frozen.actions, res.Processed, res.Failed)
	}
}

// HU-WRK-002 D-1 (spec 016 RN-07): an overdue refund is NOT frozen by the inactivation. With no decision listed (frozen) the job still marks
// the refund that is past its 5 business days: the sweeps are independent.
func TestHU_WRK_002_D1_an_overdue_refund_is_marked_even_when_no_decision_is_listed(t *testing.T) {
	s := &service{lists: map[string]string{"/refund-authorizations": `{"refundAuthorizationId":"` + refund + `"}`}}

	res := s.runner(t, io.Discard).RunOnce(t.Context())

	if len(s.actions) != 1 || s.actions[0] != "POST /refund-authorizations/"+refund+"/overdue-mark" || res.Failed != 0 {
		t.Fatalf("actions %v failed %d, want the overdue mark of the refund", s.actions, res.Failed)
	}
}

// HU-WRK-002 D-2 (spec 016 AC-16.10): a credit balance request that waited 24 hours is one more item of the decision list: the job asks its
// default like any other and the service authorizes the available amount.
func TestHU_WRK_002_D2_a_credit_balance_request_is_defaulted_like_any_decision(t *testing.T) {
	s := &service{lists: map[string]string{"/financial-decisions": `{"financialDecisionId":"` + decision + `","kind":"CREDIT_BALANCE_REQUEST"}`}}

	res := s.runner(t, io.Discard).RunOnce(t.Context())

	if len(s.actions) != 1 || s.actions[0] != "POST /financial-decisions/"+decision+"/default" || res.Failed != 0 {
		t.Fatalf("actions %v failed %d, want the default of the request", s.actions, res.Failed)
	}
}

// Annex D: the log lines carry tenant and item and never the credential.
func TestHU_WRK_002_D7_log_lines_carry_the_ids_and_never_the_token(t *testing.T) {
	s := &service{lists: map[string]string{
		"/financial-decisions": `{"financialDecisionId":"` + decision + `"}`,
		"/payment-proofs":      `{"paymentId":"` + payment + `"}`,
	}}
	var logs bytes.Buffer

	s.runner(t, &logs).RunOnce(t.Context())

	out := logs.String()
	if strings.Contains(out, token) || strings.Contains(strings.ToLower(out), "bearer") {
		t.Errorf("the log carries the credential: %s", out)
	}
	for _, want := range []string{tenant, decision, payment} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not carry %q: %s", want, out)
		}
	}
}

// HU-WRK-002 D-6 (the story fixes no cadence, ADR-004 D10): each tick of the runner is one run that sweeps the three lists.
func TestHU_WRK_002_D6_each_interval_is_one_run(t *testing.T) {
	s := &service{lists: map[string]string{"/financial-decisions": `{"financialDecisionId":"` + decision + `"}`}}
	ticks := make(chan time.Time)
	r := s.runner(t, io.Discard, runner.Options{Interval: 5 * time.Minute, Ticks: ticks})
	done := make(chan struct{})
	ctx, stop := context.WithCancel(t.Context())
	go func() { r.Start(ctx); close(done) }()

	for i := 1; i <= 3; i++ {
		ok := false
		for attempt := 0; attempt < 5 && !ok; attempt++ {
			ticks <- time.Now()
			deadline := time.After(time.Second)
			for !ok {
				s.mu.Lock()
				n := len(s.actions)
				s.mu.Unlock()
				if n >= i {
					ok = true
					break
				}
				select {
				case <-deadline:
				case <-time.After(5 * time.Millisecond):
					continue
				}
				break
			}
		}
		if !ok {
			t.Fatalf("interval %d did not run the job", i)
		}
	}
	stop()
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.actions) != 3 {
		t.Errorf("defaults asked = %d, want one per interval (3)", len(s.actions))
	}
}
