# Structurizr/C4 Deployment Model + Validation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape the stored deployment topology to Structurizr's C4 deployment metamodel (deployment nodes instancing *containers*, plus infrastructure + external software-system nodes), validate it against the System components in the platform `methodcheck` gate, wire that gate to run over archistrator's own `project.json`, and rewrite archistrator's deployment data to the real k8s topology.

**Architecture:** The deployment topology declares `containers` once (each packaging System components by name); deployment nodes reference them via `containerInstance`. Non-component infra → `infrastructureNode`; external systems → `softwareSystemInstance`. The JSON shape is defined in TWO Go representations that must stay byte-identical — the server's authoritative structs and the platform validator's decode structs — plus a hand-written TS mirror. Validation lives in the platform `methodcheck` package (already home to `DEP-*` rules); archistrator gains a repo-root `TestMethod` that runs it over its own state.

**Tech Stack:** Go (two modules via `go.work`), React 19 + TypeScript + `@xyflow/react` (webApp), MUI 7.

## Global Constraints

- **Two repos, one workspace.** `/Users/davidmarne/mixofrealitystudio/archistrator` (server + webApp) and `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go` (the `methodcheck` validator). `archistrator/go.work` already `use`s the platform module locally, so platform changes are live in dev without a version bump.
- **JSON shape is defined in THREE places that must agree exactly:** `server/internal/resourceaccess/projectstate/models_phase1.go` (authoritative), `archistrator-platform/framework-go/methodcheck/project.go` (validator decode), `webApp/src/api/models.ts` (hand mirror). Field names = the Go `json:"…"` tags, verbatim.
- **Go `make` targets run `GOWORK=off`** (server builds against published platform tags). Plain `go test ./...` in a module uses the workspace.
- **Component references use the component NAME** (not id) — matches the existing author prompt and the architecture view. Validators/adapters resolve name → component.
- **No new architecture-model level.** Containers live in the deployment artifact only.
- **webApp checks:** `cd webApp && npx tsc --noEmit && npx eslint <files> && npx prettier --check <files>` must all pass per task.
- Commit messages end with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: Platform — container-based deployment structs in `methodcheck`

**Files:**
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/methodcheck/project.go:238-263`

**Interfaces:**
- Produces: Go types `DeployContainer`, `ContainerInstance{ContainerKey,Note,Tags}`, `InfrastructureNode`, `SoftwareSystemInstance`, and reshaped `DeploymentNode`/`DeploymentTopology` consumed by Task 2's rules.

- [ ] **Step 1: Replace the deployment structs.** In `project.go`, replace the block currently at lines 238-263 with:

```go
// DeploymentTopology is the typed C4 deployment model.
type DeploymentTopology struct {
	DeliveryStyle string                  `json:"deliveryStyle"`
	Containers    []DeployContainer       `json:"containers"`
	Environments  []DeploymentEnvironment `json:"environments"`
}

// DeployContainer is a deployable unit (C4 Container) packaging System components by name.
type DeployContainer struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Technology string   `json:"technology"`
	Description string   `json:"description"`
	Components []string `json:"components"` // System component NAMES
}

// DeploymentEnvironment is the set of nodes for one profile.
type DeploymentEnvironment struct {
	Profile string           `json:"profile"`
	Title   string           `json:"title"`
	Nodes   []DeploymentNode `json:"nodes"`
}

// DeploymentNode is a nestable C4 deployment node.
type DeploymentNode struct {
	Name                    string                   `json:"name"`
	Technology              string                   `json:"technology"`
	Description             string                   `json:"description"`
	Instances               int                      `json:"instances"`
	Tags                    []string                 `json:"tags"`
	Children                []DeploymentNode         `json:"children"`
	InfrastructureNodes     []InfrastructureNode     `json:"infrastructureNodes"`
	ContainerInstances      []ContainerInstance      `json:"containerInstances"`
	SoftwareSystemInstances []SoftwareSystemInstance `json:"softwareSystemInstances"`
}

// ContainerInstance instances a declared DeployContainer inside a node.
type ContainerInstance struct {
	ContainerKey string   `json:"containerKey"`
	Note         string   `json:"note"`
	Tags         []string `json:"tags"`
}

