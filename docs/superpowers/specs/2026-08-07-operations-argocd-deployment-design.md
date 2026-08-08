# Operations & Deployment — ArgoCD GitOps rail (archistrator dogfood)

**Date:** 2026-08-07
**Status:** design approved, pending implementation plan
**Scope:** make archistrator itself a deployable, operated app through its own operations rail — the N-DEP follow-up.

---

## 1. Problem

`operatedRuntimeAccess` ships a two-profile seam. The `Local` profile is a deterministic dry-run; the `Real` profile is a skeleton whose every verb returns an explicit diagnostic:

> `operatedruntime real profile: publishDesiredState requires the GitOps/kubernetes backend, which is not yet implemented (follow-up N-DEP, pairs with the Argo deployment work)`

Everything above that seam is built. `operationsManager.DeployWorkflow` already reads head-state, retrieves the deployable bundle, publishes desired state, and records the transition with a Conflict recovery loop. The Operations console already renders Deploy / Scale / Update-autoscaler / Withdraw against real routes. The missing piece is the substrate: nothing renders manifests and nothing commits them anywhere.

Three structural gaps block a first deploy, all three currently documented in-tree as follow-ups:

1. **Nothing renders desired state.** `DesiredStateChange.RenderedDesiredState` is a caller-supplied `[]byte` on the public façade, and the webApp never sends it (`useOperationsMutations.ts` omits the field). So the rail publishes empty bytes.
2. **Nothing can create an operated app.** `operatedSystemStateAccess` has six verbs — `PublishDesiredState`, `ReadInFlightOperatedApps`, `ReadOperatedSystem`, `RecordDelinquencyAction`, `RecordRuntimeStatusChange`, `WithdrawSystem` — and no create/seed verb. `schema.sql` states it plainly: *"the frozen §2 contract has NO write verb carrying a bundle ref, so this column has no writer here and stays `''`"*. `DeployWorkflow` then hard-fails `FailedPrecondition` when `DeployableBundleRef == ""`.
3. **The retrieved bundle is discarded.** `deploy.go:47` — `if _, berr := wf.retrieveBundle(ctx, op.DeployableBundleRef); berr != nil` — fetches the bundle and throws the result away.

Meanwhile the software repo (`../software`) runs a standard app-of-apps: `root` → `k8s/argocd/layers/*` → `k8s/argocd/applications/*.yaml`, every Application pinned to `repoURL: aiarchmultiplatform.git`, with `prune: true` and `selfHeal: true` throughout. Archistrator's own four charts (`archistrator-server`, `archistrator-webapp`, `archistrator-postgres`, `archistrator-gateway-routes`) live there hand-written, with the image tag bumped by hand (`0.8.16`, annotated *"promotion is currently MANUAL … decision pending"*).

---

## 2. Decisions

