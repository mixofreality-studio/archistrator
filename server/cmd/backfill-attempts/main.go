// cmd/backfill-attempts derives TaskAttempt records for the activities of the committed
// plan whose construction is established by evidence that exists today — the code in
// this repository, and a founder sign-off recorded against a committed artifact.
//
// It is a ONE-SHOT tool whose output is a REVIEWABLE COMMITTED DIFF. No server code
// path may fabricate an attempt at request time; that is rule 4 of the provenance
// contract, and it is what stops "the UI generates plausible history" from becoming
// permanent architecture. Synthesis that lives in a read path is invisible, unversioned
// and unrevertable; synthesis that lives in a commit is none of those things.
//
// Every attempt it writes is stamped OriginBackfilled — never observed: nobody watched
// any of it run — with a basis naming what it was derived from, and every attempt is run
// through AttemptProvenance.Validate before anything is written. Activities with no
// evidence get NO attempts: absence stays absence, and the pump will pick them up as
// real work. Every re-run re-examines every activity, so an activity this tool
// backfilled whose evidence has since regressed is caught: the run refuses, writing
// nothing, and names it with the condition it now fails (see deQualified).
//
// There are exactly three evidence paths, and no fourth:
//
//   - CODE (a component). The founder ruled (2026-09-09) "assume any component that is
//     fully implemented is done and reviewed and integrated", and ruled (F2) "if they're
//     not implemented in code, then leave them as real work that still needs to be
//     done". "Fully implemented" is read off the repository — see fullyImplemented.
//   - INFERRED (a Resource). A Resource has no code of its own in this repository; it
//     qualifies iff every ResourceAccess with a slot-5 relationship to it qualifies on
//     code, and its basis says the evidence is inferred.
//   - SIGN-OFF. A founder sign-off recorded verbatim against a committed artifact (see
//     founderSignOffs). It qualifies an activity only while that artifact exists.
//
// Usage (from server/):
//
//	GOWORK=off go run ./cmd/backfill-attempts -repo .. [-dry-run]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// statePath is the project.json location relative to the repository root. It is
// compiler input as much as it is state (it drives the Go contract layer, the OpenAPI
// document and the TS client), which is why this tool rewrites exactly one member of it.
var statePath = filepath.Join(".aiarch", "state", "project.json")

// serverDir is where every .serviceContracts goPackage is rooted, relative to the repo.
const serverDir = "server"

// generatorID identifies this tool in every provenance stamp it writes.
const generatorID = "cmd/backfill-attempts"

// constructionMember is the one top-level project.json member this tool may write.
const constructionMember = "activityConstruction"

// founderRuling is the ruling that turns "fully implemented" into "done, reviewed and
// integrated", quoted verbatim so the sentence a reader finds in a committed basis is
// the sentence the founder actually said — not a paraphrase this tool invented.
const founderRuling = "assume any component that is fully implemented is done and reviewed and integrated"

// founderRulingRef is how that ruling is cited inside a basis string.
const founderRulingRef = "founderRuling[2026-09-09]=" + founderRuling

// founderSignOff is a founder decision, recorded verbatim, that an activity's committed
// artifact is accepted. It is DATA — an entry here is a decision someone made, with the
// words they used — and it is the evidence path for an activity whose product is an
// artifact rather than code. A sign-off never stands alone: the artifact it approved
// must still exist in the committed state, or the activity does not qualify.
type founderSignOff struct {
	ActivityID string
	Date       string
	Quote      string
	Artifact   signedArtifact
}

// signedArtifact names the committed artifact a sign-off approved and reads it.
type signedArtifact struct {
	// Ref is where the artifact lives in project.json, e.g. "testingState.systemTestPlan".
	Ref string
	// Describe reads the artifact out of the committed state and returns a short,
	// countable description of it ("5 scenarios"). It fails when the artifact is absent
	// or empty — a sign-off on nothing is not evidence.
	Describe func(projectstate.Project) (string, error)
}

// systemTestPlanArtifact is .testingState.systemTestPlan, described by its scenario count.
var systemTestPlanArtifact = signedArtifact{
	Ref: "testingState.systemTestPlan",
	Describe: func(p projectstate.Project) (string, error) {
		if p.TestingState == nil || p.TestingState.SystemTestPlan == nil {
			return "", errors.New("no .testingState.systemTestPlan is committed")
		}
		n := len(p.TestingState.SystemTestPlan.Scenarios)
		if n == 0 {
			return "", errors.New(".testingState.systemTestPlan holds no scenarios")
		}
		return fmt.Sprintf("%d scenarios", n), nil
	},
}

// founderSignOffs is every recorded founder sign-off.
//
// N-STP (2026-09-12): the founder reviewed the system test plan in the app at
// /project/archistrator/construction?lens=list&a=N-STP and said, verbatim, "stp looks
// good in the app. sign off. continue".
var founderSignOffs = []founderSignOff{{
	ActivityID: "N-STP",
	Date:       "2026-09-12",
	Quote:      "stp looks good in the app. sign off. continue",
	Artifact:   systemTestPlanArtifact,
}}

// signOffBasis is the provenance basis of a sign-off: the artifact (counted from the
// file) and the decision, in the founder's words.
func signOffBasis(s founderSignOff, description string) string {
	return fmt.Sprintf("%s (%s) + founderSignOff[%s]=%q", s.Artifact.Ref, description, s.Date, s.Quote)
}

// verdict is one activity's outcome: whether it qualifies, why, and — when it does —
// what its attempts cite.
type verdict struct {
	ActivityID string
	Qualifies  bool
	// Reason is the failing condition when the activity does not qualify, and the
	// evidence path when it does. It is the run report's line for the activity.
	Reason string
	// Basis is the provenance basis stamped on every attempt (qualifying only).
	Basis string
	// ContractRef / CodeRef back the tasks that were read off an artifact: the
	// component's contract key (detailed design + its review) and the commit the code was
	// read at (construction + code review). ArtifactRef backs every task of a sign-off.
	ContractRef string
	CodeRef     string
	ArtifactRef string
	// Files are the repo-relative files a code verdict read; the run refuses to cite
	// HEAD for them unless they are unmodified there.
	Files []string
}

