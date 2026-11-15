package financialtimeouts_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"
	"github.com/code-corhuila/multi-tour-worker-runtime/runner"

	alertlog "github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/adapter/out/alert"
	financialhttp "github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-financial-timeouts/internal/application/usecase"
)

const (
	tenant   = "11111111-1111-4111-8111-111111111111"
	decision = "55555555-5555-4555-8555-555555555555"
	refund   = "66666666-6666-4666-8666-666666666666"
	payment  = "77777777-7777-4777-8777-777777777777"
	token    = "service-token-value"
	base     = "/api/v1/commercial-reservation/internal/tenants/" + tenant
)

const page = `"meta":{"page":1,"limit":100,"total":1,"totalPages":1}`

// HU-WRK-002 AC1, AC2: one run of the whole worker (runner, use case, client and alert) against a service with a due decision, a
// due refund and an overdue proof: it defaults the decision, marks the refund, only reads the proof, and every request carries the
// token and the correlation id of the run.
func TestHU_WRK_002_one_run_applies_the_three_timeouts_with_one_correlation_id(t *testing.T) {
	var mu sync.Mutex
	var calls, correlations []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		correlations = append(correlations, r.Header.Get("X-Correlation-Id"))
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET " + base + "/financial-decisions":
			_, _ = w.Write([]byte(`{"data":[{"financialDecisionId":"` + decision + `"}],` + page + `}`))
		case "POST " + base + "/financial-decisions/" + decision + "/default":
			_, _ = w.Write([]byte(`{"refundAuthorizationId":"` + refund + `"}`))
		case "GET " + base + "/refund-authorizations":
			_, _ = w.Write([]byte(`{"data":[{"refundAuthorizationId":"` + refund + `"}],` + page + `}`))
		case "POST " + base + "/refund-authorizations/" + refund + "/overdue-mark":
			_, _ = w.Write([]byte(`{"refundAuthorizationId":"` + refund + `","overdue":true}`))
		case "GET " + base + "/payment-proofs":
			_, _ = w.Write([]byte(`{"data":[{"paymentId":"` + payment + `"}],` + page + `,"overdueCount":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := httpclient.New(api.URL, runtimeconfig.Secret(token))
	job := usecase.NewApplyTimeouts(financialhttp.NewFinancial(client), alertlog.NewLog(log), []string{tenant}, log)
	r, err := runner.New(job, runner.Options{Interval: time.Hour, BatchSize: 100, RunTimeout: 10 * time.Second, Logger: log})
	if err != nil {
		t.Fatal(err)
	}

	res := r.RunOnce(t.Context())

	if res.Processed != 3 || res.Failed != 0 {
		t.Fatalf("processed %d failed %d, want 3 and 0", res.Processed, res.Failed)
	}
	if len(calls) != 5 {
		t.Fatalf("calls = %v, want three lists, a default and a mark", calls)
	}
	for _, c := range correlations {
		if c == "" || c != res.CorrelationID {
			t.Errorf("correlation %q, want the run's %q on every request", c, res.CorrelationID)
		}
	}
}

// Annex D: when the service fails, the run is counted as failed and the next run works.
func TestHU_WRK_002_a_failing_service_fails_the_run_and_the_next_one_recovers(t *testing.T) {
	var mu sync.Mutex
	down := true
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if down {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"INTERNAL_ERROR","message":"x","traceId":"t-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"meta":{"page":1,"limit":100,"total":0,"totalPages":0}}`))
	}))
	defer api.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := httpclient.New(api.URL, runtimeconfig.Secret(token), httpclient.WithAttempts(1))
	job := usecase.NewApplyTimeouts(financialhttp.NewFinancial(client), alertlog.NewLog(log), []string{tenant}, log)
	r, _ := runner.New(job, runner.Options{Interval: time.Hour, BatchSize: 100, RunTimeout: 10 * time.Second, Logger: log})

	bad := r.RunOnce(t.Context())
	mu.Lock()
	down = false
	mu.Unlock()
	good := r.RunOnce(t.Context())

	if bad.Failed != 3 || good.Failed != 0 {
		t.Errorf("failed %d then %d, want 3 (one per list) then 0", bad.Failed, good.Failed)
	}
}