| # | Decision | Rejected alternatives |
|---|---|---|
| D1 | **The Argo `Application` lives in `../software`.** Non-negotiable: `root` only watches `aiarchmultiplatform.git`, and `layer-applications` only watches `k8s/argocd/applications/`. | — |
| D2 | **Archistrator renders workload manifests into `../software`.** The app repo holds code and produces a container image; it contributes no Kubernetes YAML. | *Chart in the app repo* (true multi-source) was rejected: it puts agent-authored manifests into the cluster, which needs an AppProject sandbox to be safe at all. *Multi-source Application* was rejected for the same reason plus added moving parts. |
| D3 | **Fully rendered plain manifests**, not helm values over a shared chart. What is in git is literally what runs. | *One Application with inline `valuesObject`* and *per-app `values.yaml`* both rejected in favour of maximum auditability. Accepted cost: a platform-level fix means re-rendering the fleet (see §5.4). |
| D4 | **`operatedRuntimeAccess` renders.** `RuntimeDesiredState` becomes a typed, substrate-neutral struct; the Real profile turns it into Kubernetes YAML and commits. Kubernetes vocabulary never appears above ResourceAccess — the same rule that keeps SQL inside a database RA. | *A pure render Engine* was rejected as substrate leakage above ResourceAccess. *Construction renders into the bundle* was rejected because it re-introduces agent-authored YAML, giving back exactly what D2 bought. |
| D5 | **Render inputs join three sources**: structure from the app's `project.json` deployment model, image tags from the release/bundle, operator knobs from head-state. Deployment stays a projection of the design. | *All-in-head-state* rejected — duplicates the design model and drifts silently. *All-in-bundle* rejected — operator knobs still need an overlay. |
| D6 | **First slice is archistrator-only.** Its images already exist in GHCR and its four hand-written charts become the golden-file target the renderer must reproduce. | *Dogfood + built-app onboarding* deferred: `method-assets/assets/scaffold/` emits no Dockerfile and no release workflow, so built apps have no image and nothing to deploy. That is its own spec. |
| D7 | **Health is real; SLO and cost attribution are not.** `getApplicationHealth` reads the Argo `Application` CR; `getSloStatus` returns unconfigured and `readComputeAttribution` returns empty, so the 30s Schedule stays green, billing receives nothing, and the autoscaler stays inert for want of a signal. | *All three real* deferred — needs a Prometheus client, per-app SLO definitions, and a defensible attribution formula feeding real money. |
| D8 | **Approval is the operator clicking Deploy**; the server commits directly to `../software` main. Archistrator's own Application is rendered with `prune: false` and manual sync, so self-modification always requires a human to press Sync in Argo. | *PR-and-merge* rejected: moves approval out of the product into GitHub and makes every deploy two-step. *Full auto with no self-guard* rejected: with `prune: true` a renderer bug that omits a manifest deletes the control plane, leaving no automated way back. |
| D9 | **The local profile does not surface operations at all** — routes unmounted, console hidden. | *Dry-run console visible locally* rejected by the founder: local must not appear to operate. |
| D10 | **Deployment diagram health is strict green / red / neutral.** `Healthy` → green; every other observed state (`Progressing`, `Degraded`, `Missing`, `Suspended`, `Unknown`) → red; anything outside the app's resource set → neutral. | *Amber for Progressing* rejected in favour of glance-readability. Accepted cost: the diagram reads red mid-rollout until it settles. |
| D11 | **The model maps to manifests by `role`, not by technology string.** The deployable elements are `infrastructureNodes` carrying machine-readable `role` values (`gateway`, `identityProvider`, `database`) plus workload nodes. Selection keys off those, never off the free-text `technology` field. | *Selecting by `technology`* rejected: it puts Kubernetes vocabulary in the Manager and couples assembly to an unconstrained free-text string (`"k8s"` vs `"k8s-namespace"` vs `"Kubernetes Deployment"`, none of them validated). |
| D12 | **The renderer emits a Keycloak realm CR per app.** Onboarding an app provisions its realm and OIDC client instead of requiring manual admin-console work. The cluster already runs the Keycloak operator (`keycloak-k8s-resources` 26.4.2, API group `k8s.keycloak.org/v2alpha1`). **Create-only — see §5.5.** | *Keeping Keycloak manual* rejected by the founder. *Reconciling the realm for real* (Admin API or a reconciling controller) rejected as materially larger scope that puts an automated writer on the path to the founder's own login. Accepted cost: this is the one rendered object with no production counterpart to diff against (§5.2). |

---

## 3. Architecture

```
operator clicks Deploy  (archistrator Operations console — cloud profile only)
        │
        ▼
operationsManager.DeployWorkflow                        [Manager — assembles]
   ├── projectStateAccess   → deployment model: containers, infra, host, ns, env   ← NEW edge
   ├── artifactAccess       → deployable bundle → image tags   (retrieveBundle finally consumed)
   └── operatedSystemState  → replicas, autoscaler policy, pinned
        │
        │  typed, substrate-neutral DesiredState
        ▼
operatedRuntimeAccess (Real profile)                    [ResourceAccess — renders + commits]
   ├── renders plain manifests      → k8s/argocd/apps/<app>/*.yaml
   ├── renders the Argo Application → k8s/argocd/applications/<app>.yaml
   └── git commit → ../software main   (content-idempotent)
        │
        ▼
ArgoCD
   ├── tenant apps        : auto-sync, prune, selfHeal
   └── archistrator itself: manual sync, prune: false
```

The Manager assembles inputs from three systems and hands the RA one typed value. No ResourceAccess calls another ResourceAccess; no layering rule is bent.

**Reconcile.** The existing 30s Temporal Schedule calls `getApplicationHealth`, which reads `status.health.status` and `status.sync.status` from the Argo `Application` CR via an in-cluster ServiceAccount with read access to `argocd`. No API token to mint or distribute — the server already runs in the cluster.

---

## 4. Contract changes