// inputs is everything the evaluation reads: the decoded committed state, where the
// server tree is, and the commit that tree is at.
type inputs struct {
	Project projectstate.Project
	// ServerRoot is the directory every goPackage is resolved under.
	ServerRoot string
	// Head is the commit the server tree was read at; it is cited in every code basis.
	Head string
}

// planOf returns the committed activity list and System, refusing when either is missing:
// without them there is no activity to evaluate and no component to read.
func planOf(p projectstate.Project) (projectstate.ActivityList, projectstate.System, error) {
	list, ok := p.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || list == nil {
		return projectstate.ActivityList{}, projectstate.System{}, fmt.Errorf("slot activityList holds %T, not an ActivityList", p.ActivityList.Model)
	}
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return projectstate.ActivityList{}, projectstate.System{}, fmt.Errorf("slot systemDesign holds %T, not a System", p.SystemDesign.Model)
	}
	return *list, *sys, nil
}

// evaluate decides every activity of the committed plan, in plan order.
func evaluate(in inputs) ([]verdict, error) {
	list, sys, err := planOf(in.Project)
	if err != nil {
		return nil, err
	}
	components := make(map[string]projectstate.Component, len(sys.Components))
	for _, c := range sys.Components {
		components[c.ID] = c
	}
	activityFor := map[string]string{} // componentId -> activity id
	for _, a := range list.Activities {
		if a.ComponentID != "" {
			activityFor[a.ComponentID] = a.Name
		}
	}
	signOffs := map[string]founderSignOff{}
	for _, s := range founderSignOffs {
		signOffs[s.ActivityID] = s
	}

	out := make([]verdict, 0, len(list.Activities))
	byID := map[string]verdict{}
	var resources []projectstate.ActivityItem
	for _, a := range list.Activities {
		component, hasComponent := components[a.ComponentID]
		var v verdict
		switch {
		case hasComponent && component.Kind == projectstate.CompResource:
			resources = append(resources, a) // decided below, once every RA is known.
			continue
		case hasComponent:
			v = fullyImplemented(a.Name, component, in)
		case a.ComponentID != "":
			v = verdict{ActivityID: a.Name, Reason: fmt.Sprintf("componentId %q names no component of the committed System", a.ComponentID)}
		default:
			v = signedOff(a.Name, signOffs, in.Project)
		}
		byID[a.Name] = v
	}
	for _, a := range resources {
		byID[a.Name] = inferredFromAccess(a.Name, components[a.ComponentID], sys, activityFor, byID)
	}
	for _, a := range list.Activities {
		out = append(out, byID[a.Name])
	}
	return out, nil
}

// signedOff is the sign-off evidence path for a componentless activity.
func signedOff(activityID string, signOffs map[string]founderSignOff, p projectstate.Project) verdict {
	s, ok := signOffs[activityID]
	if !ok {
		return verdict{ActivityID: activityID, Reason: "no evidence path: a componentless activity has no code to read, and no founder sign-off is recorded for it"}
	}
	description, err := s.Artifact.Describe(p)
	if err != nil {
		return verdict{ActivityID: activityID, Reason: fmt.Sprintf("founder sign-off recorded (%s) but its artifact does not stand: %v", s.Date, err)}
	}
	basis := signOffBasis(s, description)
	return verdict{ActivityID: activityID, Qualifies: true, Reason: "sign-off: " + basis, Basis: basis, ArtifactRef: s.Artifact.Ref}
}

// fullyImplemented applies the founder's "fully implemented" test to one component. A
// component is fully implemented only when ALL of these hold:
//
//  1. It has .serviceContracts entries, grouped by their .component.
//  2. Every one of those entries has a non-empty goPackage and no stub: true.
//  3. server/<goPackage>/contract.gen.go exists, AND the component's hand-written
//     server/<goPackage>/<lowercase interface>.go declares ONE receiver type with a
//     method for every operation of the component's contract — checked with go/parser.
//
// Condition 3 as first written asked for a declared `type <Interface>Impl`. No
// component in this repository satisfies that: an engine's <Interface>Impl struct is
// GENERATED (in contract.gen.go), a Manager's concrete type is the unexported
// <interface> struct, and a ResourceAccess names its concrete types after the resource
// it binds (postgresUsageAccess, gitLocalAccess, …). A declared type proves nothing
// about implementation in any case. The hand-written file carrying a method per
// contract operation on one receiver is what "implemented" means here, whatever the
// receiver is called, and it is strictly stronger than a declaration.
//
// 3b. (Architect ruling, 2026-09-12.) For a ResourceAccess, at least one covering
// receiver must be a struct with at least one field, its declaration resolved across
// every non-test file of the package (contract.gen.go included — GitArtifactAccess is
// declared there). A ResourceAccess binds a Resource, and every placeholder in this
// repository (noopUsageAccess, dryRunArtifacts, notConfiguredMerchantGatewayAccess, …)
// is an empty struct{}: a component whose only implementation holds nothing binds
// nothing. Engines and Managers are exempt — they are stateless by doctrine, and for
// them stub: true stays authoritative. Accepted residual: erroringArtifactAccess{err}
// has a field, but it only ever co-covers beside GitArtifactAccess.
//
// FACETS: a component may own several contracts (projectStateAccess carries
// constructionTransitionAccess, designSessionAccess and gitActivityStatusAccess;
// billingStateAccess carries revenueLedgerAccess). Facets share the component's
// goPackage and are held to conditions 1 and 2 with it, but condition 3 reads only the
// component's OWN interface — the entry whose key is the component itself. A facet
// interface has no file of its own, and demanding one would fail a component that is
// implemented.
func fullyImplemented(activityID string, component projectstate.Component, in inputs) verdict {
	fail := func(format string, args ...any) verdict {
		return verdict{ActivityID: activityID, Reason: fmt.Sprintf(format, args...)}
	}
	if component.ContractKey == nil || *component.ContractKey == "" {
		return fail("condition 1: component %q names no contractKey, so no .serviceContracts entry is grouped under it", component.ID)
	}
	key := *component.ContractKey
	entries := contractsOf(in.Project.ServiceContracts, key)
	if len(entries) == 0 {
		return fail("condition 1: no .serviceContracts entry has component %q", key)
	}
	own, hasOwn := in.Project.ServiceContracts[key]
	if !hasOwn || own.Component != key {
		return fail("condition 1: .serviceContracts has no entry for the component's own interface (key %q)", key)
	}
	for _, k := range entries {
		sc := in.Project.ServiceContracts[k]
		if sc.GoPackage == "" {
			return fail("condition 2: serviceContracts[%s] has no goPackage", k)
		}
		if sc.Stub {
			return fail("condition 2: serviceContracts[%s] is stub: true", k)
		}
		if sc.GoPackage != own.GoPackage {
			return fail("condition 2: serviceContracts[%s] is in %s, not the component's package %s", k, sc.GoPackage, own.GoPackage)
		}
	}
	impl, err := implementation(in.ServerRoot, own, component.Kind)
	if err != nil {
		return fail("%v", err)
	}
	files := impl.files
	implemented := fmt.Sprintf("%s implemented by %s", own.Interface.Name, strings.Join(impl.covering, ", "))
	if len(impl.fielded) > 0 {
		implemented += fmt.Sprintf("; kind %s, bound by fielded %s", component.Kind, strings.Join(impl.fielded, ", "))
	}
	basis := fmt.Sprintf("serviceContracts[%s] + %s (%s) @ %s + %s",
		strings.Join(entries, ","), strings.Join(files, " + "), implemented, in.Head, founderRulingRef)
	return verdict{
		ActivityID: activityID, Qualifies: true,
		Reason:      fmt.Sprintf("code: serviceContracts[%s]; %s", strings.Join(entries, ","), implemented),
		Basis:       basis,
		ContractRef: key,
		CodeRef:     in.Head,
		Files:       files,
	}
}

