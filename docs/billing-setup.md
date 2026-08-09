# billing-setup.md — Stripe / billing provisioning runbook

> This is the R-BG runbook. R-BG's committed reconciliation note reads: *"Completed
> with zero scope: Stripe vendor provisioning deferred to the settlement re-plan
> (founder billing→settlement deferral). Setup steps documented in
> docs/billing-setup.md."* Doing what's written below **is** R-BG's deferred scope.
> Full billing/settlement construction (a real Stripe client, real billing-state
> persistence, the webhook handler) returns with the settlement re-plan — this doc
> only gets you to "provisioned and ready," not "live."

Referenced by `merchantgateway.notConfiguredDetail`
(`server/internal/resourceaccess/merchantgateway/merchantgatewayaccess.go`): every
`ChargeCustomer` / `ValidateStoredInstrument` call fails today with
`"merchantgateway: Stripe not configured — see docs/billing-setup.md"`. If you landed
here from that error, jump to [What's missing to go live](#whats-missing-to-go-live).

## 1. What works today without Stripe

Truthfully, as of 2026-08-09:

- **billingManager** (`server/internal/manager/billing`) is real and wired: four
  Temporal workflows — `OnboardPaymentIntegration`, `RegisterCustomer`,
  `CloseBillingCycle`, `RunShortfallSweep` — plus the `RecordInboundRevenue` /
  `RecordRevenueReversal` signals, the per-customer `closeBillingCycle:<id>` Schedule,
  the hourly `shortfallSweep` Schedule, and the head-state Conflict re-read/re-apply
  loop. All of this dispatches correctly.
- **billingEngine** (`server/internal/engine/billing`) is real: a pure, deterministic
  Engine that folds revenue + usage into a signed net and states a routing directive.
  Charge-only: a non-negative net always routes `NoAction` — the platform never pays
  out, only charges shortfalls.
- **billingStateAccess** (`server/internal/resourceaccess/billingstate`) is a real,
  contract-backed **component** (wired via `hooks.FinalizeBillingStateAccess`, which
  is identity), but its head-state ops — `ReadBilling`, `RegisterCustomer`,
  `BindGatewayLive`, `SettleCycle`, `ResettleCycle`,
  `ReadPersistentlyDelinquentCustomers` — are still the **generated, unimplemented
  stub** (`contract.gen.go`'s `stubBillingStateAccess`): every call returns
  `fwra.Unknown, "not implemented"`. There is no pgx/Postgres backing yet. This is a
  **separate gap from Stripe** — even with Stripe fully provisioned, no billing
  workflow can complete a real run until this component gets a real persistence
  implementation (or an honest not-configured wrapper, mirroring what
  `merchantGatewayAccess` already has).
- **revenueLedgerAccess** (folded into the same `billingstate` package,
  `revenueledger.go`) is a **documented, permanent no-op** — not a stub waiting to be
  filled in. Per the charge-only ruling (2026-07-03, R-013), the platform never
  invoices for or shares in end-user revenue, so `RecordInboundRevenue` /
  `RecordReversal` drop the fact (return a stub ref) and `ReadRange` always returns
  nothing. `FinalizeRevenueLedgerAccess` is identity over this no-op.
- **merchantGatewayAccess** (`server/internal/resourceaccess/merchantgateway`) is a
  real, wired component whose one remaining dependency is a live Stripe credential.
  `hooks.FinalizeMerchantGatewayAccess` swaps the generated not-implemented stub for
  `notConfiguredMerchantGatewayAccess`, which fails **both** ops
  (`ChargeCustomer`, `ValidateStoredInstrument`) terminally with `fwra.Auth` and the
  message above — no wasted retry budget (`Auth` is in
  `gatewayActivityOptions().TerminalRA`, `server/internal/manager/billing/billingmanager.go`),
  and the message names this doc.

Net effect: `OnboardPaymentIntegration` fails at `BindGatewayLive` (the
`billingStateAccess` stub) before it would even reach Stripe; `RegisterCustomer`
fails at `ValidateStoredInstrument` (the not-configured gateway). Provisioning Stripe
alone (this doc) does not make a billing workflow complete end-to-end — see
[What's missing to go live](#whats-missing-to-go-live).

## 2. The billing model: ordinary Stripe vendor account, NOT Connect

Per the 2026-06-09 merchant-of-record reversal (confirmed current in
`R-BG.md`, `products/archistrator/helm/archistrator-server/values.yaml`): aiarch
charges **its own customers** for **its own service invoices** (tokens + hosting) as
an **ordinary Stripe account**.

- **No Stripe Connect.** No platform account, no connected accounts, no
  end-user KYC/onboarding flow. That entire surface was deleted in the reversal.
- **No merchant-of-record.** aiarch is not settling money on behalf of end users or
  taking a revenue share (`revenueLedgerAccess` is the no-op that enforces this at
  the code level).
- The only money movement is aiarch **charging its own customer** (positive net →
  `NoAction`; negative net → `ChargeCustomer`) and, at registration, a **zero-amount
  auth** on the stored payment instrument (`ValidateStoredInstrument`).

If you've seen "Stripe Connect" mentioned elsewhere for this project, treat this doc
and `R-BG.md` as authoritative — Connect was explicitly rejected.

## 3. Provisioning steps (do this in TEST MODE first)

Test mode needs no business activation and no bank account, and charges nothing
real; the env wiring is identical to live mode.

1. **Create (or sign in to) the Stripe account** at https://dashboard.stripe.com — a
   plain standard account for the aiarch/mixofreality business. Skip the "activate
   your account" business-details flow for now (test mode works without it).
   **Do not enable Connect or add any platform/connected-account features.**
2. **Create a restricted API key (test mode):** Dashboard → Developers → API keys →
   "Create restricted key", name it `archistrator-server`. Scope exactly:
   - Customers: **Write**
   - PaymentIntents: **Write**
   - SetupIntents: **Write**
   - Payment Methods: **Read**
   - everything else: **None**

   Copy the `rk_test_...` value. (A restricted key, not the full `sk_test_...`
   secret — least privilege for what the contract actually needs.)
3. **Create the webhook endpoint (test mode):** Dashboard → Developers → Webhooks →
   "Add endpoint", URL:

       https://archistrator.capture-gtd.com/api/v1/billing/stripe/webhook

   Events: `payment_intent.succeeded`, `payment_intent.payment_failed`,
   `setup_intent.succeeded`, `setup_intent.setup_failed`. Copy the signing secret
   `whsec_...`. (No handler exists there yet — see
   [What's missing to go live](#whats-missing-to-go-live) — so the endpoint will show
   failed deliveries until then. Harmless in test mode; Stripe just retries.)
4. **Create the cluster secret** (needs cluster write access, which the construction
   agent's kubeconfig does not have — this step is human-only):

       kubectl create secret generic archistrator-stripe-secret \
         --namespace archistrator \
         --from-literal=apiKey=rk_test_... \
         --from-literal=webhookSecret=whsec_...

   Leave `stripe.enabled: false` in `values.yaml` until the component that consumes
   these vars actually lands — the secret can sit in the cluster unreferenced, and
   the env refs are `optional: true` either way (see below).

### Going live later

When billing construction actually lands (C-BG/C-BM, post settlement re-plan):

1. Complete Stripe account **activation** (business details + bank account —
   ordinary vendor KYB for aiarch itself, not Connect onboarding).
2. Repeat steps 2–3 above in **live mode** (dashboard toggle): a live restricted key
   `rk_live_...` with the same 4-permission scope, and a live-mode webhook endpoint
   at the same URL (its signing secret differs from test mode's).
3. Rotate the secret in place and restart (env-from-secret is read at pod start):

       kubectl delete secret archistrator-stripe-secret -n archistrator
       kubectl create secret generic archistrator-stripe-secret \
         --namespace archistrator \
         --from-literal=apiKey=rk_live_... \
         --from-literal=webhookSecret=whsec_... \
       && kubectl rollout restart deployment archistrator-server -n archistrator

4. Don't create the live webhook until the bypass route + handler exist (below) —
   live mode disables endpoints that fail for several consecutive days and emails
   the account owner.

## 4. Every envvar/secret: name, reader, deployment landing spot

| Env var | Read by | Status today |
|---|---|---|
| `ARCHISTRATOR_STRIPE_API_KEY` | `merchantGatewayAccess` (once the real Stripe client is built) | Forward-wired in the helm chart, **not yet read** by `server/cmd/server/config.gen.go` |
| `ARCHISTRATOR_STRIPE_WEBHOOK_SECRET` | The billing Stripe-webhook handler (not built yet — see below) | Forward-wired in the helm chart, **not yet read** anywhere |

These two names are **not invented by this doc** — they already exist, forward-wired
but dormant, in `products/archistrator/helm/archistrator-server`:

- `values.yaml` (lines ~216–234): a `stripe:` block — `enabled: false` (gates the env
  wiring entirely; there's no non-secret public id to gate on, unlike `github.appId`)
  and `secret: {name: archistrator-stripe-secret, apiKeyKey: apiKey,
  webhookSecretKey: webhookSecret}`.
- `templates/deployment.yaml` (lines ~191–211): behind `{{- if .Values.stripe.enabled
  }}`, emits `ARCHISTRATOR_STRIPE_API_KEY` and `ARCHISTRATOR_STRIPE_WEBHOOK_SECRET`
  from `secretKeyRef`s into `archistrator-stripe-secret`, both `optional: true`. The
  template comment is explicit: *"config.go does NOT read ARCHISTRATOR_STRIPE_* yet.
  This is FORWARD ... wiring."* Confirmed still true — `config.gen.go` currently has
  zero `Stripe`/`Billing`/`Merchant` fields.
- `secrets.yaml.example`: documents the out-of-band `kubectl create secret` command
  and the restricted-key scope (same convention as
  `archistrator-anthropic-secret` / `archistrator-github-app-secret` — no secret is
  ever rendered by the chart itself).

**A client-side publishable key is not currently needed.** The frozen
`merchantGatewayAccess` contract has exactly two server-side ops,
`ChargeCustomer(customerID, amount, idempotencyKey)` and
`ValidateStoredInstrument(customerID, idempotencyKey)` — both driven from the
Temporal workflow, both authenticated with the restricted secret key. There is no
card-collection flow in `webApp` today (`webApp/src/routes/Billing.tsx` is a
human-gated placeholder screen with no Stripe Elements/SetupIntent client code). If a
future construction wave adds client-side card collection, a publishable key
(`ARCHISTRATOR_STRIPE_PUBLISHABLE_KEY` or similar, non-secret) would be added then,
following the same `ARCHISTRATOR_*` convention — don't provision or wire one now.

### Local dev (`server/.env`)

`server/.env` is gitignored — these are instructions, not values, and nothing below
is consumed by the server yet (see the table above):

1. Once `config.gen.go` is extended to read them (a construction task, not part of
   this doc), add to `server/.env`:

       ARCHISTRATOR_STRIPE_API_KEY=rk_test_...
       ARCHISTRATOR_STRIPE_WEBHOOK_SECRET=whsec_...

2. Use TEST MODE values from the steps above. Never put a live key in `server/.env`.
3. Until step 1 lands, setting these locally is a no-op — the composition root still
   builds `notConfiguredMerchantGatewayAccess` unconditionally
   (`cmd/server/hooks.go` `FinalizeMerchantGatewayAccess` has no cfg-gated branch —
   see next section).

## 5. What's missing to go live

Provisioning Stripe (sections 3–4) is necessary but not sufficient. In order, what
still has to be built (all deferred to the settlement re-plan per the founder's
billing→settlement deferral):

1. **Real `billingStateAccess` persistence.** Today it's the generated
   not-implemented stub for every op — no database backing at all. This blocks every
   billing workflow regardless of Stripe. Needs a real (pgx-backed, most likely)
   implementation, or at minimum an honest not-configured wrapper like
   `merchantGatewayAccess` has today, so failures are diagnosable instead of a bare
   `"not implemented"`.
2. **A real `merchantGatewayAccess` Stripe client**, replacing
   `notConfiguredMerchantGatewayAccess`, implementing `ChargeCustomer` and
   `ValidateStoredInstrument` against the real Stripe API using
   `ARCHISTRATOR_STRIPE_API_KEY`.
3. **`config.gen.go` gets the two Stripe fields** (a `configgen` regen), and
   `cmd/server/hooks.go`'s `FinalizeMerchantGatewayAccess` gets a real cfg-gated
   branch (real client when configured, `notConfiguredMerchantGatewayAccess`
   otherwise) instead of the current unconditional not-configured return.
4. **Flip `stripe.enabled: true`** in `values.yaml` in the same change.
5. **The instrument-binding seam needs a real gateway ref, not a placeholder.**
   Today, `OnboardWorkflow.bindGatewayLive`
   (`server/internal/manager/billing/onboard.go`, `gatewayBindingFor`) sets
   `GatewayBinding.GatewayCustomerRef` to the customer's own UUID string — there is
   no call to Stripe at binding time at all. When the real gateway lands, instrument
   binding must actually **mint a Stripe Customer + SetupIntent at binding time** and
   return the real gateway customer reference. The current two-op contract
   (`ChargeCustomer` / `ValidateStoredInstrument`) has no op that returns a gateway
   ref, so this is a **contract change**, not just an implementation swap — e.g.
   evolving `ValidateStoredInstrument` to return a `GatewayBinding`, or adding a new
   `BindInstrument` op. Earmarked, not yet designed.
6. **The webhook handler.** `POST /api/v1/billing/stripe/webhook`, verifying
   `Stripe-Signature` against `ARCHISTRATOR_STRIPE_WEBHOOK_SECRET`, then delivering
   `RecordInboundRevenue` / `RecordRevenueReversal` to `billingManager`. The `/api`
   HTTPRoute carries Envoy's OIDC/JWT `SecurityPolicy`; Stripe authenticates by
   signature, not a Keycloak bearer token, so a **more-specific bypass HTTPRoute**
   (path `/api/v1/billing/stripe/webhook`, no `SecurityPolicy` attachment — same
   mechanism as the `/healthz`/`/readyz` bypass routes) has to ship alongside the
   handler.
7. **Sweep the dormant `settlement` package** (the sunk MoR-era design stub still
   referencing a `settle:{customerId}:{cycleId}` Stripe idempotency key) when C-BM
   replaces it.

## 6. Pointer: this is a deferred-scope doc, not a construction doc

Full billing/settlement construction — the items in section 5 — returns with the
**settlement re-plan** (the founder's billing→settlement deferral). This doc's job
was narrower: make the vendor-account provisioning runbook exist so R-BG could close
with zero further scope, and give the next construction wave (C-BG/C-BM) a correct,
truthful starting point instead of rediscovering the gaps above from scratch. See the
earmark ledger in `docs/bugs/2026-08-09-construction-open-items.md` for the tracked
reopen-seam / settlement-re-plan items this doc is a dependency of.
