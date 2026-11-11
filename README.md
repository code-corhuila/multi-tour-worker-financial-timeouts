# multi-tour-worker-financial-timeouts

> Worker job 2: financial timeouts (commercial-reservation)

Part of the **Multi Tour** distributed system — team `multi-tour`, Grupo 2.
Governance and documentation live in [`multi-tour-docs`](https://github.com/code-corhuila/multi-tour-docs).

## What it is

Job 2 of ADR-004 D3 (HU-WRK-002). Every 5 minutes, for each tenant it sweeps, it asks `commercial-reservation` through its internal routes for: the financial
decisions past their 24 running hours and asks for the default 100 % refund of each (`.../financial-decisions?due=true`, `POST .../default`); the refunds
past their 5 business days and asks for the overdue mark of each (`.../refund-authorizations?due=true`, `POST .../overdue-mark`); and the proofs not reviewed
in 24 hours (`.../payment-proofs?overdue=true`), for which it raises an alert and never reviews. The service decides every one at the call, including that it
does nothing while a tenant is inactive (HU-COM-016, HU-COM-020), so the worker holds no rule, has no database and exposes only `/health`.

## How to start it

Copy `.env.example`, fill `SERVICE_TOKEN` (never versioned) and, optionally, `TENANT_IDS`, and run `docker compose -f deploy/compose.yml up`. The configuration is read at
start and a bad variable stops the worker naming it.

## How to test it

`gofmt -l .` must print nothing, then `go vet ./...` and `go test ./...` (Go 1.26). The same run in `ci.yml` on pull requests.

## What is missing

- The tenants: when `TENANT_IDS` is empty each run asks commercial-reservation for the active tenants (`GET /internal/tenants?status=ACTIVE`, the `SERVICE` token). `TENANT_IDS` stays as the fallback and wins when set (declared debt until the identity-audit route exists, HU-WRK-001 DP-08).
- The overdue-proof alert is a logging double until the notification piece exists. The proof list repeats a proof until it is reviewed, and each list is read
  one page per run (`BATCH_SIZE`), so a page full of already-alerted proofs hides newer ones.
- Tested against a stub of the contract, not against the running api.

## Branching

Three permanent branches. **None of them accepts a direct commit** — you enter through a child
branch and leave through a Pull Request.

```
develop  <--PR--  feat/... fix/... chore/...
qa       <--PR--  qa/...
main     <--PR--  release/...  hotfix/...
```

Promotion happens **by re-application** (`git cherry-pick -x`), never by merging one permanent
branch into another: `merge develop -> qa` and `merge qa -> main` do not exist in this model.

`main` requires **1 approval from `ariel5253`**. On `develop` and `qa` the team sets its own review
rule.

Full policy: `00-governance/branching-policy.md` in `multi-tour-docs`.