// contractsOf returns the .serviceContracts keys grouped under component, sorted with the
// component's own key first so a basis always reads own-then-facets.
func contractsOf(contracts map[string]projectstate.ServiceContract, component string) []string {
	var facets []string
	own := false
	for k, sc := range contracts {
		if sc.Component != component {
			continue
		}
		if k == component {
			own = true
			continue
		}
		facets = append(facets, k)
	}
	sort.Strings(facets)
	if own {
		return append([]string{component}, facets...)
	}
	return facets
}

// implemented is what condition 3 found for a component's own contract.
type implemented struct {
	// files are the repo-relative files read: contract.gen.go, the hand-written
	// <lowercase interface>.go, and — under 3b — any other file declaring a fielded
	// covering receiver.
	files []string
	// covering is every receiver with a method for every contract operation, in name order.
	covering []string
	// fielded is the covering receivers that are structs with at least one field. It is
	// computed, and must be non-empty, for a ResourceAccess only (3b); nil otherwise.
	fielded []string
}

// implementation checks condition 3 — and, for a ResourceAccess, condition 3b — for a
// component's own contract. Its errors name the condition they fail.
func implementation(serverRoot string, own projectstate.ServiceContract, kind projectstate.ComponentKind) (implemented, error) {
	pkg := filepath.FromSlash(own.GoPackage)
	gen := filepath.Join(serverRoot, pkg, "contract.gen.go")
	if _, err := os.Stat(gen); err != nil {
		return implemented{}, fmt.Errorf("condition 3: %s: %w", repoPath(own.GoPackage, "contract.gen.go"), err)
	}
	name := strings.ToLower(own.Interface.Name) + ".go"
	ops := make([]string, 0, len(own.Interface.Operations))
	for _, op := range own.Interface.Operations {
		ops = append(ops, op.Name)
	}
	// A contract with no operations is refused, not vacuously covered: every receiver
	// has a method for each of zero operations, and "implements nothing" is not evidence.
	if len(ops) == 0 {
		return implemented{}, fmt.Errorf("condition 3: serviceContracts[%s] declares no operations, so there is nothing to find implemented", own.Component)
	}
	covering, err := implementingReceivers(filepath.Join(serverRoot, pkg, name), ops)
	if err != nil {
		return implemented{}, fmt.Errorf("condition 3: %s: %w", repoPath(own.GoPackage, name), err)
	}
	impl := implemented{
		files:    []string{repoPath(own.GoPackage, "contract.gen.go"), repoPath(own.GoPackage, name)},
		covering: covering,
	}
	if kind != projectstate.CompResourceAccess {
		return impl, nil // 3b: Engines and Managers are stateless by doctrine.
	}
	return bindsAResource(impl, filepath.Join(serverRoot, pkg), own.GoPackage)
}

// bindsAResource is condition 3b: at least one of a ResourceAccess's covering receivers
// is a struct with a field. A file declaring one that the basis does not already cite is
// added to the cited files, so the run's clean-at-HEAD check covers it too.
func bindsAResource(impl implemented, dir, goPackage string) (implemented, error) {
	structs, err := fieldedStructs(dir)
	if err != nil {
		return implemented{}, fmt.Errorf("condition 3b: %w", err)
	}
	for _, recv := range impl.covering {
		file, ok := structs[recv]
		if !ok {
			continue
		}
		impl.fielded = append(impl.fielded, recv)
		if cited := repoPath(goPackage, file); !slices.Contains(impl.files, cited) {
			impl.files = append(impl.files, cited)
		}
	}
	if len(impl.fielded) == 0 {
		return implemented{}, fmt.Errorf("condition 3b: no covering receiver of this ResourceAccess (%s) is a struct with a field — a ResourceAccess binds a Resource, and a receiver that holds nothing binds nothing",
			strings.Join(impl.covering, ", "))
	}
	return impl, nil
}

