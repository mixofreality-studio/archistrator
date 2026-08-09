# Operations & ArgoCD Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make archistrator deployable through its own operations rail — the `operatedRuntimeAccess` Real profile renders plain Kubernetes manifests into the `../software` ArgoCD app-of-apps, reads health back from the Argo `Application` CR, and colours the deployment diagram live.

**Architecture:** The Manager assembles a typed, substrate-neutral `RuntimeDesiredState` from three sources (project.json deployment model, deployable bundle, head-state) and hands it to the ResourceAccess layer, which renders Kubernetes YAML and commits it to one git repository. Kubernetes vocabulary exists only inside `operatedRuntimeAccess`. Rendering is a pure deterministic function, which is what lets the health overlay re-derive its model-key map by re-rendering rather than storing a mapping table.

**Tech Stack:** Go 1.26, Temporal, pgx/CloudNativePG, ArgoCD, Envoy Gateway, React + MUI + TanStack Query, schema-first codegen (`project.json` → `contract.gen.go`).

**Spec:** `docs/superpowers/specs/2026-08-07-operations-argocd-deployment-design.md`

## Global Constraints

- **All Go commands run with `GOWORK=off`.** The server builds against published platform tags, not the workspace. Every `go test` / `go run` / `make` invocation in this plan already includes it.
- **Never hand-edit `*.gen.go`.** Contracts are owned by `.serviceContracts` in `.aiarch/state/project.json`; regenerate with `cd server && GOWORK=off make gen-models`.
- **Never render a `Secret` carrying `data`.** Inherited convention, stated in `archistrator-server/templates/secret.yaml`: all secrets are created out-of-band. The renderer emits only `secretKeyRef` references.
- **The software repo is `/Users/davidmarne/mixofrealitystudio/software`**, remote `https://github.com/davidmarne/aiarchmultiplatform.git`. Its ArgoCD tree runs `prune: true` and `selfHeal: true`.
- **Archistrator's own Application must render `prune: false` and manual sync.** A renderer bug must never be able to delete the control plane.
- **Test commands:** `cd server && GOWORK=off make test` (Go), `cd webApp && npm run check` (TypeScript: typecheck + lint + format + test).
- **Commit after every task.** Tasks are ordered so the tree builds and tests pass at each boundary.

---

## File Structure

**Server — new files**

| Path | Responsibility |
|---|---|
| `server/internal/resourceaccess/operatedruntime/testdata/golden/*.yaml` | Expected rendered output, and the `helm template` capture of the four production charts. |
| `server/internal/resourceaccess/operatedruntime/testdata/argo/*.json` | Argo `Application` CR fixtures for the health parser. |

> **The `TestFileLayout` gate forbids new hand-written `.go` files in these packages.** Every ResourceAccess package in this repo has exactly one impl file and one test file (`projectstateaccess.go` runs past 5,800 lines — large single files are the accepted cost of the standard). So the renderer, the GitOps commit path, and the Argo health reader ALL fold into `operatedruntimeaccess.go`, and all their tests into `access_test.go`. Do not create `render.go`, `gitops.go`, `argohealth.go`, or their test files — the gate is zero-waiver and will fail the build.

**Server — modified**

| Path | Change |
|---|---|
| `.aiarch/state/project.json` | Three `.serviceContracts` entries; one `.systemDesign` relationship; one `gitops` infra entry + binding. |
| `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go` | Real profile bodies replace `notImplemented`. |
| `server/internal/resourceaccess/operatedsystemstate/operatedsystemstateaccess.go` | `RegisterOperatedSystem` implementation. |
| `server/internal/resourceaccess/operatedsystemstate/schema.sql` | Drop the "no writer" gap comments; `deployable_bundle_ref` and `customer_id` now have a writer. |
| `server/internal/manager/operations/deploy.go` | Consume the retrieved bundle; pass typed desired state. |
| `server/internal/manager/operations/operationsmanager.go` | New public ops; drop `RenderedDesiredState`. |
| `server/cmd/server/hooks.go` | Gate operations route mounting on profile; wire gitops credential. |

**webApp — modified**

| Path | Change |
|---|---|
| `src/hooks/useCapabilities.ts` (new) | Reads `GET /api/v1/capabilities`. |
| `src/routes/router.tsx` | Gate the Operations route. |
| `src/components/AppShell.tsx` | Gate the Operations nav entry. |
| `src/components/flow/DeploymentNodes.tsx` | Health colour on node borders. |
| `src/components/flow/DeploymentFlow.tsx` | Thread health map into nodes. |
| `src/hooks/useDeploymentHealth.ts` (new) | Reads `QueryDeploymentHealth`. |

---

## Task 1: `RegisterOperatedSystem` contract + implementation

Closes the documented seeding gap. Without this, no operated app can exist and `DeployWorkflow` cannot get past its `FailedPrecondition` guard.

**Files:**
- Modify: `.aiarch/state/project.json` (`.serviceContracts.operatedSystemStateAccess`)
- Modify: `server/internal/resourceaccess/operatedsystemstate/operatedsystemstateaccess.go`
- Modify: `server/internal/resourceaccess/operatedsystemstate/schema.sql`
- Test: `server/internal/resourceaccess/operatedsystemstate/access_test.go`

**Interfaces:**
- Produces: `RegisterOperatedSystem(rc fwra.Context, operatedAppID uuid.UUID, customerID uuid.UUID, projectRef string, deployableBundleRef string, idempotencyKey fwra.IdempotencyKey) (Version, error)` — returns `Version(1)` for a fresh row; on replay returns the recorded version (idempotent no-op).

- [ ] **Step 1: Add the operation to the contract entry**

Run this script from the repo root:

```python
import json, collections
p = '.aiarch/state/project.json'
d = json.load(open(p), object_pairs_hook=collections.OrderedDict)
e = d['serviceContracts']['operatedSystemStateAccess']
ops = e['interface']['operations']
assert not any(o['name'] == 'RegisterOperatedSystem' for o in ops), 'already added'
ops.insert(0, collections.OrderedDict([
    ('name', 'RegisterOperatedSystem'),
    ('params', [
        {'name': 'operatedAppID', 'schema': {'type': 'string', 'format': 'uuid',
         'x-go-import': 'github.com/google/uuid', 'x-go-type': 'uuid.UUID'}},
        {'name': 'customerID', 'schema': {'type': 'string', 'format': 'uuid',
         'x-go-import': 'github.com/google/uuid', 'x-go-type': 'uuid.UUID'}},
        {'name': 'projectRef', 'schema': {'type': 'string'}},
        {'name': 'deployableBundleRef', 'schema': {'type': 'string'}},
        {'name': 'idempotencyKey', 'schema': {'$ref': '#/$defs/IdempotencyKey'}},
    ]),
    ('returns', [{'name': 'version', 'schema': {'$ref': '#/$defs/Version'}}]),
]))
json.dump(d, open(p, 'w'), indent=2)
open(p, 'a').write('\n')
```

Then confirm the `IdempotencyKey` and `Version` `$defs` names match what the entry already uses:

```bash
python3 -c "
import json
e=json.load(open('.aiarch/state/project.json'))['serviceContracts']['operatedSystemStateAccess']
print(sorted(e['\$defs'].keys()))
print([o['name'] for o in e['interface']['operations']])
"
```

If the existing entry references idempotency keys or versions by a different `$ref` or inline shape, edit the inserted operation to match the sibling operations exactly — copy their param schema verbatim rather than inventing one.

- [ ] **Step 2: Regenerate and confirm the interface changed**

```bash
cd server && GOWORK=off make gen-models
git diff --stat internal/resourceaccess/operatedsystemstate/
```

Expected: `contract.gen.go` and `fake/fake.gen.go` both change; `OperatedSystemStateAccess` now has 7 methods.

- [ ] **Step 3: Write the failing test**

Add to `server/internal/resourceaccess/operatedsystemstate/access_test.go`, matching the file's existing setup helpers (it already stands up a Postgres-backed access for the other verbs — reuse that same helper rather than writing a new one):

```go
func TestRegisterOperatedSystem_CreatesRowAtVersionOne(t *testing.T) {
	acc, ctx := newTestAccess(t) // existing helper in this file

	appID := uuid.New()
	custID := uuid.New()
	v, err := acc.RegisterOperatedSystem(ctx, appID, custID, "project-abc", "bundle-xyz", "idem-1")
	if err != nil {
		t.Fatalf("RegisterOperatedSystem: %v", err)
	}
	if v != Version(1) {
		t.Fatalf("version = %d, want 1", v)
	}

	got, err := acc.ReadOperatedSystem(ctx, appID)
	if err != nil {
		t.Fatalf("ReadOperatedSystem: %v", err)
	}
	if got.DeployableBundleRef != "bundle-xyz" {
		t.Errorf("DeployableBundleRef = %q, want %q", got.DeployableBundleRef, "bundle-xyz")
	}
	if got.Version != Version(1) {
		t.Errorf("Version = %d, want 1", got.Version)
	}
}

func TestRegisterOperatedSystem_ReplayIsIdempotent(t *testing.T) {
	acc, ctx := newTestAccess(t)

	appID := uuid.New()
	custID := uuid.New()
	first, err := acc.RegisterOperatedSystem(ctx, appID, custID, "project-abc", "bundle-xyz", "idem-1")
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	second, err := acc.RegisterOperatedSystem(ctx, appID, custID, "project-abc", "bundle-xyz", "idem-1")
	if err != nil {
		t.Fatalf("replayed register: %v", err)
	}
	if first != second {
		t.Fatalf("replay returned version %d, want %d (idempotent no-op)", second, first)
	}
}
```