Three senior-frozen contracts change. Each needs a service-contract review pass.

### 4.1 `operatedRuntimeAccess`

`RuntimeDesiredState{Bytes []byte, ContentType string}` becomes a typed struct carrying what a render actually needs:

```
RuntimeDesiredState{
    AppName, Namespace, Host   string
    ModelKey    string                 // the cloud-environment node this app maps to
    Server      Workload{ModelKey, Image string, Replicas int64}
    WebApp      Workload{ModelKey, Image string, Replicas int64}
    Postgres    {ModelKey string, Enabled bool, Instances int64, StorageClass string}
    OIDC        {Issuer, ClientID, ClientSecretRef string}
    SelfManaged bool                   // archistrator itself → prune:false + manual sync
}
```

Two fields carry more weight than they look:

**`ModelKey` on every renderable element.** Each element names the deployment-model node it came from. This is what lets §6 re-derive a `model key → (kind, name, namespace)` map by re-rendering, with no annotations in the cluster and no separately-maintained mapping table that could drift from the renderer. Without it the renderer produces manifests it cannot attribute back to the diagram.

**`SelfManaged`.** This makes D8's self-guard a property of the rendered output rather than a convention someone remembers to follow.

### 4.2 `operatedSystemStateAccess`

New verb, closing the documented seeding gap:

```
RegisterOperatedSystem(rc, operatedAppID, customerID, projectRef, deployableBundleRef, idempotencyKey) (Version, error)
```

`projectRef` identifies the project whose `project.json` supplies the deployment model at render time (D5). It is a pointer, not a copy — the row records *which* design the deployment projects from, never a duplicated spec.

Takes the contract from 6 to 7 ops — well inside Appendix B limits. It writes the `deployable_bundle_ref` and `customer_id` columns that currently have no writer, which also un-blocks the customer-scoped delinquency sweep that returns empty today.

### 4.3 `operationsManager`

- `DesiredStateChange.RenderedDesiredState []byte` is **removed** — the server renders now.
- New public op to register/onboard an operated app (façade over 4.2).
- New public read op `QueryDeploymentHealth(operatedAppID) → [{modelKey, kind, name, health}]` (see §6).

OpenAPI regeneration and webApp contract regeneration follow.

### 4.4 System design

New relationship: `operationsManager → projectStateAccess`. Must land in `.systemDesign` or the architecture gate will reject the new edge.

---

## 5. The renderer

### 5.1 Input and output

**The renderer reads only its typed `RuntimeDesiredState`.** It never reads `project.json` — that would be a ResourceAccess component reaching across to another system's state. The **Manager** reads the deployment model's `cloud` environment (`.slots[6].model.deployment.environments[0]`) and folds namespace, host, container set, and env wiring into the typed struct before the call. This keeps §6's re-derivation honest: the same typed input always produces the same manifests and therefore the same model-key map.

Output per app: `Deployment` + `Service` for server and webapp, CNPG `Cluster`, `HTTPRoute`s, `SecurityPolicy`, `BackendTrafficPolicy`, the Keycloak realm CR (D12), and the Argo `Application` itself.