// fieldedStructs resolves the type declarations of every non-test Go file in dir and
// returns each struct type with at least one field (an embedded field counts), mapped to
// the file that declares it. An alias, a non-struct named type and an empty struct{} are
// all absent.
func fieldedStructs(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, _ := spec.(*ast.TypeSpec)
				if st, isStruct := ts.Type.(*ast.StructType); isStruct && !ts.Assign.IsValid() && st.Fields.NumFields() > 0 {
					out[ts.Name.Name] = name
				}
			}
		}
	}
	return out, nil
}

// repoPath is a file's path as cited in a basis: repo-relative, slash-separated.
func repoPath(goPackage, file string) string {
	return path.Join(serverDir, goPackage, file)
}

// implementingReceivers parses a hand-written Go file and returns every receiver type
// that has a method for every one of ops. A generated file is refused (it is not a
// hand-built implementation), and so is a file whose methods cover the operations only
// across several receivers or not at all. When more than one receiver covers them (a
// live and a dry-run implementation side by side), every one is named, so a basis never
// cites the no-op alone when a real implementation sits beside it.
func implementingReceivers(file string, ops []string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(f) {
		return nil, errors.New("is generated, not a hand-written implementation")
	}
	return coveringReceivers(methodsByReceiver(f), ops)
}

// methodsByReceiver indexes a file's methods by receiver type name.
func methodsByReceiver(f *ast.File) map[string]map[string]bool {
	methods := map[string]map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		recv := receiverTypeName(fn.Recv.List[0].Type)
		if methods[recv] == nil {
			methods[recv] = map[string]bool{}
		}
		methods[recv][fn.Name.Name] = true
	}
	return methods
}

// coveringReceivers returns every receiver whose methods cover every op, in name order;
// when none does, it says how close the best one came.
func coveringReceivers(methods map[string]map[string]bool, ops []string) ([]string, error) {
	var covering []string
	best, bestHit := "", 0
	for recv, set := range methods {
		hit := 0
		for _, op := range ops {
			if set[op] {
				hit++
			}
		}
		if hit == len(ops) {
			covering = append(covering, recv)
		}
		if hit > bestHit || (hit == bestHit && recv < best) {
			best, bestHit = recv, hit
		}
	}
	if len(covering) == 0 {
		if bestHit == 0 {
			return nil, fmt.Errorf("declares no method for any of the %d contract operations", len(ops))
		}
		return nil, fmt.Errorf("no one receiver implements all %d contract operations (best: %s with %d)", len(ops), best, bestHit)
	}
	sort.Strings(covering)
	return covering, nil
}

// receiverTypeName is the base type name of a method receiver: T for T, *T, T[K] and *T[K].
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.IndexExpr:
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// inferredFromAccess is the INFERRED evidence path for a Resource. Nothing in this
// repository is the Resource's own code, so the inference runs through the architecture:
// a Resource is provisioned and integrated iff every ResourceAccess with a slot-5
// relationship to it is fully implemented. A Resource no ResourceAccess reaches has
// nothing to infer from and does not qualify.
func inferredFromAccess(activityID string, resource projectstate.Component, sys projectstate.System,
	activityFor map[string]string, decided map[string]verdict,
) verdict {
	accessors := map[string]bool{}
	byID := map[string]projectstate.Component{}
	for _, c := range sys.Components {
		byID[c.ID] = c
	}
	for _, r := range sys.Relationships {
		if r.To != resource.ID {
			continue
		}
		if from, ok := byID[r.From]; ok && from.Kind == projectstate.CompResourceAccess {
			accessors[r.From] = true
		}
	}
	if len(accessors) == 0 {
		return verdict{ActivityID: activityID, Reason: fmt.Sprintf("inferred: no ResourceAccess has a slot-5 relationship to %q, so there is nothing to infer from", resource.ID)}
	}
	ids := make([]string, 0, len(accessors))
	for id := range accessors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cites := make([]string, 0, len(ids))
	for _, ra := range ids {
		act, ok := activityFor[ra]
		if !ok {
			return verdict{ActivityID: activityID, Reason: fmt.Sprintf("inferred: ResourceAccess %q has no activity in the plan to infer from", ra)}
		}
		v := decided[act]
		if !v.Qualifies {
			return verdict{ActivityID: activityID, Reason: fmt.Sprintf("inferred: its ResourceAccess %s does not qualify (%s)", act, v.Reason)}
		}
		cites = append(cites, fmt.Sprintf("%s (%s->%s) fully implemented: serviceContracts[%s]", act, ra, resource.ID, v.ContractRef))
	}
	basis := fmt.Sprintf("inferred, not read: a Resource has no code in this repository; every ResourceAccess with a slot-5 relationship to %s qualifies — %s + %s",
		resource.ID, strings.Join(cites, "; "), founderRulingRef)
	return verdict{ActivityID: activityID, Qualifies: true, Reason: "inferred: " + strings.Join(ids, ","), Basis: basis}
}

// evidenceFor points one task's attempt at the artifact that backs it, and at NOTHING
// when none does. Detailed design and its review were read off the contract;
// construction and its review off the code at the cited commit. A sign-off backs every
// task with the artifact it approved. The remaining tasks exist because of a ruling, not
// a file, so they carry no evidence ref: handing the UI a contract to open under a row
// labelled "Testing" would be a click-through that lies about what it shows. The basis
// still says exactly where each row came from.
//
// Every task is listed, with no default case, so `exhaustive` fails the build the moment
// a thirteenth is added without a conscious call about what backs it.
func evidenceFor(task projectstate.MethodTask, v verdict) projectstate.EvidenceRef {
	if v.ArtifactRef != "" {
		return projectstate.EvidenceRef{Kind: projectstate.EvidenceArtifact, Ref: v.ArtifactRef}
	}
	switch task {
	case projectstate.TaskDetailedDesign, projectstate.TaskDesignReview:
		if v.ContractRef != "" {
			return projectstate.EvidenceRef{Kind: projectstate.EvidenceContract, Ref: v.ContractRef}
		}
	case projectstate.TaskConstruction, projectstate.TaskCodeReview:
		if v.CodeRef != "" {
			return projectstate.EvidenceRef{Kind: projectstate.EvidenceGit, Ref: v.CodeRef}
		}
	case projectstate.TaskSRS, projectstate.TaskSRSReview,
		projectstate.TaskSTP, projectstate.TaskSTPReview,
		projectstate.TaskIntegration, projectstate.TaskTesting:
		// The ruling is the evidence; there is no artifact to point at.
	case projectstate.TaskSomeConstruction, projectstate.TaskTestClient:
		// Conditional-emit; never derived here.
	}
	return projectstate.EvidenceRef{}
}