- [ ] **Step 4: Run the tests and watch them fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedsystemstate/ -run TestRegisterOperatedSystem -v
```

Expected: FAIL — the generated interface has the method but the concrete type does not implement it yet, so the package will not compile. That compile failure IS the red state.

- [ ] **Step 5: Implement the verb**

In `operatedsystemstateaccess.go`, following the dedup-first idempotency pattern the sibling write verbs already use (read the ledger first, then insert). The row is created at `version = 1`; a conflicting existing row is **not** an error to overwrite — a second registration with a different idempotency key returns `fwra.Conflict`, matching the head-state discipline the other verbs follow.

```go
// RegisterOperatedSystem seeds the head-state row for a newly operated app. It is the
// writer for deployable_bundle_ref and customer_id — the columns schema.sql previously
// documented as having none. Dedup-first: a replayed key returns the recorded version.
func (a *access) RegisterOperatedSystem(
	rc fwra.Context,
	operatedAppID uuid.UUID,
	customerID uuid.UUID,
	projectRef string,
	deployableBundleRef string,
	idempotencyKey fwra.IdempotencyKey,
) (Version, error) {
	// Dedup-first: replayed key collapses to the recorded resulting version.
	if v, ok, err := a.lookupMutation(rc, idempotencyKey); err != nil {
		return 0, err
	} else if ok {
		return v, nil
	}

	const q = `
INSERT INTO operated_system
    (operated_app_id, version, status, in_flight, deployable_bundle_ref, customer_id, project_ref)
VALUES ($1, 1, 0, false, $2, $3, $4)
ON CONFLICT (operated_app_id) DO NOTHING
RETURNING version`

	var v int64
	err := a.pool.QueryRow(rc.Context, q, operatedAppID, deployableBundleRef, customerID, projectRef).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fwra.New(fwra.Conflict,
			"operatedsystemstate.RegisterOperatedSystem: operated app already registered")
	}
	if err != nil {
		return 0, fwra.New(fwra.Unknown, "operatedsystemstate.RegisterOperatedSystem: "+err.Error())
	}
	if err := a.recordMutation(rc, idempotencyKey, operatedAppID, Version(v)); err != nil {
		return 0, err
	}
	return Version(v), nil
}
```

Match `lookupMutation` / `recordMutation` to whatever the existing verbs in this file actually call — reuse those helpers verbatim; do not add new ones.

- [ ] **Step 6: Add the `project_ref` column**

In `schema.sql`, add to the `CREATE TABLE operated_system` body:

```sql
    project_ref           text        NOT NULL DEFAULT '',
```

And replace the two GAP paragraphs in the header comment (the ones beginning `deployable_bundle_ref  → OperatedSystem.DeployableBundleRef. GAP` and `customer_id            → the delinquency-sweep scope key`) with:

```
--   deployable_bundle_ref  → OperatedSystem.DeployableBundleRef. Written by
--                            RegisterOperatedSystem at onboarding.
--   customer_id            → the delinquency-sweep scope key for ReadInFlightOperatedApps
--                            (InFlightScope.CustomerID). Written by RegisterOperatedSystem.
--   project_ref            → the project whose project.json supplies the deployment model
--                            the operationsManager renders from. A pointer, never a copy.
```

- [ ] **Step 7: Run the tests and watch them pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedsystemstate/ -run TestRegisterOperatedSystem -v
```

Expected: PASS, both tests.

- [ ] **Step 8: Commit**

```bash
git add .aiarch/state/project.json server/internal/resourceaccess/operatedsystemstate/
git commit -m "feat(operatedsystemstate): add RegisterOperatedSystem seeding verb"
```

---

## Task 2: Typed `RuntimeDesiredState` end-to-end

Replaces the opaque `{Bytes, ContentType}` blob with the struct the renderer needs, **and** populates it from its three real sources in the same task. These are one task deliberately: a contract change without its assembly would mean writing a knowingly-incomplete literal, and there is no reason to split them — assembly depends only on the typed struct and the `projectStateAccess` edge, not on the renderer.