**No `ReferenceGrant`.** The production chart gates it on a cross-namespace backend, which cannot arise here (backends live in the app's own namespace), and the captured golden contains none. Rendering one would mean emitting a resource production does not have.

### 5.1a Model → manifest mapping (D11)

The deployment model is not a loose sketch that happens to resemble the deployment — it carries the deployable elements as `infrastructureNodes` with machine-readable `role` values, alongside workload nodes. The assembly selects on those:

| Model element | `role` | Renders as |
|---|---|---|
| `cloud-node-server-deployment` (workload node, `instances: 2`) | — | server `Deployment` + `Service` |
| `cloud-infra-static-assets` (nginx) | `other` | webapp `Deployment` + `Service` |
| `cloud-infra-gateway` (Envoy) | `gateway` | `HTTPRoute`s + `SecurityPolicy` + `BackendTrafficPolicy` |
| `cloud-infra-keycloak` (OIDC) | `identityProvider` | Keycloak realm/client CR |
| `cloud-infra-operatedsystemstate`, `-billingstate`, `-usagelog` | `database` ×3 | one CNPG `Cluster` |

Three properties of this mapping are load-bearing and easy to get wrong:

- **`role: other` is ambiguous.** `cloud-infra-static-assets` (nginx) and `cloud-infra-temporal` share it. The static-asset node is identified by its relationship to the webapp container, not by role alone. Do not invent a new role value to disambiguate — the model is a design artifact with its own review rail.
- **Three `database` nodes map to one `Cluster`.** Production runs a single `archistrator-postgres` serving all three logical stores. All three diagram nodes therefore colour from that one resource's health.
- **`archistrator-webapp` sits on `cloud-node-browser` and must stay neutral.** The SPA genuinely executes in the architect's browser; the in-cluster thing is the nginx that serves it. Colouring the browser node from cluster health would be wrong.

### 5.2 Acceptance criterion — the golden diff

Render archistrator's own `DesiredState` and compare against `helm template` output of the four existing hand-written charts. **Semantic equivalence with what is running in production today is the bar.** This is what makes the dogfood verifiable rather than hopeful: the target is not "plausible YAML", it is "the YAML currently keeping archistrator alive".

**One exemption: the Keycloak CR (D12).** It has no production counterpart — the realm and client are hand-managed in the admin console today. It is therefore the single rendered object with no golden diff to check it, and needs its own test plus a deliberate first apply. Treat it as the highest-risk object in the render, not the easiest: a wrong realm or client CR breaks login for the whole app, and archistrator's own front door is on that path.

### 5.3 Invariant gates (build-failing tests)

- No `kind: Secret` carrying `data` ever appears in rendered output. This inherits the existing convention, stated in `archistrator-server/templates/secret.yaml`: *"all secrets … are created out-of-band so they are never rendered into a committed manifest."* The renderer emits only `secretKeyRef` references.
- Every `destination.namespace` equals the app's own namespace.
- `SelfManaged` output carries `prune: false` and manual sync.
- Rendering is deterministic: identical input yields byte-identical output (required for content-idempotent publish, and for §6's re-derivation).

### 5.4 Fleet re-render

D3's accepted cost. Because publish is content-idempotent, the reconcile tick can re-render and republish only when content differs — the fleet converges on a renderer change without a separate migration mechanism. Out of scope for this slice (one app), but the property is why D3 is affordable.

### 5.5 What the Keycloak CR does and does not do (D12)

Verified against the operator's own source at the pinned 26.4.2 tag, not against documentation — third-party docs disagree with the shipped code on more than one point (the current docs describe `v2beta1`; 26.4.2 serves `v2alpha1` only, and the placeholder syntax is `${VAR}`, not `$(VAR)`).

**`KeycloakRealmImport` is create-only.** If the realm already exists it is *not* overwritten, and the CR neither updates nor deletes. The practical consequences:

| Case | Effect |
|---|---|
| A newly onboarded app | Realm and client are provisioned. This is D12's goal, and it is met. |
| **archistrator itself** | Its realm is already hand-managed in the admin console, so applying the CR is a **deliberate no-op**. |

So "GitOps owns the realm" is *not* achieved by this mechanism and cannot be. What is achieved is automated provisioning for future apps. The founder ratified this trade-off knowingly (2026-08-08) rather than expanding scope to a reconciling writer on the platform's own login path.

Two further constraints, both real prerequisites rather than footnotes:

- The operator requires `keycloakCRName` to reference a Keycloak CR in the **same namespace**, so the rendered CR lands in `keycloak`, not the app's namespace. The client-secret placeholder Secret must therefore exist in the `keycloak` namespace as well as the app's — a new out-of-band step for the cutover runbook.
- The rendered realm body is **minimal: no roles, groups, or mappers.** Harmless for archistrator (no-op anyway), but a genuinely new app's users would lack `drive-phase` / `approve-artifact`. That gap belongs to built-app onboarding (§11), not here.

Because this object is exercised for the first time by a future tenant rather than by this dogfood, its unit tests are the only thing standing behind it. Treat a change to it as a change to production login.

---

## 6. Health overlay on the deployment diagram

The Deployment & Operations Model page renders the design-time deployment view. This overlays live health onto it, turning a design artifact into the live operations surface.

**Mechanism.** Rendering is a pure, deterministic function (§5.3), so the server re-derives the `model key → (kind, name, namespace)` map on demand and joins it against `Application.status.resources[]`, which carries **per-resource** `health.status` rather than only an app-level rollup. No labels or annotations are needed in the status path.

**Scope of observability — the trap.** The model in `.slots[6].model.deployment` has three environments: `cloud`, `test`, `local`. Only `cloud` can carry health. And within `cloud`, most nodes are not ours:

```
cloud-node-architect-machine     neutral   the architect's own computer
  └ cloud-node-browser           neutral   a web browser
cloud-node-cluster
  ├ cloud-node-ns-archistrator   observed
  │   └ cloud-node-server-deployment   observed
  ├ cloud-node-ns-temporal       neutral   another namespace
  └ cloud-node-ns-gtd            neutral   another namespace
cloud-node-external              neutral   external services
```

A strict binary applied naively would paint the architect's laptop, their browser, and the gtd namespace red. **Only nodes the renderer actually emits resources for may take a colour**; everything else — including the whole `test` and `local` environments — renders neutral.

**States** (D10): `Healthy` → green; `Progressing`/`Degraded`/`Missing`/`Suspended`/`Unknown` → red; not in the resource set → neutral.

---

## 7. Security

The server's power is **git write access to exactly one repository**, not Argo API access.

- A dedicated GitHub App scoped to `../software` only, separate from the construction GitHub App which writes user repos. Least privilege: neither credential can do the other's job.
- Declared in the deployment model as a `gitops` infra entry with `profiles: ["cloud"]`.
- Combined with the existing binding — `operatedRuntimeAccess: local → Local (dry-run), cloud → Real` — the local profile has no credential to hold. **Local cannot deploy structurally, not by policy check.**
- Health reads use an in-cluster ServiceAccount with read-only access to `Application` CRs in `argocd`. No write, no token.

Because D2 keeps all Kubernetes YAML server-authored, no agent-authored manifest is ever applied. The remaining tenant risk is the app's own container image, which is addressed by ordinary namespace guardrails (PodSecurity, ResourceQuota, NetworkPolicy) — out of scope for the archistrator-only slice, required before onboarding tenants.

---

## 8. Local profile (D9)

- Composition root does not mount the operations routes when `resolveProfile()` is local.
- The webApp needs a signal, and none exists today (`resolveProfile` is server-side only). Add a minimal capabilities read — `GET /api/v1/capabilities → {operations: bool}` — and gate the Operations nav entry and route on it.
- The deployment diagram's health overlay is likewise absent locally: with no operations surface there is nothing to query, and every node renders neutral.

---

## 9. Testing

| Level | Coverage |
|---|---|
| Renderer unit | Golden files per manifest kind; determinism; the §5.3 invariant gates |
| Golden diff | Rendered output vs `helm template` of the four production charts |
| RA integration | Commit path against a scratch git repo: content-idempotency, no-op on identical content, withdraw removes the app directory |
| Manager | `DeployWorkflow` with the typed desired state; the `FailedPrecondition` path once `RegisterOperatedSystem` has run; the Conflict recovery loop |
| Health | `Application.status.resources[]` fixtures → model-key join; the neutral-state cases (other namespaces, other environments) |
| webApp | Operations hidden in local profile; diagram colouring including neutral nodes |

---

## 10. Cutover

Reversible at every step.

1. **Shadow** — render and golden-diff. Commit nothing anywhere.
2. **Parallel** — commit rendered output to `k8s/argocd/apps/archistrator/`, referenced by no Application. Inspect the real YAML in git at leisure.
3. **Flip** — delete the four `applications/archistrator-*.yaml`, add the rendered Application with `prune: false` + manual sync. A human reads the diff and clicks Sync.
4. **Rollback** — `git revert` in the software repo.

Step 3 is the only irreversible-feeling moment, and it is guarded by manual sync: Argo will show OutOfSync and wait.

---

## 11. Out of scope

Each is its own follow-up, not a gap in this design:

- **Built-app onboarding** — Dockerfile + release-workflow scaffolding in `method-assets`, image tag flow from construction through the deployable bundle. Blocks deploying anything other than archistrator (D6).
- **SLO and cost attribution** — Prometheus client, per-app SLO definitions, attribution formula. Keeps the autoscaler inert and billing empty (D7).
- **Tenant namespace guardrails** — PodSecurity, ResourceQuota, NetworkPolicy, Argo `AppProject` sandbox. Required before any non-archistrator app deploys.
- **Automated image-tag promotion** — still manual, as the current `archistrator-server.yaml` annotation notes; the renderer now owns the tag, so promotion becomes a question of what feeds it.
- **Fleet re-render on renderer change** — the mechanism is free (§5.4) but untested with one app.