// classify resolves an activity's type and testing variant from the committed plan. A
// component that qualified on code has contracts, which ClassifyType ranks above every
// other signal. An unclassifiable activity has no profile, so it has no task
// vocabulary, so there is nothing honest to write against it.
func classify(item projectstate.ActivityItem, hasContract bool) (projectstate.ActivityType, projectstate.TestingVariant, error) {
	typ, ok := projectstate.ClassifyType(item.Name, item.WorkerClass, item.Coding, hasContract)
	if !ok {
		return 0, 0, fmt.Errorf("%s: ClassifyType refused (workerClass %q, coding %v)", item.Name, item.WorkerClass, item.Coding)
	}
	variant := projectstate.TestVariantPlan
	if typ == projectstate.ActivityTypeTesting {
		_, variant, _ = projectstate.ClassifyActivity(item.Name, item.WorkerClass, item.Coding)
	}
	return typ, variant, nil
}

// attemptsFor derives a qualifying activity's attempts: ONE passed attempt at every
// non-conditional task of its profile. The conditional tasks (someConstruction,
// testClient) are conditional-emit by design — rendered only when a real attempt exists
// — and inventing one would assert a pre-design spike or a test client that may never
// have existed. The ruling says the work is done, reviewed and integrated; it does not
// say how it got there, and this tool must not fill that in.
func attemptsFor(v verdict, typ projectstate.ActivityType, variant projectstate.TestingVariant, now time.Time) []projectstate.TaskAttempt {
	var out []projectstate.TaskAttempt
	for _, task := range projectstate.TasksForProfile(projectstate.ProfileFor(typ, variant)) {
		if projectstate.IsConditionalTask(task) {
			continue
		}
		stamp := now
		out = append(out, projectstate.TaskAttempt{
			AttemptID: projectstate.AttemptID(v.ActivityID, task, 1),
			Task:      task,
			Phase:     projectstate.PhaseForTask(task),
			Attempt:   1,
			Actor:     projectstate.ActorAgent,
			Outcome:   projectstate.OutcomePassed,
			Evidence:  evidenceFor(task, v),
			Provenance: projectstate.AttemptProvenance{
				Origin:      projectstate.OriginBackfilled,
				Generator:   generatorID,
				GeneratedAt: &stamp,
				Basis:       v.Basis,
			},
		})
	}
	return out
}

// validateAttempts enforces the two invariants every attempt must satisfy before
// anything is written: Validate() must pass (a backfilled record with an empty basis is
// a hard error, not a silent nil on the wire), and the origin must be OriginBackfilled —
// this tool observes nothing.
func validateAttempts(attempts []projectstate.TaskAttempt) error {
	for _, attempt := range attempts {
		if err := attempt.Provenance.Validate(); err != nil {
			return fmt.Errorf("%s: %w", attempt.AttemptID, err)
		}
		if attempt.Provenance.Origin != projectstate.OriginBackfilled {
			return fmt.Errorf("%s: origin %q — this tool observes nothing", attempt.AttemptID, attempt.Provenance.Origin)
		}
	}
	return nil
}

// backfill applies the verdicts to p.ActivityConstruction. Nothing in p is touched until
// every check below has passed; any refusal leaves p exactly as it was given.
//
//   - A QUALIFYING activity's attempts are validated. An existing row keeps every field
//     but its attempts, and its attempts are replaced only when every one of them is this
//     tool's own earlier backfill: a row carrying any other attempt holds real history,
//     and overwriting it is refused. When this tool's earlier backfill is exactly what the
//     run derives again, it is kept as it stands (see sameDerivation), so a re-run over
//     unchanged evidence writes nothing.
//   - A NON-QUALIFYING activity is re-examined too: when its row holds any attempt this
//     generator backfilled, the evidence that row was derived from has regressed, and the
//     whole run is refused, naming every such activity and its failing condition (see
//     deQualified). A row with no attempt of this generator's is left alone.
func backfill(p *projectstate.Project, verdicts []verdict, now time.Time) (int, error) {
	list, _, err := planOf(*p)
	if err != nil {
		return 0, err
	}
	if err := deQualified(p.ActivityConstruction, verdicts); err != nil {
		return 0, err
	}
	items := make(map[string]projectstate.ActivityItem, len(list.Activities))
	for _, a := range list.Activities {
		items[a.Name] = a
	}
	type planned struct {
		row      projectstate.ActivityConstructionStatus
		attempts []projectstate.TaskAttempt
	}
	var writes []planned
	for _, v := range verdicts {
		if !v.Qualifies {
			continue
		}
		typ, variant, err := classify(items[v.ActivityID], v.ContractRef != "")
		if err != nil {
			return 0, err
		}
		attempts := attemptsFor(v, typ, variant, now)
		if len(attempts) == 0 {
			return 0, fmt.Errorf("%s qualifies but its profile yields no task to record", v.ActivityID)
		}
		if err := validateAttempts(attempts); err != nil {
			return 0, err
		}
		row, attempts, err := rowFor(p.ActivityConstruction, v, typ, variant, attempts)
		if err != nil {
			return 0, err
		}
		writes = append(writes, planned{row: row, attempts: attempts})
	}
	if len(writes) > 0 && p.ActivityConstruction == nil {
		p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{}
	}
	total := 0
	for _, w := range writes {
		w.row.Attempts = w.attempts
		p.ActivityConstruction[w.row.ActivityID] = w.row
		total += len(w.attempts)
	}
	return total, nil
}