// InfrastructureNode is non-deployable infra (gateway, DB engine, broker).
type InfrastructureNode struct {
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// SoftwareSystemInstance is an external software system (GitHub, Anthropic, Keycloak).
type SoftwareSystemInstance struct {
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}
```

- [ ] **Step 2: Compile (expect rules to break).**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go && go build ./methodcheck/`
Expected: FAIL — `rules_deployment.go` references removed fields (`inst.ComponentID`, `n.Instances`). This is expected; Task 2 fixes it.

- [ ] **Step 3: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git add framework-go/methodcheck/project.go
git commit -m "methodcheck: reshape deployment structs to C4 container model

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Platform — retune the `DEP-*` rules for the container shape

**Files:**
- Modify: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/methodcheck/rules_deployment.go`
- Test: `/Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go/methodcheck/rules_deployment_test.go`

**Interfaces:**
- Consumes: Task 1 structs; existing helpers `componentIndex(s) map[string]Component`, `loc`, `Finding`, `SeverityError/Warning`, `profileCloud/Local/Test`, `styleCloud/Local/Both`, `sameSet`, `sortedComponentIDs`.
- Produces: rule IDs `DEP-CONTAINER-REF`, `DEP-MEMBER-EXIST`, `DEP-COVERAGE`, `DEP-PROFILE-SET`, `DEP-GRAPH-IDENTITY`, `DEP-NODE-WELLFORMED`; `deploymentConsistency(op, s) []Finding` (unchanged signature, called from `validateOperationalConcepts`).

- [ ] **Step 1: Add a component-name index helper.** In `rules_deployment.go`, add:

```go
// componentNameIndex maps System component NAME → Component (deployment refs by name).
func componentNameIndex(s System) map[string]Component {
	idx := make(map[string]Component, len(s.Components))
	for _, c := range s.Components {
		idx[c.Name] = c
	}
	return idx
}
```

- [ ] **Step 2: Write failing tests for the new rules.** Replace the deployment cases in `rules_deployment_test.go` (or add these) with fixtures on the new shape:

```go
func TestDeployment_ContainerRefMustResolve(t *testing.T) {
	s := System{Components: []Component{{ID: "billing-manager", Name: "Billing Manager", Kind: kindManager}}}
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleCloud,
		Containers:    []DeployContainer{{Key: "server", Name: "server", Components: []string{"Billing Manager"}}},
		Environments: []DeploymentEnvironment{
			{Profile: profileCloud, Title: "Cloud", Nodes: []DeploymentNode{
				{Name: "ns", ContainerInstances: []ContainerInstance{{ContainerKey: "MISSING"}}}}},
			{Profile: profileTest, Title: "Test", Nodes: []DeploymentNode{
				{Name: "p", ContainerInstances: []ContainerInstance{{ContainerKey: "server"}}}}},
		},
	}}
	got := deploymentConsistency(op, s)
	if !hasRule(got, ruleDepContainerRef) {
		t.Fatalf("expected DEP-CONTAINER-REF for missing container, got %v", got)
	}
}