**Files:**
- Modify: `.aiarch/state/project.json` (`.serviceContracts.operatedRuntimeAccess`, `.systemDesign` relationships)
- Modify: `server/internal/manager/operations/deploy.go` (assembly folds in here — `TestFileLayout` allows one file per workflow, and this is `DeployWorkflow`'s)
- Modify: `server/internal/manager/operations/manager_test.go`
- Modify: `server/internal/manager/operations/deploy.go`

**Interfaces:**
- Consumes: `projectstate.ReadProject(rc fwra.Context, projectID ProjectID) (Project, error)`; `artifact.RetrieveConstructionOutput`; `operatedsystemstate.OperatedSystem`.
- Produces: `operatedruntime.RuntimeDesiredState` with fields `AppName, Namespace, Host, ModelKey string`, `Server, WebApp Workload`, `Postgres PostgresSpec`, `OIDC OIDCSpec`, `SelfManaged bool`; `Workload{ModelKey, Image string, Replicas int64}`; `PostgresSpec{ModelKeys []string, Enabled bool, Instances int64, StorageClass string}`; `OIDCSpec{ModelKey, Issuer, ClientID, ClientSecretRef string}`.

> **`PostgresSpec.ModelKeys` is a slice, not a single key.** Three `database` nodes collapse to one rendered `Cluster`, but all three are real diagram nodes and all three must colour from that one resource's health. The keys are sorted, because the renderer must be byte-deterministic.
- Produces: `func assembleDesiredState(proj projectstate.Project, bundle deployableBundle, op operatedsystemstate.OperatedSystem) (operatedruntime.RuntimeDesiredState, error)`.

- [ ] **Step 1: Replace the `RuntimeDesiredState` `$def`**

```python
import json, collections
p = '.aiarch/state/project.json'
d = json.load(open(p), object_pairs_hook=collections.OrderedDict)
defs = d['serviceContracts']['operatedRuntimeAccess']['$defs']

def obj(props, required):
    return collections.OrderedDict([
        ('type', 'object'),
        ('properties', collections.OrderedDict(props)),
        ('required', required),
        ('additionalProperties', False),
    ])

defs['Workload'] = obj([
    ('ModelKey', {'type': 'string'}),
    ('Image', {'type': 'string'}),
    ('Replicas', {'type': 'integer'}),
], ['ModelKey', 'Image', 'Replicas'])

defs['PostgresSpec'] = obj([
    ('ModelKeys', {'type': 'array', 'items': {'type': 'string'}}),
    ('Enabled', {'type': 'boolean'}),
    ('Instances', {'type': 'integer'}),
    ('StorageClass', {'type': 'string'}),
], ['ModelKeys', 'Enabled', 'Instances', 'StorageClass'])

defs['OIDCSpec'] = obj([
    ('ModelKey', {'type': 'string'}),
    ('Issuer', {'type': 'string'}),
    ('ClientID', {'type': 'string'}),
    ('ClientSecretRef', {'type': 'string'}),
], ['ModelKey', 'Issuer', 'ClientID', 'ClientSecretRef'])

defs['RuntimeDesiredState'] = obj([
    ('AppName', {'type': 'string'}),
    ('Namespace', {'type': 'string'}),
    ('Host', {'type': 'string'}),
    ('ModelKey', {'type': 'string'}),
    ('Server', {'$ref': '#/$defs/Workload'}),
    ('WebApp', {'$ref': '#/$defs/Workload'}),
    ('Postgres', {'$ref': '#/$defs/PostgresSpec'}),
    ('OIDC', {'$ref': '#/$defs/OIDCSpec'}),
    ('SelfManaged', {'type': 'boolean'}),
], ['AppName', 'Namespace', 'Host', 'ModelKey', 'Server', 'WebApp', 'Postgres', 'OIDC', 'SelfManaged'])

json.dump(d, open(p, 'w'), indent=2)
open(p, 'a').write('\n')
```

- [ ] **Step 2: Add the architecture relationship**

The Manager is about to call a ResourceAccess it has never called. Declare it, or the architecture gate rejects the edge.

Inspect the existing shape first — the slot index and relationship fields must match siblings exactly:

```bash
python3 -c "
import json
d=json.load(open('.aiarch/state/project.json'))
for k,s in d['slots'].items():
    if isinstance(s,dict) and s.get('model',{}).get('relationships'):
        print('slot',k,'->',json.dumps(s['model']['relationships'][0]))
        break
"
```

Then append, in that same shape:

```
{"from": "operationsManager", "to": "projectStateAccess",
 "label": "Reads the operated app's deployment model from",
 "technology": "in-process", "mode": "sync"}
```

- [ ] **Step 3: Regenerate contracts and activities**

```bash
cd server && GOWORK=off make gen-models && GOWORK=off make gen-temporal
git diff --stat internal/
```

Expected: `operatedruntime/contract.gen.go` carries the new struct; `operations/activities.gen.go` and `invokers.gen.go` gain `ProjectState*` entries.

- [ ] **Step 4: Write the failing tests**

```go
func TestAssembleDesiredState_MapsCloudEnvironmentAndBundleImages(t *testing.T) {
	proj := testProject(t)   // fixture: a trimmed project with a cloud environment
	bundle := deployableBundle{Output: artifact.ConstructionOutput{ /* server+webapp image refs */ }}
	op := operatedsystemstate.OperatedSystem{ID: uuid.New(), Version: 3}

	got, err := assembleDesiredState(proj, bundle, op)
	if err != nil {
		t.Fatalf("assembleDesiredState: %v", err)
	}
	if got.Namespace != "archistrator" {
		t.Errorf("Namespace = %q, want archistrator", got.Namespace)
	}
	if got.Host != "archistrator.capture-gtd.com" {
		t.Errorf("Host = %q, want archistrator.capture-gtd.com", got.Host)
	}
	if got.Server.ModelKey != "cloud-node-server-deployment" {
		t.Errorf("Server.ModelKey = %q, want cloud-node-server-deployment", got.Server.ModelKey)
	}
	if !got.SelfManaged {
		t.Error("archistrator must assemble as SelfManaged")
	}
}

func TestAssembleDesiredState_RejectsAModelWithNoCloudEnvironment(t *testing.T) {
	proj := testProjectLocalOnly(t) // fixture with only the local environment
	_, err := assembleDesiredState(proj, deployableBundle{}, operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when the deployment model has no cloud environment")
	}
}
```

- [ ] **Step 5: Run and watch them fail**

```bash
cd server && GOWORK=off go test ./internal/manager/operations/ -run TestAssemble -v
```

Expected: FAIL — `undefined: assembleDesiredState`. The package may also fail to compile because `deploy.go` still builds the old `{Bytes, ContentType}` literal; that compile failure is part of the red state.

- [ ] **Step 6: Implement the fold**

Read the `cloud` environment from the project's deployment model and select elements **by `role`, never by the free-text `technology` string** (spec D11). The deployable elements are `infrastructureNodes` carrying roles, alongside workload nodes:

| Model element | `role` | Feeds |
|---|---|---|
| `cloud-node-server-deployment` (workload node, `instances: 2`) | — | `Server` workload |
| `cloud-infra-static-assets` (nginx) | `other` | `WebApp` workload |
| `cloud-infra-gateway` (Envoy) | `gateway` | route/policy rendering |
| `cloud-infra-keycloak` (OIDC) | `identityProvider` | `OIDC` spec + the Keycloak CR |
| `cloud-infra-operatedsystemstate`, `-billingstate`, `-usagelog` | `database` ×3 | one `PostgresSpec` |

Three traps, all of which will silently produce wrong output if missed:

- **`role: other` is ambiguous** — nginx and `cloud-infra-temporal` share it. Identify the static-asset node by its relationship to the webapp container, not by role alone. Do NOT add a new role value to the model; it is a design artifact with its own review rail.
- **Three `database` nodes → one `PostgresSpec`.** Production runs a single `archistrator-postgres` serving all three logical stores.
- **`archistrator-webapp` sits on `cloud-node-browser`.** That is correct — the SPA executes in the browser. The in-cluster thing is the nginx serving it, so the `WebApp` workload's `ModelKey` is the static-asset node's key, NOT the browser node's.

Take images from the bundle. Replicas come from the workload node's `Instances` (head-state carries no replica field). `SelfManaged` is true when the operated app's project is archistrator's own.

Fail loudly rather than defaulting: a model with no cloud environment, or a required role with no matching node, is a real misconfiguration and must surface as an error the operator can read — not a silently half-rendered deployment.

Fail loudly rather than defaulting: a model with no cloud environment, or a workload node with no matching container instance, is a real misconfiguration and must surface as an error the operator can read — not a silently half-rendered deployment.

- [ ] **Step 7: Wire it into `deploy.go`**

Replace the old `RuntimeDesiredState{Bytes: ..., ContentType: ...}` literal with a call through `assembleDesiredState`. The bundle comes from `retrieveBundle` — **delete the `_` discard at line 47** and retain the result. The project comes from the newly generated `ProjectStateReadProject` invoker.

- [ ] **Step 8: Run and watch them pass**

```bash
cd server && GOWORK=off go test ./internal/manager/operations/ ./internal/resourceaccess/operatedruntime/ -v
```

Expected: PASS. The Real profile's `publishDesiredState` still returns its unimplemented-backend diagnostic — that is correct and expected until Task 7; the Local/dry-run profile accepts the payload as a no-op.

- [ ] **Step 9: Full suite and commit**

```bash
cd server && GOWORK=off make test
git add .aiarch/state/project.json server/internal/
git commit -m "feat(operations): typed RuntimeDesiredState assembled from model, bundle, and head-state"
```

---

## Task 3: Capture the golden target

Before writing a renderer, capture what it must reproduce. This is the acceptance bar for Tasks 4–6.

**Files:**
- Create: `server/internal/resourceaccess/operatedruntime/testdata/golden/production/*.yaml`

- [ ] **Step 1: Render the four production charts**

```bash
cd /Users/davidmarne/mixofrealitystudio/software/products/archistrator/helm
DEST=/Users/davidmarne/mixofrealitystudio/archistrator/server/internal/resourceaccess/operatedruntime/testdata/golden/production
mkdir -p "$DEST"
for c in archistrator-server archistrator-webapp archistrator-postgres archistrator-gateway-routes; do
  helm template "$c" "./$c" > "$DEST/$c.yaml"
done
wc -l "$DEST"/*.yaml
```

If a chart requires a secrets file it does not have, render it with placeholder values via `--set` — the placeholders must be obviously fake (`PLACEHOLDER_NOT_A_SECRET`) and must never be real credentials.

- [ ] **Step 2: Verify no real secrets were captured**

```bash
grep -rn "kind: Secret" -A 5 "$DEST"/*.yaml || echo "no Secret resources — good"
```

Expected: either no matches, or only Secrets whose values are the obvious placeholders. **If any real credential appears, delete the file and re-render.** These files are about to be committed.

- [ ] **Step 3: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/testdata/golden/production/
git commit -m "test(operatedruntime): capture production chart output as render target"
```

---

## Task 4: Renderer — server and webapp workloads

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go` (append the renderer — `TestFileLayout` forbids a new `render.go`)
- Modify: `server/internal/resourceaccess/operatedruntime/access_test.go` (append the tests)

**Interfaces:**
- Consumes: `RuntimeDesiredState` (Task 2).
- Produces: `type Manifest struct { ModelKeys []string; Kind, Name, Namespace, YAML string }` and `func render(d RuntimeDesiredState) ([]Manifest, error)` — returns manifests in a deterministic order (sorted by `Kind` then `Name`).

> **`ModelKeys` is a slice because the mapping is not one-to-one.** Most manifests answer to exactly one diagram node, but the Postgres `Cluster` answers to all three `database` nodes (`operatedsystemstate`, `billingstate`, `usagelog`) — production runs one cluster serving all three logical stores, and all three nodes must colour from its health. Keep the slice sorted so the render stays byte-deterministic.

- [ ] **Step 1: Write the failing test**

```go
package operatedruntime

import "testing"

func testDesiredState() RuntimeDesiredState {
	return RuntimeDesiredState{
		AppName:   "archistrator",
		Namespace: "archistrator",
		Host:      "archistrator.capture-gtd.com",
		ModelKey:  "cloud-node-ns-archistrator",
		Server: Workload{
			ModelKey: "cloud-node-server-deployment",
			Image:    "ghcr.io/mixofreality-studio/archistrator-server:0.8.16",
			Replicas: 1,
		},
		WebApp: Workload{
			ModelKey: "cloud-infra-static-assets",
			Image:    "ghcr.io/mixofreality-studio/archistrator-webapp:0.6.14",
			Replicas: 1,
		},
		Postgres: PostgresSpec{
			ModelKeys:    []string{"cloud-infra-billingstate", "cloud-infra-operatedsystemstate", "cloud-infra-usagelog"},
			Enabled:      true,
			Instances:    1,
			StorageClass: "do-block-storage",
		},
		OIDC: OIDCSpec{
			ModelKey:        "cloud-infra-keycloak",
			Issuer:          "https://keycloak.capture-gtd.com/realms/archistrator",
			ClientID:        "archistrator-webapp",
			ClientSecretRef: "archistrator-oidc-client-secret",
		},
		SelfManaged: true,
	}
}

func TestRender_EmitsServerDeploymentWithModelKey(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var found *Manifest
	for i := range ms {
		if ms[i].Kind == "Deployment" && ms[i].Name == "archistrator-server" {
			found = &ms[i]
		}
	}
	if found == nil {
		t.Fatal("no Deployment/archistrator-server in rendered output")
	}
	if len(found.ModelKeys) != 1 || found.ModelKeys[0] != "cloud-node-server-deployment" {
		t.Errorf("ModelKeys = %v, want [cloud-node-server-deployment]", found.ModelKeys)
	}
	if found.Namespace != "archistrator" {
		t.Errorf("Namespace = %q, want archistrator", found.Namespace)
	}
}

func TestRender_IsDeterministic(t *testing.T) {
	a, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	b, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("manifest count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("manifest %d differs between renders:\n%+v\n%+v", i, a[i], b[i])
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender -v
```

Expected: FAIL — `undefined: render`, `undefined: Manifest`.

- [ ] **Step 3: Implement the workload half of the renderer**

Create `render.go`. Build the YAML with `text/template` over typed data (not string concatenation), and sort the output for determinism. Derive the env block from the same variable names the production chart uses — read `testdata/golden/production/archistrator-server.yaml` and reproduce its `env:` entries exactly.

```go
package operatedruntime

import (
	"bytes"
	"sort"
	"text/template"
)

// Manifest is one rendered Kubernetes object plus the deployment-model node it
// came from. ModelKey is what lets the health overlay attribute a live resource
// back to a diagram node without storing a mapping table.
type Manifest struct {
	// ModelKeys are the deployment-model nodes this object answers to. Usually one;
	// the Postgres Cluster answers to all three database nodes, since production runs
	// one cluster serving all three logical stores and every one of those diagram
	// nodes must colour from its health. Kept sorted so the render is deterministic.
	ModelKeys []string
	Kind      string
	Name      string
	Namespace string
	YAML      string
}

var deploymentTmpl = template.Must(template.New("deployment").Parse(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app.kubernetes.io/name: {{ .Name }}
    app.kubernetes.io/part-of: {{ .AppName }}
spec:
  replicas: {{ .Replicas }}
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Name }}
    spec:
      containers:
      - name: {{ .Name }}
        image: "{{ .Image }}"
        ports:
        - name: http
          containerPort: {{ .Port }}
          protocol: TCP
`))