// deQualified refuses a run in which an activity that does NOT qualify has a row holding
// an attempt this generator backfilled — a row that says "done" on evidence that no
// longer stands (the implementing file was deleted, the contract flipped to stub: true,
// the signed-off artifact was removed, …). It names every such activity, in plan order,
// with the condition it now fails.
//
// Neither silent option is honest. Retracting the row would delete history: it was a
// true record of what the evidence showed when it was written. Keeping it would leave a
// row that is wrong. A done activity going undone is a regression, and a human decides
// what to do about it — so the tool stops and says so.
func deQualified(rows map[string]projectstate.ActivityConstructionStatus, verdicts []verdict) error {
	var regressed []string
	for _, v := range verdicts {
		if v.Qualifies {
			continue
		}
		if slices.ContainsFunc(rows[v.ActivityID].Attempts, ownBackfill) {
			regressed = append(regressed, fmt.Sprintf("  %s: %s", v.ActivityID, v.Reason))
		}
	}
	if len(regressed) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to write anything: activities this tool backfilled no longer qualify (%d) — a done activity going undone is a regression for a human to decide (retracting its row would delete history; keeping it would leave a row that is wrong):\n%s",
		len(regressed), strings.Join(regressed, "\n"))
}

// ownBackfill reports whether an attempt is this tool's own earlier backfill: origin
// backfilled AND generator this tool. It is the one test of "ours" — the overwrite guard
// and the regression check must agree on it, or a row could be replaceable as ours yet
// escape re-examination as not ours (or the reverse).
func ownBackfill(a projectstate.TaskAttempt) bool {
	return a.Provenance.Origin == projectstate.OriginBackfilled && a.Provenance.Generator == generatorID
}

// rowFor returns the row a qualifying activity's attempts go into, and the attempts to
// put there. A new row is typed from the verdict's classification. An existing row keeps
// every field; it is refused unless every attempt it holds is this generator's own
// backfill, and when that backfill is exactly what this run derived again, its attempts
// are kept as they stand — the original generatedAt and citation included.
func rowFor(rows map[string]projectstate.ActivityConstructionStatus, v verdict,
	typ projectstate.ActivityType, variant projectstate.TestingVariant, fresh []projectstate.TaskAttempt,
) (projectstate.ActivityConstructionStatus, []projectstate.TaskAttempt, error) {
	row, exists := rows[v.ActivityID]
	if !exists {
		return projectstate.ActivityConstructionStatus{ActivityID: v.ActivityID, Type: typ, Variant: variant}, fresh, nil
	}
	for _, a := range row.Attempts {
		if !ownBackfill(a) {
			return row, nil, fmt.Errorf("activityConstruction[%s] already holds attempt %s (origin %q, generator %q) — refusing to overwrite real history",
				v.ActivityID, a.AttemptID, a.Provenance.Origin, a.Provenance.Generator)
		}
	}
	if sameDerivation(row.Attempts, fresh, v.CodeRef) {
		return row, row.Attempts, nil // unchanged: keep the original stamp and citation.
	}
	return row, fresh, nil
}

// sameDerivation reports whether a row's held attempts are what this run derived again,
// differing only in the two stamps a run puts on its output: generatedAt, and — for a
// code verdict — the commit its basis and git evidence cite (fresh cites head, held cites
// the commit the code was first read at). When they match, the held attempts are kept
// byte for byte, so a re-run over unchanged evidence writes nothing: re-stamping the time
// and the sha would be a diff that records no new fact, and it would also replace a
// citation the reader can check (the commit the code was read at) with one that merely
// agrees with it.
func sameDerivation(held, fresh []projectstate.TaskAttempt, head string) bool {
	if len(held) != len(fresh) {
		return false
	}
	cited := citedCommit(held)
	for i := range fresh {
		f, h := fresh[i], held[i]
		if head != "" && cited != "" {
			f = recite(f, head, cited)
		}
		f.Provenance.GeneratedAt, h.Provenance.GeneratedAt = nil, nil
		if !reflect.DeepEqual(f, h) {
			return false
		}
	}
	return true
}

// citedCommit is the one commit held's git evidence cites, or "" when it cites none or
// several (then nothing is re-cited, and a held row citing an older commit simply differs).
func citedCommit(held []projectstate.TaskAttempt) string {
	commit := ""
	for _, a := range held {
		if a.Evidence.Kind != projectstate.EvidenceGit {
			continue
		}
		if commit != "" && a.Evidence.Ref != commit {
			return ""
		}
		commit = a.Evidence.Ref
	}
	return commit
}

// recite rewrites an attempt's citation of commit from to commit to: the "@ <sha> " in
// its basis and its git evidence ref. Nothing else changes.
func recite(a projectstate.TaskAttempt, from, to string) projectstate.TaskAttempt {
	a.Provenance.Basis = strings.ReplaceAll(a.Provenance.Basis, "@ "+from+" ", "@ "+to+" ")
	if a.Evidence.Kind == projectstate.EvidenceGit && a.Evidence.Ref == from {
		a.Evidence.Ref = to
	}
	return a
}

// ---- the writer -----------------------------------------------------------------------
//
// The state file is decoded and re-encoded through the projectstate codec, and exactly
// one top-level member — .activityConstruction — is spliced back into the ORIGINAL bytes.
// Every other member keeps its bytes, which matters for more than a tidy diff: the codec
// does not carry updatedAt or activityListOverrides, so a whole-document rewrite would
// silently drop both.

// member is one member of a JSON object, in document order.
type member struct {
	key   string
	value json.RawMessage
}