func TestDeployment_MemberMustBeSystemComponent(t *testing.T) {
	s := System{Components: []Component{{ID: "billing-manager", Name: "Billing Manager", Kind: kindManager}}}
	op := OperationalConcepts{Deployment: DeploymentTopology{
		DeliveryStyle: styleCloud,
		Containers:    []DeployContainer{{Key: "server", Name: "server", Components: []string{"Ghost Manager"}}},
		Environments: []DeploymentEnvironment{
			{Profile: profileCloud, Title: "Cloud", Nodes: []DeploymentNode{{Name: "ns", ContainerInstances: []ContainerInstance{{ContainerKey: "server"}}}}},
			{Profile: profileTest, Title: "Test", Nodes: []DeploymentNode{{Name: "p", ContainerInstances: []ContainerInstance{{ContainerKey: "server"}}}}},
		},
	}}
	got := deploymentConsistency(op, s)
	if !hasRule(got, ruleDepMemberExist) {
		t.Fatalf("expected DEP-MEMBER-EXIST for unknown component, got %v", got)
	}
}
```

Add a `hasRule` test helper if one does not already exist:

```go
func hasRule(fs []Finding, id RuleID) bool {
	for _, f := range fs {
		if f.RuleID == id {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run tests (verify RED).**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go && go test ./methodcheck/ -run TestDeployment -v`
Expected: build error / FAIL (rules not yet reworked, `ruleDepContainerRef`/`ruleDepMemberExist` undefined).

- [ ] **Step 4: Rework the rule bodies.** Replace the rule constants and the flatten/env-check logic. New constants:

```go
const (
	ruleDepContainerRef   RuleID = "DEP-CONTAINER-REF"
	ruleDepMemberExist    RuleID = "DEP-MEMBER-EXIST"
	ruleDepProfileSet     RuleID = "DEP-PROFILE-SET"
	ruleDepGraphIdentity  RuleID = "DEP-GRAPH-IDENTITY"
	ruleDepCoverage       RuleID = "DEP-COVERAGE"
	ruleDepNodeWellformed RuleID = "DEP-NODE-WELLFORMED"
)
```

Replace `flattenInstances` with a container-key flattener:

```go
// flattenContainerKeys collects every containerInstance key in an env's node tree,
// and reports whether any deployment node has an empty Name.
func flattenContainerKeys(nodes []DeploymentNode) (keys []string, emptyNodeName bool) {
	for _, n := range nodes {
		if n.Name == "" {
			emptyNodeName = true
		}
		for _, ci := range n.ContainerInstances {
			keys = append(keys, ci.ContainerKey)
		}
		childKeys, childEmpty := flattenContainerKeys(n.Children)
		keys = append(keys, childKeys...)
		if childEmpty {
			emptyNodeName = true
		}
	}
	return keys, emptyNodeName
}
```

Rework `deploymentConsistency`: validate container membership against System components once (topology-level), then per-env validate container-key references and build the covered-component set as the union of instanced containers' members.

```go
func deploymentConsistency(op OperationalConcepts, s System) []Finding {
	topo := op.Deployment
	if len(topo.Environments) == 0 {
		return nil
	}
	nameIdx := componentNameIndex(s)
	var out []Finding

	// DEP-MEMBER-EXIST: every container member resolves to a System component.
	containersByKey := make(map[string]DeployContainer, len(topo.Containers))
	for _, c := range topo.Containers {
		containersByKey[c.Key] = c
		for _, member := range c.Components {
			if _, ok := nameIdx[member]; !ok {
				out = append(out, Finding{
					RuleID:   ruleDepMemberExist,
					Severity: SeverityError,
					Message:  fmt.Sprintf("container %q packages %q, which is not a System component", c.Key, member),
					Location: loc(0, "deployment topology"),
				})
			}
		}
	}

	byProfile := make(map[string]envSet)
	presentProfiles := make(map[string]bool)
	for i, env := range topo.Environments {
		ordinal := i + 1
		presentProfiles[env.Profile] = true
		covered, envFindings := checkDeploymentEnvironment(env, ordinal, containersByKey)
		out = append(out, envFindings...)
		byProfile[env.Profile] = envSet{ordinal: ordinal, set: covered}
	}
	out = append(out, checkProfileSets(presentProfiles, expectedProfiles(topo.DeliveryStyle))...)
	out = append(out, checkCrossProfileCoverage(byProfile, internalComponentNames(s))...)
	return out
}

// checkDeploymentEnvironment validates container-key refs and returns the SET of
// System component NAMES covered by the containers instanced in this env.
func checkDeploymentEnvironment(env DeploymentEnvironment, ordinal int, containersByKey map[string]DeployContainer) (map[string]bool, []Finding) {
	section := fmt.Sprintf("deployment environment %q", profileName(env.Profile))
	var out []Finding
	keys, emptyNodeName := flattenContainerKeys(env.Nodes)
	if emptyNodeName {
		out = append(out, Finding{RuleID: ruleDepNodeWellformed, Severity: SeverityError,
			Message: fmt.Sprintf("%s: a deployment node has an empty Name", section), Location: loc(ordinal, section)})
	}
	if len(keys) == 0 {
		out = append(out, Finding{RuleID: ruleDepNodeWellformed, Severity: SeverityError,
			Message: fmt.Sprintf("%s: environment instances no containers", section), Location: loc(ordinal, section)})
	}
	covered := make(map[string]bool)
	for _, key := range keys {
		c, ok := containersByKey[key]
		if !ok {
			out = append(out, Finding{RuleID: ruleDepContainerRef, Severity: SeverityError,
				Message: fmt.Sprintf("%s: containerInstance %q does not reference a declared container", section, key), Location: loc(ordinal, section)})
			continue
		}
		for _, member := range c.Components {
			covered[member] = true
		}
	}
	return covered, out
}
```

Add a name-keyed internal-component set (parallels the removed `internalComponents`):

```go
func internalComponentNames(s System) map[string]bool {
	internal := make(map[string]bool)
	for _, c := range s.Components {
		if isRunningComponent(c.Kind) {
			internal[c.Name] = true
		}
	}
	return internal
}
```

Keep `checkProfileSets`, `checkCrossProfileCoverage`, `checkTestEnvCoverage`, `checkProfileEnvCoverage`, `sameSet`, `sortedComponentIDs`, `expectedProfiles`, `profileName`, `envSet` as-is — they operate on the covered-name sets, which now hold component names instead of ids (semantics identical). Delete the now-unused `internalComponents` and the old `flattenInstances`.

- [ ] **Step 5: Run tests (verify GREEN).**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator-platform/framework-go && go build ./methodcheck/ && go test ./methodcheck/ -v`
Expected: PASS (all methodcheck tests, including the two new ones).

- [ ] **Step 6: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator-platform
git add framework-go/methodcheck/rules_deployment.go framework-go/methodcheck/rules_deployment_test.go
git commit -m "methodcheck: retune DEP-* rules for C4 container deployment model

DEP-CONTAINER-REF (instance→declared container), DEP-MEMBER-EXIST (container
member→System component by name); coverage/profile/identity rules operate on the
covered component-name set.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Server — mirror the container structs (authoritative) + shapegen

**Files:**
- Modify: `server/internal/resourceaccess/projectstate/models_phase1.go:178-208`
- Modify: `server/cmd/shapegen/main.go:238-272`

**Interfaces:**
- Produces: the authoritative `DeploymentTopology`/`DeployContainer`/`DeploymentNode`/`ContainerInstance`/`InfrastructureNode`/`SoftwareSystemInstance` structs (json tags identical to Task 1). These define what `DecodeProjectJSON` accepts.

- [ ] **Step 1: Replace the structs.** In `models_phase1.go`, replace lines 178-208 (`ContainerInstance` through `DeploymentTopology`) with the same field set as Task 1 Step 1, but keep `ComponentID`/`DeploymentProfile` Go types where they already exist. Use the typed `DeploymentProfile` for `DeploymentEnvironment.Profile` and typed `DeliveryStyle` for `DeploymentTopology.DeliveryStyle` (the enums encode to the wire strings via `enumjson.go`):

```go
// DeployContainer is a deployable unit (C4 Container) packaging System Components by name.
type DeployContainer struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Technology string   `json:"technology"`
	Description string   `json:"description"`
	Components []string `json:"components"` // System Component NAMES
}

// ContainerInstance instances a declared DeployContainer inside a node.
type ContainerInstance struct {
	ContainerKey string   `json:"containerKey"`
	Note         string   `json:"note"`
	Tags         []string `json:"tags"`
}

type InfrastructureNode struct {
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type SoftwareSystemInstance struct {
	Name        string   `json:"name"`
	Technology  string   `json:"technology"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type DeploymentNode struct {
	Name                    string                   `json:"name"`
	Technology              string                   `json:"technology"`
	Description             string                   `json:"description"`
	Instances               int                      `json:"instances"`
	Tags                    []string                 `json:"tags"`
	Children                []DeploymentNode         `json:"children"`
	InfrastructureNodes     []InfrastructureNode     `json:"infrastructureNodes"`
	ContainerInstances      []ContainerInstance      `json:"containerInstances"`
	SoftwareSystemInstances []SoftwareSystemInstance `json:"softwareSystemInstances"`
}

type DeploymentEnvironment struct {
	Profile DeploymentProfile `json:"profile"`
	Title   string            `json:"title"`
	Nodes   []DeploymentNode  `json:"nodes"`
}

type DeploymentTopology struct {
	DeliveryStyle DeliveryStyle           `json:"deliveryStyle"`
	Containers    []DeployContainer       `json:"containers"`
	Environments  []DeploymentEnvironment `json:"environments"`
}
```

- [ ] **Step 2: Update the shapegen example.** In `server/cmd/shapegen/main.go`, replace the `Deployment:` block (238-272) so it declares a container and instances it:

```go
Deployment: projectstate.DeploymentTopology{
	DeliveryStyle: projectstate.StyleCloud,
	Containers: []projectstate.DeployContainer{
		{Key: "server", Name: "server", Technology: "Go", Description: "the application server", Components: []string{"System Design Manager"}},
	},
	Environments: []projectstate.DeploymentEnvironment{
		{Profile: projectstate.ProfileCloud, Title: "Cloud", Nodes: []projectstate.DeploymentNode{
			{Name: "Kubernetes cluster", Technology: "k8s", Children: []projectstate.DeploymentNode{
				{Name: "app namespace", Technology: "k8s-namespace", Instances: 2,
					ContainerInstances: []projectstate.ContainerInstance{{ContainerKey: "server", Note: "app pod"}}},
			}},
		}},
		{Profile: projectstate.ProfileTest, Title: "Test", Nodes: []projectstate.DeploymentNode{
			{Name: "test process", Technology: "in-process",
				ContainerInstances: []projectstate.ContainerInstance{{ContainerKey: "server", Note: "embedded test server"}}},
		}},
	},
},
```

- [ ] **Step 3: Build the server.**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && GOWORK=off go build ./...`
Expected: PASS (may surface other references to the old fields — fix any compile errors by updating call sites to the new field names; there should be none outside the model + shapegen).

- [ ] **Step 4: Regenerate the shape example + OpenAPI.**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator/server && go run ./cmd/shapegen && GOWORK=off make gen-client`
Expected: PASS; `…/software/.git/sdd/archistrator-shapes.json` and `server/api/openapi.yaml` regenerate (model payloads stay opaque, so no TS type churn).

- [ ] **Step 5: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/resourceaccess/projectstate/models_phase1.go server/cmd/shapegen/main.go server/api/openapi.yaml
git commit -m "projectstate: C4 container deployment structs (match methodcheck)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: webApp — update the hand-mirror `models.ts`

**Files:**
- Modify: `webApp/src/api/models.ts:198-220`

**Interfaces:**
- Produces: TS interfaces `DeployContainer`, `ContainerInstance{containerKey,note,tags?}`, `InfrastructureNode`, `SoftwareSystemInstance`, reshaped `DeploymentNode`, `DeploymentTopology{deliveryStyle,containers,environments}` consumed by Task 5's adapter.

- [ ] **Step 1: Replace the deployment interfaces.** In `models.ts`, replace `ContainerInstance` through `DeploymentTopology` (198-220) with:

```ts
export interface DeployContainer {
  key: string;
  name: string;
  technology: string;
  description: string;
  components: string[] | null;
}

export interface ContainerInstance {
  containerKey: string;
  note: string;
  tags?: string[] | null;
}

export interface InfrastructureNode {
  name: string;
  technology: string;
  description: string;
  tags?: string[] | null;
}

export interface SoftwareSystemInstance {
  name: string;
  technology: string;
  description: string;
  tags?: string[] | null;
}

export interface DeploymentNode {
  name: string;
  technology: string;
  description: string;
  instances: number;
  tags?: string[] | null;
  children: DeploymentNode[] | null;
  infrastructureNodes: InfrastructureNode[] | null;
  containerInstances: ContainerInstance[] | null;
  softwareSystemInstances: SoftwareSystemInstance[] | null;
}

export interface DeploymentEnvironment {
  profile: DeploymentProfile;
  title: string;
  nodes: DeploymentNode[] | null;
}

export interface DeploymentTopology {
  deliveryStyle: DeliveryStyle;
  containers: DeployContainer[] | null;
  environments: DeploymentEnvironment[] | null;
}
```

- [ ] **Step 2: Typecheck (expect adapter break).**

Run: `cd webApp && npx tsc --noEmit`
Expected: FAIL in `adapters.ts` (old `node.instances`/`inst.componentId` references) — fixed in Task 5.

- [ ] **Step 3: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add webApp/src/api/models.ts
git commit -m "webApp: mirror C4 container deployment model in models.ts

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: webApp — adapter builds the container deployment view

**Files:**
- Modify: `webApp/src/api/adapters.ts:406-480`

**Interfaces:**
- Consumes: Task 4 interfaces.
- Produces: `DeploymentNodeView { name; technology; description; instances; children; infrastructure: InfraView[]; externals: ExternalView[]; containers: ContainerInstanceView[] }`, `ContainerInstanceView { key; name; technology; description; note; components: {name; layer: Layer}[] }`, `InfraView`, `ExternalView`, and `toDeploymentView(...)` / `listDeploymentProfiles(...)` (same names) consumed by Tasks 6/8.

- [ ] **Step 1: Replace the view types + `toDeploymentView`.** Rewrite the region so each node carries container instances resolved to their packaged components (name+layer), plus infra/external nodes:

```ts
export interface ComponentRef { name: string; layer: Layer }

export interface ContainerInstanceView {
  key: string;
  name: string;
  technology: string;
  description: string;
  note: string;
  components: ComponentRef[]; // packaged System components (resolved), for the hover/expand list
}

export interface InfraView { name: string; technology: string; description: string }
export interface ExternalView { name: string; technology: string; description: string }

export interface DeploymentNodeView {
  name: string;
  technology: string;
  description: string;
  instances: number;
  children: DeploymentNodeView[];
  containers: ContainerInstanceView[];
  infrastructure: InfraView[];
  externals: ExternalView[];
}

export interface DeploymentProfileRef { profile: DeploymentProfile; title: string }

export function listDeploymentProfiles(
  opEnvelope: ArtifactModelEnvelope | undefined
): DeploymentProfileRef[] {
  const op = narrow(opEnvelope, 'operationalConcepts');
  return (op?.deployment?.environments ?? []).map((e) => ({ profile: e.profile, title: e.title }));
}

export function toDeploymentView(
  opEnvelope: ArtifactModelEnvelope | undefined,
  systemEnvelope: ArtifactModelEnvelope | undefined,
  profile: DeploymentProfile
): DeploymentNodeView[] | undefined {
  const op = narrow(opEnvelope, 'operationalConcepts');
  const topo = op?.deployment;
  const env = (topo?.environments ?? []).find((e) => e.profile === profile);
  if (env === undefined) return undefined;

  const system = narrow(systemEnvelope, 'system');
  const byName = new Map<string, Layer>();
  for (const c of system?.components ?? []) byName.set(c.name, c.layer);

  const containersByKey = new Map<string, DeployContainer>();
  for (const c of topo?.containers ?? []) containersByKey.set(c.key, c);

  const resolveContainer = (ci: ContainerInstance): ContainerInstanceView => {
    const c = containersByKey.get(ci.containerKey);
    return {
      key: ci.containerKey,
      name: c?.name ?? ci.containerKey,
      technology: c?.technology ?? '',
      description: c?.description ?? '',
      note: ci.note,
      components: (c?.components ?? []).map((n) => ({ name: n, layer: byName.get(n) ?? 'utility' })),
    };
  };

  const mapNode = (node: DeploymentNode): DeploymentNodeView => ({
    name: node.name,
    technology: node.technology,
    description: node.description,
    instances: node.instances > 0 ? node.instances : 1,
    children: (node.children ?? []).map(mapNode),
    containers: (node.containerInstances ?? []).map(resolveContainer),
    infrastructure: (node.infrastructureNodes ?? []).map((n) => ({ name: n.name, technology: n.technology, description: n.description })),
    externals: (node.softwareSystemInstances ?? []).map((n) => ({ name: n.name, technology: n.technology, description: n.description })),
  });

  return (env.nodes ?? []).map(mapNode);
}
```

Add the imports at the top of `adapters.ts` if not present: `DeployContainer`, `ContainerInstance`, `DeploymentNode` from `./models`.

- [ ] **Step 2: Typecheck + lint.**

Run: `cd webApp && npx tsc --noEmit && npx eslint src/api/adapters.ts`
Expected: `tsc` PASS; eslint may flag the deployment renderer (still on the old view) — that is Task 6/8.

- [ ] **Step 3: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add webApp/src/api/adapters.ts
git commit -m "webApp: adapter resolves container instances + packaged components

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: webApp — container/infra/external node types + instance badges

**Files:**
- Modify: `webApp/src/components/flow/DeploymentNodes.tsx`
- Modify: `webApp/src/components/flow/DeploymentFlow.tsx`

**Interfaces:**
- Consumes: Task 5 view types.
- Produces: React Flow node types `deployGroup` (adds `×N` badge + description), `deployContainer` (C4 container box with packaged-components hover/expand), `deployInfra`, `deployExternal`; a `DeploymentFlow` `measure`/`emit` that lays out container/infra/external boxes (one per row, wrapping) instead of per-layer component rows.

- [ ] **Step 1: Add the node components.** In `DeploymentNodes.tsx`, add `DeployContainerNode` (name, `[Container: technology]`, description, and a "packages N components" line that expands on hover/click to list `components[].name` colored by layer via `layerColors(t)`), `DeployInfraNode`, and `DeployExternalNode` (dashed border). Keep `DeployGroupNode`, adding a `×{instances}` chip in the header when `instances > 1` and an optional description line. Remove `DeployLayerLabelNode` (no longer used by deployment). Full component code follows the existing MUI/token patterns in this file (fixed width from `NodeProps`, `useTokens()`); model each box on `DeployInstanceNode`'s structure.

*(Implementer: reuse `layerColors(t)` from `./flowLayout` for the component chips; the container box is the primary unit, the component list is secondary/expandable.)*

- [ ] **Step 2: Rework `DeploymentFlow` layout.** In `DeploymentFlow.tsx`: replace the per-layer-row bucketing (added in commit `f6bde0b`) with a simpler layout — a `deployGroup` holds its `deployContainer` / `deployInfra` / `deployExternal` boxes wrapped left-to-right (a small grid), then nested child groups below. Register the new `nodeTypes`. Recompute group width/height bottom-up to fit the wrapped boxes (keep the existing `measure`/`emit` two-pass structure and `fitView`).

- [ ] **Step 3: Typecheck + lint + format.**

Run: `cd webApp && npx tsc --noEmit && npx eslint src/components/flow/DeploymentNodes.tsx src/components/flow/DeploymentFlow.tsx && npx prettier --write src/components/flow/DeploymentNodes.tsx src/components/flow/DeploymentFlow.tsx`
Expected: all PASS.

- [ ] **Step 4: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add webApp/src/components/flow/DeploymentNodes.tsx webApp/src/components/flow/DeploymentFlow.tsx
git commit -m "webApp: C4 container/infra/external deployment nodes + instance badges

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: archistrator — wire the `methodcheck` gate over its own project.json (RED)

**Files:**
- Create: `method_test.go` at the archistrator repo root (`/Users/davidmarne/mixofrealitystudio/archistrator/method_test.go`)
- Modify: `server/Makefile` (add a `project-check` target) and `.github/workflows/server-checks.yml`

**Interfaces:**
- Consumes: platform `methodcheck.Check` + `arch.MethodSpec` (Tasks 1-2 shape).

- [ ] **Step 1: Add the self-gate test** (mirrors the downstream template, RepoRoot ".", module = the server module path):

```go
package method_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// TestMethod runs the platform Method design gate over archistrator's OWN
// .aiarch/state/project.json (the repo dogfoods its own Method state).
func TestMethod(t *testing.T) {
	methodcheck.Check(t, methodcheck.ProjectSpec{
		RepoRoot: ".",
		Arch:     arch.MethodSpec(".", "github.com/mixofreality-studio/archistrator/server/"),
	})
}
```

(If a root `go.mod` does not exist, place this test in the module that can import `methodcheck` and whose test working directory reaches the repo-root `.aiarch/state/project.json`; confirm the import path of the server module with `head -1 server/go.mod`.)

- [ ] **Step 2: Run the gate (verify RED).**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator && go test . -run TestMethod -v`
Expected: FAIL — the current `project.json` still has the OLD deployment shape (no `containers`), so `DEP-NODE-WELLFORMED`/`DEP-CONTAINER-REF` and the `project-manager` issue surface. This red gate is the test for Task 8.

- [ ] **Step 3: Add the Make target + CI job.** In `server/Makefile` add:

```make
# Run the platform Method DESIGN gate over this repo's own project.json.
project-check:
	cd .. && go test . -run TestMethod -v
```

In `.github/workflows/server-checks.yml`, add a step after `make test-short` that runs `go test . -run TestMethod -v` from the repo root (workspace on, so it resolves the local `framework-go`).

- [ ] **Step 4: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add method_test.go server/Makefile .github/workflows/server-checks.yml
git commit -m "archistrator: run methodcheck design gate over own project.json

Dogfoods the platform Method design rules (incl. DEP-*) on archistrator's own
.aiarch/state/project.json. Currently RED until the deployment slot is migrated.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: archistrator — rewrite the deployment data to the real topology (GREEN)

**Files:**
- Modify: `.aiarch/state/project.json` → `.slots['6'].model.deployment`

**Interfaces:**
- Consumes: Task 3 wire shape; Task 7 gate.

- [ ] **Step 1: Rewrite the deployment slot** to the container shape from the spec (`docs/superpowers/specs/2026-07-02-structurizr-deployment-model-design.md` §2). Declare `containers`: `archistrator-server` (Go · distroless; packages mcp-client, scheduler-client, every Manager/Engine/ResourceAccess/Utility by their exact System names), `archistrator-webapp` (nginx · React SPA; packages `Web Client`), `archistrator-postgres` (CloudNativePG · Postgres 16; packages `Operated System State`, `Billing State`, `Usage Log` — use each Resource component's exact System name). Build the node tree: `Mixofreality Kubernetes Cluster` → {`gtd namespace`(InfrastructureNode: Envoy Gateway), `archistrator namespace` → server/webapp/postgres Deployment nodes instancing the containers with `instances` 2/2/1, `temporal namespace`(InfrastructureNode: Temporal Cluster)}; sibling external nodes for GitHub (`softwareSystemInstances`) and Anthropic; Keycloak as external. Provide `test` env instancing the same containers in a single `test process` node. **Do NOT emit `project-manager`.** Use the committed `system` slot (`.slots['5'].model.components[].name`) as the exact source of component names.

*(Implementer: enumerate the real names first — `jq '.slots[] | select(.model.components) | .model.components[].name' .aiarch/state/project.json` — then assign each to exactly one container so `DEP-COVERAGE` passes.)*

- [ ] **Step 2: Run the gate (verify GREEN).**

Run: `cd /Users/davidmarne/mixofrealitystudio/archistrator && go test . -run TestMethod -v`
Expected: PASS — zero Error findings (DEP-MEMBER-EXIST / DEP-CONTAINER-REF / DEP-COVERAGE / DEP-GRAPH-IDENTITY / DEP-PROFILE-SET all satisfied). Fix names until green.

- [ ] **Step 3: Round-trip stability.**

Run: `cd server && GOWORK=off go run ./cmd/validate ../.aiarch/state/project.json`
Expected: decode + re-encode byte-stable; committed-slot report unchanged except the deployment slot.

- [ ] **Step 4: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add .aiarch/state/project.json
git commit -m "state: migrate deployment to C4 container topology; drop project-manager

Rewrites the deployment slot to the real k8s topology (archistrator-server /
-webapp / -postgres containers + infra/external nodes) and removes the dangling
project-manager reference. The methodcheck design gate is now green.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 9: server — teach the author prompt the new shape

**Files:**
- Modify: `server/internal/manager/systemdesign/prompts.go:172`

**Interfaces:**
- Consumes: Task 3 wire shape.

- [ ] **Step 1: Rewrite the deployment paragraph** so the LLM emits the container shape: first declare `containers` (each with `key`, `name`, `technology`, `description`, and `components` = the exact System component NAMES it packages — every running component packaged in exactly one container); then nest `deploymentNodes` whose `containerInstances` reference containers by `containerKey`, with `instances` multipliers; use `infrastructureNodes` for non-component infra and `softwareSystemInstances` for external systems. Keep the cross-profile invariant (identical container set across cloud/local; test instances them all) and the "reference by NAME" rule (now honored by the validator).

- [ ] **Step 2: Build.**

Run: `cd server && GOWORK=off go build ./... && GOWORK=off go test ./internal/manager/systemdesign/ -run Prompt`
Expected: PASS (prompt is a string; any prompt snapshot test updates accordingly).

- [ ] **Step 3: Commit.**

```bash
cd /Users/davidmarne/mixofrealitystudio/archistrator
git add server/internal/manager/systemdesign/prompts.go
git commit -m "systemdesign: author prompt emits C4 container deployment shape

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 10: Verify end-to-end in the running app

**Files:** none (verification only).

- [ ] **Step 1: Boot + view.** Run the app on real state (per the run skill: `GOWORK=off`, `CONSTRUCTION_DRYRUN`, local-git substrate on branch `main`), open the System Design → Operational Concepts → DEPLOYMENT view, both profiles.
- [ ] **Step 2: Confirm** the diagram shows container boxes (`archistrator-server` ×2, `archistrator-webapp` ×2, `archistrator-postgres`) inside the namespace nodes, infra nodes (Envoy Gateway, Temporal) and external nodes (GitHub, Anthropic, Keycloak), instance `×N` badges, and that hovering/expanding a container reveals its packaged components colored by layer. Screenshot both profiles.
- [ ] **Step 3: STOP for review** (per the founder's UI review loop) before merging.

---

## Platform release note

For local dev, `go.work` makes the `framework-go` changes (Tasks 1-2) live immediately. To ship: release `framework-go` (bump from `v0.3.0`) and bump `server/go.mod`'s require to the new tag so CI (which builds `GOWORK=off` against the published tag) sees the new `methodcheck` rules. Sequence this at merge time.

## Self-review notes

- Spec coverage: model reshape (T1,T3,T4), validation retune + self-gate (T2,T7), data rewrite + project-manager deletion (T8), renderer incl. packaged-components hover/expand + postgres-as-container (T6,T8), generation prompt (T9), verify (T10). All spec sections mapped.
- Container membership + coverage are validated by name in both the platform gate and the adapter, consistent with the "reference by name" constraint.
