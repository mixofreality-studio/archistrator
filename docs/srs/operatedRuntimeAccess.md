# Service Requirement Specification — `operatedRuntimeAccess`

**Component:** `operatedRuntimeAccess`
**Layer:** ResourceAccess
**Stereotype:** «ResourceAccess»
**Contract revision:** r4 (2026-06-07, FROZEN surface from r2)

---

## 1. Purpose

`operatedRuntimeAccess` is the sole gateway between archistrator and the operated-runtime infrastructure. It MUST own and isolate the entire surface through which the system writes desired state to the GitOps manifests repository and reads runtime health, SLO, and compute-attribution data back from the observability stack. No Manager or higher layer may reference the underlying infrastructure (git repo, ArgoCD, Argo Rollouts, Prometheus) directly; all interaction MUST flow through this component.

---

## 2. Operations

### 2.1 `publishDesiredState(appId, desiredState, idempotencyKey) → void`

The component MUST accept an infrastructure-neutral desired-state declaration and commit it to the deterministic manifest path for the identified app. Acceptance is durable (persisted to the manifests repository) before the operation returns; the component MUST NOT wait for convergence. This single operation absorbs all desired-state write shapes: initial deploy, autoscale-revision republish, idle-pause patch, delinquency patch, and payment-config patch. Writes with identical content to an existing manifest MUST be naturally idempotent (no new git revision). The caller-supplied `idempotencyKey` MUST be recorded in the git commit message.

### 2.2 `withdraw(appId, idempotencyKey) → void`

The component MUST remove the app's desired-state declaration from the manifests repository. The downstream GitOps reconciler (ArgoCD) will prune the live resources asynchronously; this component's responsibility ends at durable removal of the manifest. A `NotFound` condition on withdrawal (the app was never written, or was already removed) MUST be mapped to success — already-gone is semantically equivalent to withdrawn.

### 2.3 `readRuntimeStatus(appIds) → map[AppId]RuntimeStatus`

The component MUST return a batched, point-in-time snapshot of health and SLO state for every supplied app ID in a single call. This operation collapses what would otherwise be per-app health and SLO round-trips into one. The result is a map keyed by `AppId`; an app that cannot be found MUST surface as an absent entry or as `StatusUnknown` in the map, not as a whole-call error. The operation MUST carry no side effects.

### 2.4 `readComputeAttribution(appIds, window) → map[AppId]ComputeAttribution`

The component MUST return per-app infrastructure-neutral compute consumption over a closed time window. Attribution MUST be expressed in provider-neutral `ComputeUnits` (a normalized float64); raw provider-opaque meter bytes MAY be included as an optional field. The component MUST NOT translate units into pricing, vCPU, GiB, or SKU — that translation belongs to higher layers. An app absent from the observability source MUST surface as an absent map entry, not as a whole-call error.

---

## 3. Encapsulated Volatility

The component absorbs the **OperatedRuntime volatility**, which has three faces:

1. **Convergence mechanism** — how desired state is reconciled with the live cluster (currently GitOps / ArgoCD). The Manager layer MUST remain unaware of whether reconciliation is git-push-based, direct API-based, or anything else.
2. **Compute-cost attribution source** — which observability system supplies metering data (currently Prometheus). Callers receive provider-neutral `ComputeUnits`; the specific pull mechanism is hidden.
3. **Runtime-status transport** — how health and SLO observations are surfaced (currently observability-stack pull). The transport protocol and scrape topology are invisible above this seam.

Additionally, the component absorbs the **ArtifactTarget volatility**: the manifests git repository target is profile-selected (cloud = Gitea, local = on-disk, enterprise = customer repository). The contract surface is identical regardless of which target is active.

---

## 4. Dependencies

### Inbound (callers)

| Caller | Layer |
|---|---|
| `operationsManager` | Manager |
| `settlementManager` | Manager |

Both callers wrap Activity-stereotype operations in Temporal activities; the component itself is a plain Go method set.

### Outbound (resources owned)

| Resource | How |
|---|---|
| `operatedRuntime` (GitOps repo + ArgoCD + Argo Rollouts + Prometheus) | Direct by value — plain Go methods; writes = git commit + push to deterministic manifest path; reads = observability stack pull |

---

## 5. Error Model

All errors MUST be typed as `fwra.Error` (framework-go ResourceAccess error). Retryability is seeded from `Kind`:

| Kind | Retryable | Notes |
|---|---|---|
| `Transient` | Yes | Transient infrastructure glitch |
| `Conflict` | Yes | Concurrent desired-state write to the same manifest path |
| `Infrastructure` | Yes | Underlying git/observability infrastructure failure |
| `Auth` | No (terminal) | Infrastructure token expired or invalid |
| `NotFound` | No (terminal) — **except** on `withdraw`, where it MUST map to success | Unknown `appId` on read |
| `ContractMisuse` | No (terminal) | Empty `appId` or `idempotencyKey`, malformed `AttributionWindow` |

---

## 6. Idempotency

**Writes** (`publishDesiredState`, `withdraw`): identical content written to a deterministic manifest path produces no new git revision. The caller-supplied `fwra.IdempotencyKey` (derived by the Manager from `${workflowId}:${activityId}`) is embedded in the commit message to make intent traceable.

**Reads** (`readRuntimeStatus`, `readComputeAttribution`): pure reads with no side effects; no idempotency key is required or accepted.