// members decodes a compact JSON object into its members, in document order. An object
// that holds a member twice is refused: which copy counts is up to the reader (Go's
// decoder takes the last), so a splice over it could write one copy and leave the other
// standing, and every byte comparison here would be comparing an arbitrary pick.
func members(raw []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	var out []member
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read member key: %w", err)
		}
		key, _ := tok.(string)
		if seen[key] {
			return nil, fmt.Errorf("the document holds member %q twice — which copy counts is ambiguous; refusing", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("read member %q: %w", key, err)
		}
		out = append(out, member{key: key, value: value})
	}
	return out, nil
}

// joinMembers re-emits members as a compact JSON object.
func joinMembers(ms []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key) // a string always marshals
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// valueOf returns the value of key among ms, or nil.
func valueOf(ms []member, key string) json.RawMessage {
	for _, m := range ms {
		if m.key == key {
			return m.value
		}
	}
	return nil
}

// compactDocument returns the document compacted, plus the whitespace after its closing
// brace. FIDELITY GATE: it refuses a document that re-indenting would not reproduce
// byte-for-byte — writing one would smuggle a reformat of unrelated state into the diff.
func compactDocument(raw []byte) ([]byte, []byte, error) {
	trimmed := bytes.TrimRight(raw, " \t\r\n")
	trailer := raw[len(trimmed):]
	var body bytes.Buffer
	if err := json.Compact(&body, trimmed); err != nil {
		return nil, nil, fmt.Errorf("parse the project document: %w", err)
	}
	var again bytes.Buffer
	if err := json.Indent(&again, body.Bytes(), "", "  "); err != nil {
		return nil, nil, fmt.Errorf("indent the project document: %w", err)
	}
	if !bytes.Equal(again.Bytes(), trimmed) {
		return nil, nil, errors.New("re-indenting the project document does not reproduce it byte-for-byte; a rewrite would reformat unrelated state — refusing")
	}
	return body.Bytes(), trailer, nil
}

// decodeDocument decodes a project document through the codec, under the id it carries.
func decodeDocument(raw []byte) (projectstate.Project, error) {
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return projectstate.Project{}, fmt.Errorf("read the project id: %w", err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(head.ID))
	if err != nil {
		return projectstate.Project{}, err
	}
	if !ok {
		return projectstate.Project{}, errors.New("the file holds no project document")
	}
	return p, nil
}

// encodeCompact encodes p through the codec, compacted.
func encodeCompact(p projectstate.Project) ([]byte, error) {
	enc, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Compact(&out, enc); err != nil {
		return nil, fmt.Errorf("compact the encoded project: %w", err)
	}
	return out.Bytes(), nil
}

// spliceConstruction puts the codec's encoding of .activityConstruction into the original
// document and changes nothing else.
//
//   - The member is REPLACED in place when the document holds it. Before that, its
//     original bytes must equal the codec's own encoding of them: the codec drops any
//     field it does not carry, so a member it cannot round-trip would lose data in the
//     replacement, and codec-vs-codec checks cannot see that loss. Such a member is refused.
//   - The member is ADDED when the document does not hold it (the codec omits an empty
//     map), at the position the codec itself gives it: straight after the nearest member
//     that precedes it in the codec's order and is present in the document.
//   - It is REMOVED when the codec omits it after the edit.
func spliceConstruction(body, before, after []byte) ([]byte, error) {
	original, err := members(body)
	if err != nil {
		return nil, err
	}
	was, err := members(before)
	if err != nil {
		return nil, err
	}
	now, err := members(after)
	if err != nil {
		return nil, err
	}
	if err := onlyConstructionEdited(was, now); err != nil {
		return nil, err
	}
	if err := roundTripsExactly(valueOf(original, constructionMember), valueOf(was, constructionMember)); err != nil {
		return nil, err
	}
	value := valueOf(now, constructionMember)
	out := make([]member, 0, len(original)+1)
	placed := false
	for _, m := range original {
		if m.key == constructionMember {
			if value != nil {
				out = append(out, member{key: m.key, value: value})
			}
			placed = true
			continue
		}
		out = append(out, m)
	}
	if !placed && value != nil {
		at, err := codecPosition(original, now)
		if err != nil {
			return nil, err
		}
		out = append(out[:at], append([]member{{key: constructionMember, value: value}}, out[at:]...)...)
	}
	return joinMembers(out), nil
}

// onlyConstructionEdited refuses an edit whose codec encoding moved any member but
// .activityConstruction.
func onlyConstructionEdited(was, now []member) error {
	for _, m := range now {
		if m.key != constructionMember && !bytes.Equal(m.value, valueOf(was, m.key)) {
			return fmt.Errorf("the edit changed %s, which this tool may not touch — refusing", m.key)
		}
	}
	return nil
}

// roundTripsExactly refuses to replace a committed member whose original bytes differ
// from the codec's encoding of them (held is the original value, nil when the document
// does not hold it; encoded is the codec's encoding of the decoded document). The codec
// drops whatever it does not carry, and a codec-vs-codec comparison cannot see that loss —
// only a comparison against the original bytes can.
func roundTripsExactly(held, encoded json.RawMessage) error {
	if held == nil {
		return nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, held); err != nil {
		return err
	}
	if !bytes.Equal(compact.Bytes(), encoded) {
		return errors.New("the committed .activityConstruction does not survive a codec round trip byte-for-byte (the codec would drop or reshape part of it) — replacing it would lose data; refusing")
	}
	return nil
}

// codecPosition is the index in original at which the construction member belongs: just
// after the nearest member that precedes it in the codec's encoding and that original
// holds, or at the start when none does.
func codecPosition(original, encoded []member) (int, error) {
	held := make(map[string]int, len(original))
	for i, m := range original {
		held[m.key] = i
	}
	at := 0
	for _, m := range encoded {
		if m.key == constructionMember {
			return at, nil
		}
		if i, ok := held[m.key]; ok {
			at = i + 1
		}
	}
	return 0, errors.New("the codec's encoding holds no activityConstruction member to place")
}