type deploymentData struct {
	Name, Namespace, AppName, Image string
	Replicas                        int64
	Port                            int
}

func renderDeployment(d deploymentData) (string, error) {
	var b bytes.Buffer
	if err := deploymentTmpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// render turns a typed desired state into the ordered manifest set. Pure: no I/O,
// no clock, no randomness — the health overlay depends on re-rendering producing
// byte-identical output.
func render(d RuntimeDesiredState) ([]Manifest, error) {
	var out []Manifest

	for _, w := range []struct {
		suffix string
		wl     Workload
		port   int
	}{
		{"-server", d.Server, 8080},
		{"-webapp", d.WebApp, 80},
	} {
		name := d.AppName + w.suffix
		y, err := renderDeployment(deploymentData{
			Name: name, Namespace: d.Namespace, AppName: d.AppName,
			Image: w.wl.Image, Replicas: w.wl.Replicas, Port: w.port,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, Manifest{
			ModelKeys: []string{w.wl.ModelKey}, Kind: "Deployment",
			Name: name, Namespace: d.Namespace, YAML: y,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
```

- [ ] **Step 4: Run and watch it pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender -v
```

Expected: PASS.

- [ ] **Step 5: Add Services, then re-run**

Add a `serviceTmpl` and emit one `Service` per workload, appending to `out` before the sort. Extend `TestRender_EmitsServerDeploymentWithModelKey` with a sibling assertion that a `Service` named `archistrator-server` exists carrying the same `ModelKey`. Re-run the same command; expect PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "feat(operatedruntime): render server and webapp workloads"
```

---

## Task 5: Renderer — Postgres, gateway routes, and the Argo Application

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go`
- Modify: `server/internal/resourceaccess/operatedruntime/access_test.go`

**Interfaces:**
- Produces: `render` additionally emits `Cluster` (CNPG), 4× `HTTPRoute`, `SecurityPolicy`, 4× `BackendTrafficPolicy`, the Keycloak realm CR, and `Application`. No `ReferenceGrant`.

- [ ] **Step 1: Write the failing test for the self-managed guard**

This is the safety property from the spec — write it before the code that satisfies it.

```go
func TestRender_SelfManagedApplicationDisablesPrune(t *testing.T) {
	ms, err := render(testDesiredState()) // SelfManaged: true
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var app *Manifest
	for i := range ms {
		if ms[i].Kind == "Application" {
			app = &ms[i]
		}
	}
	if app == nil {
		t.Fatal("no Argo Application rendered")
	}
	if strings.Contains(app.YAML, "prune: true") {
		t.Error("self-managed Application must not enable prune")
	}
	if strings.Contains(app.YAML, "automated:") {
		t.Error("self-managed Application must not enable automated sync")
	}
}

func TestRender_TenantApplicationEnablesAutomatedSync(t *testing.T) {
	d := testDesiredState()
	d.SelfManaged = false
	d.AppName = "gtdapp"
	d.Namespace = "gtdapp"

	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind != "Application" {
			continue
		}
		if !strings.Contains(m.YAML, "prune: true") {
			t.Error("tenant Application should enable prune")
		}
		if !strings.Contains(m.YAML, "selfHeal: true") {
			t.Error("tenant Application should enable selfHeal")
		}
		return
	}
	t.Fatal("no Argo Application rendered")
}
```

- [ ] **Step 2: Run and watch both fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run 'TestRender_(SelfManaged|Tenant)' -v
```

Expected: FAIL — no `Application` kind is emitted yet.

- [ ] **Step 3: Implement the Application template**

```go
var applicationTmpl = template.Must(template.New("application").Parse(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .AppName }}
  namespace: argocd
  finalizers:
  - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: {{ .RepoURL }}
    targetRevision: main
    path: k8s/argocd/apps/{{ .AppName }}
    directory:
      recurse: true
  destination:
    server: https://kubernetes.default.svc
    namespace: {{ .Namespace }}
  syncPolicy:
{{- if .SelfManaged }}
    # SELF-MANAGED: archistrator renders the manifests that govern archistrator.
    # Sync is manual and prune is disabled so a renderer bug can never delete the
    # control plane. A human reads the diff in the Argo UI and clicks Sync.
    syncOptions:
    - CreateNamespace=true
    - ServerSideApply=true
{{- else }}
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - CreateNamespace=true
    - ServerSideApply=true
{{- end }}
`))
```

Emit it in `render` with `ModelKeys: []string{d.ModelKey}`, `Kind: "Application"`, `Name: d.AppName`, `Namespace: "argocd"`.

- [ ] **Step 4: Run and watch both pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run 'TestRender_(SelfManaged|Tenant)' -v
```

Expected: PASS.

- [ ] **Step 5: Add the CNPG Cluster**

Emit a `Cluster` (`postgresql.cnpg.io/v1`) when `d.Postgres.Enabled`, with `instances: {{ .Instances }}` and the storage class, mirroring `testdata/golden/production/archistrator-postgres.yaml`. Carry `ModelKey: d.Postgres.ModelKey`. Add a test asserting that `Postgres.Enabled == false` emits no `Cluster`.

- [ ] **Step 6: Add gateway routes**

Emit `HTTPRoute` (one per route in the production chart: webapp `/`, api `/api`, healthz `/healthz`, readyz `/readyz`), `SecurityPolicy`, and `BackendTrafficPolicy` — 4 of each route/policy, and NO `ReferenceGrant` (the chart gates it on a cross-namespace backend that cannot arise; the golden has none) — mirroring `testdata/golden/production/archistrator-gateway-routes.yaml`.

**Reproduce the production chart's route arrangement exactly**, including the deliberate absence of a dedicated `/oauth2` route. That chart carries a load-bearing comment explaining why: a more-specific `/oauth2` route would steal the OIDC callback away from the policy-attached `/` route, so the Envoy filter would never run and no session would be established. Adding one would break login in a way that is hard to diagnose.

The `SecurityPolicy` references the OIDC client secret by name (`d.OIDC.ClientSecretRef`) and must never inline its value.

- [ ] **Step 6b: Add the Keycloak realm/client CR (spec D12)**

Emit a Keycloak CR provisioning the app's realm and its confidential OIDC client, carrying `ModelKeys: []string{d.OIDC.ModelKey}` — the `identityProvider` infra node's key (`cloud-infra-keycloak`).

**This object is the highest-risk thing in the render and has no golden diff.** Production's realm and client are hand-managed in the admin console — the `KeycloakRealmImport` was deliberately removed once — so nothing in `testdata/golden/production/` constrains it. A wrong realm or client breaks login for the entire app, and archistrator's own front door is on that path.

Consequences for how you write it:
- Match the CRD version the cluster's Keycloak operator actually serves. Check `k8s/argocd/auth/keycloak-operator.yaml` in the software repo rather than assuming.
- The client secret is referenced, never rendered (the no-Secret-data gate applies).
- The rendered client's `clientId` and `redirectUris` must match what the `SecurityPolicy` from Step 6 expects, or the OIDC flow breaks in a way the manifests alone won't reveal.
- Write an explicit unit test asserting realm name, `clientId`, and redirect URIs, since no golden file will catch a regression here.

- [ ] **Step 7: Run the full renderer suite**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender -v
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "feat(operatedruntime): render postgres, gateway routes, and Argo Application"
```

---

## Task 6: Invariant gates and the golden diff

The gates are the safety net; the golden diff is the acceptance bar.

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/access_test.go`

- [ ] **Step 1: Write the invariant gates**

```go
func TestRender_NeverEmitsSecretData(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if m.Kind == "Secret" {
			t.Errorf("renderer emitted a Secret (%s); all secrets are created out-of-band", m.Name)
		}
		if strings.Contains(m.YAML, "\ndata:") || strings.Contains(m.YAML, "\nstringData:") {
			t.Errorf("manifest %s/%s carries inline secret data", m.Kind, m.Name)
		}
	}
}

func TestRender_AllManifestsTargetTheAppNamespace(t *testing.T) {
	d := testDesiredState()
	ms, err := render(d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		// The Argo Application itself lives in argocd; everything else is the app's.
		if m.Kind == "Application" {
			if m.Namespace != "argocd" {
				t.Errorf("Application namespace = %q, want argocd", m.Namespace)
			}
			continue
		}
		if m.Namespace != d.Namespace {
			t.Errorf("%s/%s namespace = %q, want %q", m.Kind, m.Name, m.Namespace, d.Namespace)
		}
	}
}

func TestRender_EveryManifestCarriesAModelKey(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, m := range ms {
		if len(m.ModelKeys) == 0 {
			t.Errorf("%s/%s has no ModelKeys; it cannot be attributed to a diagram node", m.Kind, m.Name)
		}
		if !sort.StringsAreSorted(m.ModelKeys) {
			t.Errorf("%s/%s ModelKeys not sorted: %v (render must be deterministic)", m.Kind, m.Name, m.ModelKeys)
		}
	}
}
```

- [ ] **Step 2: Run the gates**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender -v
```

Expected: PASS. If `TestRender_EveryManifestCarriesAModelKey` fails, the missing key is a real gap — add the corresponding node to `testDesiredState()` and thread it through, do not weaken the assertion.

- [ ] **Step 3: Write the golden diff test**

```go
// TestRender_MatchesProduction is the acceptance bar: the renderer must reproduce
// what is currently keeping archistrator alive. Run with -update to refresh the
// expected file after an INTENTIONAL change, and read the diff before you do.
var update = flag.Bool("update", false, "rewrite golden files")

func TestRender_MatchesProduction(t *testing.T) {
	ms, err := render(testDesiredState())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var b strings.Builder
	for _, m := range ms {
		b.WriteString("---\n")
		b.WriteString(m.YAML)
	}
	got := b.String()

	path := filepath.Join("testdata", "golden", "archistrator-rendered.yaml")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("rendered output differs from golden.\nRun: go test ./internal/resourceaccess/operatedruntime/ -run TestRender_MatchesProduction -update\nThen READ the diff before committing.")
	}
}
```

- [ ] **Step 4: Generate the golden and compare against production by hand**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestRender_MatchesProduction -update
diff <(grep -v '^\s*#' internal/resourceaccess/operatedruntime/testdata/golden/archistrator-rendered.yaml) \
     <(grep -v '^\s*#' internal/resourceaccess/operatedruntime/testdata/golden/production/archistrator-server.yaml) | head -60
```

**This diff is the real work of the task.** Walk every difference and classify it:
- *Renderer is missing something production has* → fix the renderer.
- *Production has something we deliberately dropped* → note why in a comment in `render.go`.
- *Cosmetic (label ordering, whitespace)* → acceptable.

Repeat for `archistrator-webapp.yaml`, `archistrator-postgres.yaml`, and `archistrator-gateway-routes.yaml`. Do not proceed until every difference is explained.

- [ ] **Step 5: Run the full suite**

```bash
cd server && GOWORK=off make test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "test(operatedruntime): invariant gates and production golden diff"
```

---

## Task 7: GitOps commit path

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go` (append the commit path — `TestFileLayout` forbids a new `gitops.go`)
- Modify: `server/internal/resourceaccess/operatedruntime/access_test.go`

**Interfaces:**
- Consumes: `render` (Task 4–5).
- Produces: `realOperatedRuntime.PublishDesiredState` and `.Withdraw` become functional. `RuntimeConfig` gains `GitOpsToken string` alongside the existing `GitOpsRepoURL`.

- [ ] **Step 1: Write the failing test against a scratch repo**

```go
func TestPublishDesiredState_WritesManifestsAndIsContentIdempotent(t *testing.T) {
	repo := t.TempDir()
	mustRun(t, repo, "git", "init", "--initial-branch=main")
	mustRun(t, repo, "git", "config", "user.email", "test@example.com")
	mustRun(t, repo, "git", "config", "user.name", "test")

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	first := headCommit(t, repo)

	appPath := filepath.Join(repo, "k8s", "argocd", "apps", "archistrator")
	if _, err := os.Stat(appPath); err != nil {
		t.Fatalf("expected rendered manifests at %s: %v", appPath, err)
	}

	// Identical content must NOT produce a second commit.
	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-2"); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if headCommit(t, repo) != first {
		t.Error("republishing identical content created a new commit; publish must be content-idempotent")
	}
}

func TestWithdraw_RemovesTheAppDirectory(t *testing.T) {
	repo := t.TempDir()
	mustRun(t, repo, "git", "init", "--initial-branch=main")
	mustRun(t, repo, "git", "config", "user.email", "test@example.com")
	mustRun(t, repo, "git", "config", "user.name", "test")

	rt := realOperatedRuntime{config: RuntimeConfig{GitOpsRepoURL: "file://" + repo}}
	appID := uuid.New()

	if err := rt.PublishDesiredState(testCtx(t), appID, testDesiredState(), "idem-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := rt.Withdraw(testCtx(t), appID, "idem-2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	appPath := filepath.Join(repo, "k8s", "argocd", "apps", "archistrator")
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Errorf("withdraw left %s behind", appPath)
	}
}
```

Write `mustRun`, `headCommit`, and `testCtx` as small helpers at the bottom of the test file.

- [ ] **Step 2: Run and watch them fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run 'TestPublish|TestWithdraw' -v
```

Expected: FAIL — the Real profile still returns the `notImplemented` diagnostic.

- [ ] **Step 3: Implement the commit path**

In `gitops.go`: clone to a temp dir, write the manifest set to `k8s/argocd/apps/<app>/`, write the Application to `k8s/argocd/applications/<app>.yaml`, stage, and commit **only if `git diff --cached --quiet` reports changes**. That check is what makes publish content-idempotent — do not implement idempotency by comparing your own strings.

Withdraw removes both paths and commits the same way. Removing something that is already gone is a success, matching the contract's `NotFound ⇒ success` withdraw semantics.

**Withdraw must refuse the self-managed app.** This is the other half of the D8 guard, and without it the guard is decorative. `prune: false` stops a *renderer* bug from deleting archistrator's control plane, and Task 5 additionally omits the Argo finalizer when `SelfManaged` — but `Withdraw` deletes the Application outright, which is a third path to the same outcome. An operator clicking Withdraw on archistrator in archistrator's own console would take down the thing servicing the click, with nothing left to undo it.

Hard-guard it: `Withdraw` returns a terminal `fwra.ContractMisuse` (never a retry) naming the app when the target is the self-managed one. Write that test before the implementation. Tearing archistrator down is a deliberate `kubectl` operation by a human who means it, not a button.

- [ ] **Step 4: Replace the Real profile bodies**

In `operatedruntimeaccess.go`, `PublishDesiredState` and `Withdraw` now delegate to the `gitops.go` functions. Leave `WirePaymentConfig` returning `notImplemented` — it is genuinely out of scope. Update the package doc comment: the "N-DEP follow-up" paragraph is now stale for these two verbs.

- [ ] **Step 5: Run and watch them pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "feat(operatedruntime): GitOps commit path for publish and withdraw"
```

---

## Task 8: Public façade — register op and dropping `RenderedDesiredState`

**Files:**
- Modify: `.aiarch/state/project.json` (`.serviceContracts.operationsManager`)
- Modify: `server/internal/manager/operations/operationsmanager.go`
- Modify: `server/api/openapi.yaml` (generated)
- Modify: `webApp/src/hooks/useOperationsMutations.ts`

- [ ] **Step 1: Update the contract entry**

Remove `RenderedDesiredState` from the `DesiredStateChange` `$def`. Add a `RegisterOperatedApp` operation taking `operatedAppID`, `customerID`, `projectRef`, `deployableBundleRef` and returning the version.

- [ ] **Step 2: Regenerate everything downstream**

```bash
cd server && GOWORK=off make gen-models && GOWORK=off make gen-client && GOWORK=off make gen-temporal
cd ../webApp && npm run gen:api && npm run gen:ops
```

- [ ] **Step 3: Implement the façade method**

`RegisterOperatedApp` validates non-empty ids and starts a short Workflow that calls `RegisterOperatedSystem`, mirroring the pre-condition checks the sibling ops already perform before any Temporal call.

- [ ] **Step 4: Verify both sides build**

```bash
cd server && GOWORK=off go build ./... && GOWORK=off make test
cd ../webApp && npm run check
```

Expected: PASS both.

- [ ] **Step 5: Commit**

```bash
git add .aiarch/state/project.json server/ webApp/
git commit -m "feat(operations): add RegisterOperatedApp; drop client-supplied rendered state"
```

---

## Task 9: Health reads from the Argo Application CR

**Files:**
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go` (append the health reader — `TestFileLayout` forbids a new `argohealth.go`)
- Modify: `server/internal/resourceaccess/operatedruntime/access_test.go`

**Interfaces:**
- Produces: `func mapArgoHealth(status string) RuntimeStatus` — `"Healthy"` → `RuntimeStatusHealthy`; everything else observed → `RuntimeStatusDegraded`; unknown/absent → `RuntimeStatusUnknown`. Also `type ResourceHealth struct { Kind, Name, Namespace, Health string }` and a parser from the CR's `status.resources[]`.

- [ ] **Step 1: Write the failing test against a fixture**

Save a real-shaped Argo `Application` CR to `testdata/argo/application-healthy.json` containing a `status.resources` array with at least one `Healthy` and one `Degraded` entry, then:

```go
func TestMapArgoHealth(t *testing.T) {
	cases := []struct {
		in   string
		want RuntimeStatus
	}{
		{"Healthy", RuntimeStatusHealthy},
		{"Progressing", RuntimeStatusDegraded},
		{"Degraded", RuntimeStatusDegraded},
		{"Missing", RuntimeStatusDegraded},
		{"Suspended", RuntimeStatusDegraded},
		{"", RuntimeStatusUnknown},
	}
	for _, c := range cases {
		if got := mapArgoHealth(c.in); got != c.want {
			t.Errorf("mapArgoHealth(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseResourceHealth(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "argo", "application-healthy.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rs, err := parseResourceHealth(raw)
	if err != nil {
		t.Fatalf("parseResourceHealth: %v", err)
	}
	if len(rs) == 0 {
		t.Fatal("no resources parsed from status.resources[]")
	}
}
```

Note the deliberate asymmetry: `Progressing` maps to `Degraded`, not `Healthy`. Per the spec's D10 this is intended — the diagram reads red mid-rollout until it settles.

- [ ] **Step 2: Run and watch it fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run 'TestMapArgo|TestParseResource' -v
```

Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement the mapper and parser, then the live read**

`GetApplicationHealth` reads the `Application` CR for the app from the `argocd` namespace using an in-cluster ServiceAccount, and folds `status.health.status` through `mapArgoHealth`. When the CR is absent the app has never synced: return `RuntimeStatusPending`, not an error.

`GetSloStatus` returns `SloStatus{SloMet: true, Detail: "SLO monitoring not configured"}` and `ReadComputeAttribution` returns an empty `ComputeAttribution{}`. Both are deliberate per spec D7 — the 30s Schedule must stay green and billing must receive nothing. Comment them as such so a later reader does not mistake them for stubs to fill in casually.

- [ ] **Step 4: Run and watch them pass**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/resourceaccess/operatedruntime/
git commit -m "feat(operatedruntime): read health from the Argo Application CR"
```

---

## Task 10: `QueryDeploymentHealth`

Joins model keys to live per-resource health. **The join lives in ResourceAccess, not the Manager** — see the layering note below, which reverses this plan's original design.

**Files:**
- Modify: `.aiarch/state/project.json` (`.serviceContracts.operatedRuntimeAccess` — new verb; `.serviceContracts.operationsManager` — new public op)
- Modify: `server/internal/resourceaccess/operatedruntime/operatedruntimeaccess.go`
- Modify: `server/internal/resourceaccess/operatedruntime/access_test.go`
- Modify: `server/internal/arch_test.go` (REMOVE two allowlist entries — see below)
- Create: `server/internal/manager/operations/querydeploymenthealth.go` — permitted only because `TestFileLayout` allows one file per WORKFLOW and this is `QueryDeploymentHealthWorkflow`'s. The required filename is `strings.ToLower(strings.TrimSuffix(entryFunc, "Workflow")) + ".go"`; verify before creating.
- Modify: `server/internal/manager/operations/manager_test.go`

**Interfaces:**
- New RA verb: `GetDeploymentResourceHealth(rc fwra.Context, appID uuid.UUID, desired RuntimeDesiredState) ([]ModelKeyHealth, error)` where `ModelKeyHealth{ModelKey string, Status RuntimeStatus}`.
- New public op: `QueryDeploymentHealth(rc fwm.Context, operatedAppID uuid.UUID) (DeploymentHealth, error)` where `DeploymentHealth{Nodes []NodeHealth}` and `NodeHealth{ModelKey string, Health HealthState}`; `HealthState ∈ {HealthStateNeutral, HealthStateHealthy, HealthStateUnhealthy}`.

### Why the join moved into ResourceAccess

This plan originally put `joinHealth(manifests []Manifest, health []ResourceHealth, …)` in the Manager. That was wrong on two counts, both found in Task 9's review:

1. **It reintroduces the exact leak decision D11 removed.** `Manifest{Kind, Name, Namespace, YAML}` and `ResourceHealth{Kind, Name, Namespace, Health}` are Kubernetes vocabulary, and the planned `healthStateFor` switched on the literal Argo string `"Healthy"` inside `manager/operations`. D11 removed a single `"k8s"` literal from the Manager; this would have put a Kubernetes object-identity triple *and* an Argo health enum there. Spec D4 states the rule without exception.
2. **It could not compile.** The original text says `QueryDeploymentHealth` "re-runs `assembleDesiredState` + `render`" — but `render` is unexported and must stay so. A new RA verb was needed regardless; the only open question was what it returns.

The RA gains no new concept: `ModelKey`/`ModelKeys` are *already* RA contract vocabulary (they sit in `RuntimeDesiredState`, `Workload`, `PostgresSpec`, `OIDCSpec`), and `Manifest.ModelKeys` is precisely the record of where a model key and a Kubernetes identity coexist. Joining there joins where both inputs already live; joining in the Manager means exporting the Kubernetes half upward only to rejoin it.

### The split of responsibilities

| Layer | Answers |
|---|---|
| ResourceAccess | "Which model keys does this app actually deploy, and how is each doing?" Returns substrate-neutral `RuntimeStatus`, never an Argo string. |
| Manager | "Of the keys on this diagram, which are even ours?" Reads the cloud environment's full key list from project.json, marks Neutral every key the RA did not return, and collapses `RuntimeStatus` → the three-state `HealthState`. |

The RA must NOT enumerate the deployment environment — it receives keys as opaque strings and never interprets them. It will not know what `cloud-node-browser` is, and does not need to. Conversely the D10 collapse (only Healthy is green) is a *diagram* concern and belongs in the Manager, expressed over `RuntimeStatus`.

- [ ] **Step 1: Add the RA verb to the contract and regenerate**

Edit `.serviceContracts.operatedRuntimeAccess` in `.aiarch/state/project.json`: add a `ModelKeyHealth` `$def` (`ModelKey` string, `Status` → the existing `RuntimeStatus` enum) and the `GetDeploymentResourceHealth` operation. Repo conventions, already verified: operations use `"result"` + `"error": true` (never `"returns"`), and there is no `IdempotencyKey` `$def` — siblings inline it.

```bash
cd server && GOWORK=off make gen-models
```

Expect hand-written fakes and noop implementations elsewhere to break; fix them in this task.

This takes `operatedRuntimeAccess` to 7 ops — inside Appendix B's limit of 12, well clear of the ≥20 reject line.

- [ ] **Step 2: Write the failing RA test**

```go
func TestGetDeploymentResourceHealth_MapsPerResourceHealthToModelKeys(t *testing.T) {
	// Fixture: an Application CR whose status.resources[] carries one Healthy
	// Deployment and one Degraded Cluster, matching what render() emits for
	// testDesiredState().
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-mixed.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	byKey := map[string]RuntimeStatus{}
	for _, h := range got {
		byKey[h.ModelKey] = h.Status
	}
	if byKey["cloud-node-server-deployment"] != RuntimeStatusHealthy {
		t.Errorf("server = %v, want Healthy", byKey["cloud-node-server-deployment"])
	}
	// All three database model keys collapse onto the one Cluster resource and
	// must therefore all report its health.
	for _, k := range []string{"cloud-infra-operatedsystemstate", "cloud-infra-billingstate", "cloud-infra-usagelog"} {
		if byKey[k] != RuntimeStatusDegraded {
			t.Errorf("%s = %v, want Degraded (all three share one Cluster)", k, byKey[k])
		}
	}
}

func TestGetDeploymentResourceHealth_RenderedButAbsentFromClusterIsDegraded(t *testing.T) {
	// A manifest the renderer emits but the cluster does not report: it should
	// exist and does not. That is Degraded, never Healthy and never omitted.
	got, err := parseModelKeyHealth(loadArgoFixture(t, "application-missing-cluster.json"), testDesiredState())
	if err != nil {
		t.Fatalf("parseModelKeyHealth: %v", err)
	}
	for _, h := range got {
		if h.ModelKey == "cloud-infra-billingstate" && h.Status == RuntimeStatusHealthy {
			t.Error("a resource missing from the cluster must not report Healthy")
		}
	}
}
```

- [ ] **Step 3: Run and watch them fail**

```bash
cd server && GOWORK=off go test ./internal/resourceaccess/operatedruntime/ -run TestGetDeploymentResourceHealth -v
```

Expected: FAIL — `undefined: parseModelKeyHealth`.

- [ ] **Step 4: Implement the RA verb**

Internally: `render(desired)` → find the Application by the `archistrator.dev/app-id` annotation → `parseResourceHealth` → match each rendered manifest on `(Kind, Name, Namespace)` → emit one `ModelKeyHealth` per `ModelKey`, fanning out where a manifest carries several (the Postgres `Cluster` carries three).

Fail-closed rules, in keeping with the two fail-open checks this plan has already rejected:
- A rendered manifest with no matching live resource is **Degraded** — it should exist and does not. Never Healthy, never silently dropped.
- Any read failure propagates as an error. Never a Healthy default.

- [ ] **Step 5: Un-export `Manifest` and `ResourceHealth`**

Both were exported only so the Manager could join them. With the join inside the RA, neither needs to be. Rename to unexported forms and **remove both entries from `encapsulationAllowlistData` in `server/internal/arch_test.go`**.

This is the strongest signal the boundary is now right: the gate's exception surface shrinks rather than grows. Do not skip it because the tests already pass.

- [ ] **Step 6: Fold in two carried Minor findings from Task 9**

Same file, so close them here rather than in a separate pass:
- `GetApplicationHealth` discards its `fwra.Context` and builds the request with `http.NewRequest`. Use `http.NewRequestWithContext` so an activity deadline or cancellation actually propagates — today it is lost.
- A 200 response with valid JSON but no `items` key currently yields `Pending`, conflating "unexpected shape" with "never synced". Distinguish them — assert `kind == "ApplicationList"`, or detect the absent key — so an unexpected payload is an error rather than a benign-looking Pending.

- [ ] **Step 7: Add the public Manager op**

Add `QueryDeploymentHealth` to `.serviceContracts.operationsManager`, regenerate, and implement `QueryDeploymentHealthWorkflow`: read head-state, read the project, assemble the desired state, call the RA verb, then apply the Manager-side rules — every cloud-environment key the RA did not return is `HealthStateNeutral`; returned keys collapse `RuntimeStatusHealthy` → `HealthStateHealthy` and everything else → `HealthStateUnhealthy`.

- [ ] **Step 8: Write the Manager neutrality test**

This is the trap from spec §6 — a naive join paints the architect's own laptop red.

```go
func TestQueryDeploymentHealth_NeutralForNodesWeDoNotDeploy(t *testing.T) {
	got := applyDiagramHealth(
		[]operatedruntime.ModelKeyHealth{{ModelKey: "cloud-node-server-deployment", Status: operatedruntime.RuntimeStatusHealthy}},
		[]string{"cloud-node-server-deployment", "cloud-node-browser", "cloud-node-ns-gtd", "cloud-node-architect-machine"},
	)
	byKey := map[string]HealthState{}
	for _, n := range got.Nodes {
		byKey[n.ModelKey] = n.Health
	}
	if byKey["cloud-node-server-deployment"] != HealthStateHealthy {
		t.Errorf("server = %v, want Healthy", byKey["cloud-node-server-deployment"])
	}
	for _, k := range []string{"cloud-node-browser", "cloud-node-ns-gtd", "cloud-node-architect-machine"} {
		if byKey[k] != HealthStateNeutral {
			t.Errorf("%s = %v, want Neutral — we do not deploy it", k, byKey[k])
		}
	}
}
```

- [ ] **Step 9: Full suite, regenerate the client surface, commit**

```bash
cd server && GOWORK=off make test && GOWORK=off make gen-client
cd ../webApp && npm run gen:api && npm run check
git add .aiarch/state/project.json server/ webApp/src/contracts/ webApp/src/api/
git commit -m "feat(operations): QueryDeploymentHealth joining model keys to live health"
```

---

## Task 11: Hide operations in the local profile

**Files:**
- Modify: `server/cmd/server/hooks.go`
- Create: `webApp/src/utilities/capabilities.ts` (pure helper — no React, so it is unit-testable under `node --test`)
- Create: `webApp/src/utilities/capabilities.test.ts`
- Create: `webApp/src/hooks/useCapabilities.ts` (the TanStack Query hook)
- Modify: `webApp/src/routes/router.tsx`
- Modify: `webApp/src/components/AppShell.tsx`

**Interfaces:**
- Produces: `type Capabilities = { operations: boolean }`; `operationsEnabled(c: Capabilities | undefined): boolean` in `capabilities.ts`; `useCapabilities(): Capabilities | undefined` in `useCapabilities.ts`.
- Server: `GET /api/v1/capabilities → {"operations": bool}`.

- [ ] **Step 1: Write the failing webApp test**

```ts
import test from 'node:test';
import assert from 'node:assert/strict';
import { operationsEnabled } from './capabilities';

test('operations hidden when the server reports no operations capability', () => {
  assert.equal(operationsEnabled({ operations: false }), false);
});

test('operations hidden while capabilities are still loading', () => {
  assert.equal(operationsEnabled(undefined), false);
});

test('operations shown only when the server reports the capability', () => {
  assert.equal(operationsEnabled({ operations: true }), true);
});
```

The loading case defaults to hidden on purpose: a flash of an Operations tab that then vanishes is worse than a tab that appears a moment late.

- [ ] **Step 2: Run and watch it fail**

```bash
cd webApp && npm run test 2>&1 | head -20
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement**

Server: in `hooks.go`, mount the operations routes and report `operations: true` only when `resolveProfile()` returns cloud. webApp: `useCapabilities` hook, `operationsEnabled` helper, and gating in the router and nav.

- [ ] **Step 4: Verify both**

```bash
cd webApp && npm run check
cd ../server && GOWORK=off make test
```

Expected: PASS both.

- [ ] **Step 5: Verify by hand in the local profile**

Boot the server locally and confirm the Operations nav entry is absent and `/operations` does not route. Navigating there directly must not render a broken console.

- [ ] **Step 6: Commit**

```bash
git add server/cmd/server/hooks.go webApp/src/
git commit -m "feat(operations): hide the operations surface in the local profile"
```

---

## Task 12: Deployment diagram health overlay

**Files:**
- Create: `webApp/src/hooks/useDeploymentHealth.ts`
- Create: `webApp/src/components/flow/deploymentHealth.ts` (pure helpers)
- Create: `webApp/src/components/flow/deploymentHealth.test.ts`
- Modify: `webApp/src/components/flow/DeploymentFlow.tsx`
- Modify: `webApp/src/components/flow/DeploymentNodes.tsx`

**Interfaces:**
- Consumes: `QueryDeploymentHealth` (Task 10) — returns `{Nodes: [{modelKey, health}]}` where health is `Healthy` | `Unhealthy`, and any diagram node absent from the response is Neutral; `useCapabilities` (Task 11).
- Produces, all in `deploymentHealth.ts`:
  - `type HealthState = 'Healthy' | 'Unhealthy'`
  - `healthColorName(state: HealthState | undefined): 'green' | 'red' | 'neutral'` — pure, no theme dependency, which is what makes it testable under `node --test`
  - `environmentIsObservable(envKey: string): boolean` — true only for `'cloud'`
  - `healthColor(state: HealthState | undefined, t: Tokens): string` — maps `healthColorName` onto theme tokens; used by the components, not by the tests

- [ ] **Step 1: Write the failing test**

```ts
test('unknown model keys render neutral, never red', () => {
  assert.equal(healthColorName(undefined), 'neutral');
});

test('healthy renders green', () => {
  assert.equal(healthColorName('Healthy'), 'green');
});

test('unhealthy renders red', () => {
  assert.equal(healthColorName('Unhealthy'), 'red');
});

test('nodes in the local and test environments are never coloured', () => {
  assert.equal(environmentIsObservable('cloud'), true);
  assert.equal(environmentIsObservable('local'), false);
  assert.equal(environmentIsObservable('test'), false);
});
```

- [ ] **Step 2: Run and watch it fail**

```bash
cd webApp && npm run test 2>&1 | head -20
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement**

`useDeploymentHealth(operatedAppId)` fetches the health map and returns `Record<string, HealthState>`. `DeploymentFlow` threads it to `DeploymentNodes`, which colours a node's border by `healthColor(map[node.key])`. Only the `cloud` environment consults the map; `test` and `local` render exactly as they do today.

Gate the whole overlay on `useCapabilities().operations` — with operations hidden there is nothing to query, and the diagram must render unchanged rather than erroring.

- [ ] **Step 4: Verify**

```bash
cd webApp && npm run check
```

Expected: PASS.

- [ ] **Step 5: Verify visually**

Open the Deployment & Operations Model page and confirm, on the cloud environment: the server deployment carries a colour, and the architect's machine, the browser, the temporal namespace, and the gtd namespace are all neutral. Then switch to the local environment and confirm nothing is coloured at all.

- [ ] **Step 6: Commit**

```bash
git add webApp/src/
git commit -m "feat(webapp): live health overlay on the deployment diagram"
```

---

## Task 13: Cutover

Not code — a runbook, executed against the real cluster. Each step is reversible.

- [ ] **Step 1: Shadow**

Run the renderer against archistrator's real project state and diff the output against the four production charts one final time. Commit nothing to the software repo.

**Hand-check the rendered Argo `Application` against the four live ones.** This is the only object the automated golden diff does not cover, because production splits archistrator across four Applications and the renderer emits one — there is no counterpart to compare against. It is also the object that actually drives the cutover: everything else is applied *because* it says so. Five behaviour tests cover its shape (self-managed prune/sync/finalizer, tenant sync), but nothing compares it to what is live. Read it against `k8s/argocd/applications/archistrator-{server,webapp,postgres,gateway-routes}.yaml` and confirm the destination namespace, project, and repo URL match before going further.

- [ ] **Step 2: Register archistrator as an operated app**

Two inputs are easy to get wrong; both are corrected here from the original draft.

**The operated app id is DERIVED, not chosen** (spec D13). It is `uuidv5(namespace fa098c85-58b6-483e-8506-36045a008da7, projectId)`, realized once in `server/cmd/server/hooks.go`'s `OperatedAppIDForProject`. For `archistrator` that is:

```
b663aadc-9cc3-5069-b2bb-d360de9c6a10
```

Read it from the running server rather than trusting this document:

```bash
curl -s https://archistrator.capture-gtd.com/api/v1/projects/archistrator/operated-app-id
# {"operatedAppId":"b663aadc-9cc3-5069-b2bb-d360de9c6a10"}
```

The composition root **refuses** a registration whose id is not the derived one, so a mistyped id fails loudly instead of creating a head-state row nothing can ever address again. This same id is what the Operations console URL takes (`/operations/<operatedAppId>`) and what the deployment diagram's health overlay polls.

**The bundle ref is a content address, not an image tag.** `deployableBundleRef` is passed to `artifactAccess.retrieveConstructionOutput`, which resolves it to the bundle's bytes; those bytes must be a JSON object of exactly the shape assembly reads:

```json
{"serverImage":"ghcr.io/…/archistrator-server:0.8.16","webAppImage":"ghcr.io/…/archistrator-webapp:0.6.14"}
```

So Step 2 is really two acts: **store that JSON as a construction output, take the content address it returns, and register THAT**. Registering a bare image tag as the ref leaves the deploy in Step 3 failing at retrieval, and an empty or unrelated payload now fails loudly at assembly (`deployable bundle carries no serverImage`) rather than committing a Deployment with an empty `image:`.

Confirm afterwards that the head-state row exists with a non-empty `deployable_bundle_ref`.

**Scale and Update-autoscaler-policy are not part of this cutover.** Both are rejected by the operations façade and disabled in the console (spec §11) — desired state is assembled from the deployment model plus the bundle, and neither patch carries either. Deploy is the only publish.

- [ ] **Step 3: Parallel publish**

Deploy from the console. The server commits to `k8s/argocd/apps/archistrator/`. **No Application references that path yet**, so ArgoCD ignores it entirely. Read the committed YAML in the software repo at leisure and compare it against what is running.

- [ ] **Step 4: Flip**

In the software repo, delete the four `k8s/argocd/applications/archistrator-*.yaml` files and add the rendered Application. ArgoCD will show the app **OutOfSync and wait** — manual sync is the guard. Read the diff in the Argo UI, then click Sync.

**Prerequisite before this step:** the OIDC client-secret placeholder Secret must exist in the **`keycloak`** namespace as well as the app's namespace. The Keycloak operator requires the referenced Secret to sit alongside the Keycloak CR, and the CR is rendered into `keycloak` (spec §5.5). This is a new out-of-band step that has no equivalent in the current hand-managed setup.

**Expect a CrashLoop on first apply, and do not treat it as a failed cutover.** Production splits archistrator across four Applications carrying `sync-wave` annotations. The rendered form is one recursive Application with no waves, so the server `Deployment` and the CNPG `Cluster` sync together and the server will restart until Postgres accepts connections. It self-heals within a couple of minutes. If it has not settled after that, it is a real failure — go to Step 6.

- [ ] **Step 5: Verify**

Confirm archistrator is still reachable at `archistrator.capture-gtd.com` and the console shows healthy. Then open the Deployment & Operations Model page on the **cloud** environment and read the overlay against these expectations (the health join was corrected on 2026-08-08; before that fix two of these would have been red on a perfectly healthy cluster):

| Node | Expected once settled | Why |
|---|---|---|
| `cloud-node-server-deployment` | green | Deployment + Service, both health-checked by Argo |
| `cloud-infra-static-assets` | green | the webapp Deployment + Service |
| `cloud-infra-operatedsystemstate` / `-billingstate` / `-usagelog` | green | all three colour from the ONE CNPG `Cluster` |
| `cloud-infra-gateway` | green | HTTPRoutes + Envoy policies; several objects answer to this one key and the WORST wins, so any degraded route shows here |
| `cloud-infra-keycloak` | green | the `KeycloakRealmImport` — Argo has no health check for the kind, so its presence in the resource set is the verdict |
| `cloud-node-ns-archistrator` | green | the Argo `Application`'s OWN rollup (an Application never lists itself among its resources) |
| `cloud-node-architect-machine`, `cloud-node-browser`, `cloud-node-ns-temporal`, `cloud-node-ns-gtd`, `cloud-node-external` | neutral | archistrator does not deploy them |
| every node on the `test` and `local` environments | neutral | not observable at all |

Red on the first read is expected while the rollout settles (D10 has no amber: `Progressing` reads red). Red that persists after a couple of minutes is real — go to Step 6.

A node that is **neutral when the table says green** means the model key the renderer stamps no longer matches the diagram node's key — a rename in the deployment model, not a cluster problem.

- [ ] **Step 6: Rollback if needed**

`git revert` the software repo commit and click Sync. Because `prune: false` is set on archistrator's own Application — and Task 5 additionally omits the Argo finalizer when `SelfManaged`, and Task 7 hard-guards `Withdraw` against it — a bad render cannot have deleted anything. The worst case is an unapplied change.

Those three guards exist because they cover three genuinely different paths to the same outcome: a renderer that omits a manifest, a delete or rename of the Application object, and an operator clicking Withdraw. Any one of them alone leaves a hole.

A **fourth** guard covers the path the other three cannot: all three derive from the single `SelfManaged` boolean, so a publish arriving with it false would rewrite archistrator's own Application in tenant shape — `prune: true`, `selfHeal: true`, finalizer restored — and disarm all three in one commit. `gitOpsPublish` now refuses to overwrite a committed self-managed Application with a tenant-shaped one (or with one whose shape it cannot determine). If you ever see that refusal during cutover, do NOT work around it: it means the desired state being published does not believe archistrator is self-managed, and publishing it would arm the cluster to prune the control plane.

---

## Self-Review Notes

**Spec coverage:** D1/D2/D3 → Tasks 4–7. D4 → Task 2 + Task 4. D5 → Task 2. D6 → scope of the whole plan. D7 → Task 9. D8 → Task 5 Step 3 + Task 13. D9 → Task 11. D10 → Tasks 9–12. §4.1–4.3 contract changes → Tasks 1, 2, 8, 10. §4.4 relationship → Task 2 Step 2. §5.2 golden diff → Task 6. §5.3 gates → Task 6. §6 overlay → Tasks 10, 12. §7 security → Task 7 (credential) + Task 11 (profile). §10 cutover → Task 13.

**Known gaps, deliberately deferred to §11 of the spec:** built-app onboarding, SLO and cost attribution, tenant namespace guardrails, automated image-tag promotion, fleet re-render.

**Carried risk:** Task 6's golden diff is the task most likely to expand — the gateway-routes chart's OIDC arrangement is intricate, and reproducing it exactly is the difference between a working login and a broken one. Budget accordingly, and treat any unexplained difference as a blocker rather than a rounding error.