// rewrite applies edit to the project document raw and returns the rewritten bytes.
// Nothing outside .activityConstruction changes, and that is proved three ways before it
// is returned: the fidelity gate on the input, a byte comparison of every other member,
// and a decode of the result that must re-encode to exactly the codec's encoding of the
// edited Project.
func rewrite(raw []byte, edit func(*projectstate.Project) error) ([]byte, error) {
	body, trailer, err := compactDocument(raw)
	if err != nil {
		return nil, err
	}
	p, err := decodeDocument(raw)
	if err != nil {
		return nil, err
	}
	before, err := encodeCompact(p)
	if err != nil {
		return nil, err
	}
	if err := edit(&p); err != nil {
		return nil, err
	}
	after, err := encodeCompact(p)
	if err != nil {
		return nil, err
	}
	spliced, err := spliceConstruction(body, before, after)
	if err != nil {
		return nil, err
	}
	// STRUCTURAL TRIPWIRES. The two checks below hold by construction today — the splicer
	// builds its output from the original members and swaps only the construction member,
	// and the codec re-encodes what it decoded — so no test can reach either. They stay on
	// purpose, as tripwires: an edit to the splicer that reached another member, or a
	// codec whose encoding of .activityConstruction stopped being a fixed point, would
	// otherwise be written to the state file silently.
	if err := confirmOnlyConstructionMoved(body, spliced); err != nil {
		return nil, err
	}
	back, err := decodeDocument(spliced)
	if err != nil {
		return nil, err
	}
	again, err := encodeCompact(back)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(again, after) {
		return nil, errors.New("the spliced document does not decode to the edited project — refusing to write")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, spliced, "", "  "); err != nil {
		return nil, fmt.Errorf("indent the rewritten document: %w", err)
	}
	out.Write(trailer)
	return out.Bytes(), nil
}

// confirmOnlyConstructionMoved proves every member other than .activityConstruction is
// byte-identical, and in the same order, in the rewritten document.
func confirmOnlyConstructionMoved(original, rewritten []byte) error {
	was, err := members(original)
	if err != nil {
		return err
	}
	now, err := members(rewritten)
	if err != nil {
		return err
	}
	strip := func(ms []member) []member {
		out := make([]member, 0, len(ms))
		for _, m := range ms {
			if m.key != constructionMember {
				out = append(out, m)
			}
		}
		return out
	}
	a, b := strip(was), strip(now)
	if len(a) != len(b) {
		return fmt.Errorf("the rewrite changed the member set outside %s — refusing", constructionMember)
	}
	for i := range a {
		if a[i].key != b[i].key || !bytes.Equal(a[i].value, b[i].value) {
			return fmt.Errorf("the rewrite changed %s — refusing", a[i].key)
		}
	}
	return nil
}

// ---- the run --------------------------------------------------------------------------

// gitHead returns the commit the repository is at.
func gitHead(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output() //nolint:gosec // a one-shot CLI running git in the repo it was pointed at.
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// citedFilesClean refuses a run whose cited code differs from HEAD: every code basis
// says "@ <HEAD>", which is only true if the files read are the files committed there.
func citedFilesClean(repo string, verdicts []verdict) error {
	var files []string
	for _, v := range verdicts {
		if v.Qualifies {
			files = append(files, v.Files...)
		}
	}
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"-C", repo, "status", "--porcelain", "--"}, files...)
	out, err := exec.Command("git", args...).Output() //nolint:gosec // a one-shot CLI running git over the files it cites.
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return fmt.Errorf("cited files differ from HEAD, so a basis citing HEAD would be false:\n%s", out)
	}
	return nil
}

func run(repo string, dryRun bool) error {
	file := filepath.Join(repo, statePath)
	raw, err := os.ReadFile(file) //nolint:gosec // a one-shot CLI reading the path it was told to read.
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	p, err := decodeDocument(raw)
	if err != nil {
		return err
	}
	head, err := gitHead(repo)
	if err != nil {
		return err
	}
	verdicts, err := evaluate(inputs{Project: p, ServerRoot: filepath.Join(repo, serverDir), Head: head})
	if err != nil {
		return err
	}
	if err := citedFilesClean(repo, verdicts); err != nil {
		return err
	}
	now := time.Now().UTC()
	total := 0
	out, err := rewrite(raw, func(p *projectstate.Project) error {
		n, err := backfill(p, verdicts, now)
		total = n
		return err
	})
	if err != nil {
		return err
	}
	printReport(verdicts, total, head, dryRun)
	if dryRun {
		return nil
	}
	if bytes.Equal(out, raw) {
		fmt.Println("nothing to write")
		return nil
	}
	if err := os.WriteFile(file, out, 0o600); err != nil { //nolint:gosec // a one-shot CLI writing back the file it was told to read.
		return fmt.Errorf("write %s: %w", file, err)
	}
	fmt.Printf("wrote %s\n", file)
	return nil
}

func printReport(verdicts []verdict, total int, head string, dryRun bool) {
	mode := "write"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf("backfill-attempts (%s) @ %s\n\nQUALIFIES\n", mode, head)
	qualifying := 0
	for _, v := range verdicts {
		if v.Qualifies {
			qualifying++
			fmt.Printf("  %-32s %s\n", v.ActivityID, v.Reason)
		}
	}
	fmt.Println("\nDOES NOT QUALIFY")
	for _, v := range verdicts {
		if !v.Qualifies {
			fmt.Printf("  %-32s %s\n", v.ActivityID, v.Reason)
		}
	}
	fmt.Printf("\n  %d of %d activities qualify, %d attempts total\n", qualifying, len(verdicts), total)
}

func main() {
	repo := flag.String("repo", "..", "path to the repository root containing .aiarch/state/project.json")
	dryRun := flag.Bool("dry-run", false, "report what would be written without writing")
	flag.Parse()

	if err := run(*repo, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "backfill-attempts: %v\n", err)
		os.Exit(1)
	}
}
