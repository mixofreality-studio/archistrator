package internal_test

// signalwire_arch_test.go is the CONSUMER half of the signal wire-form rule. Stage
// 4b2 shipped the producer half (Test_DeliverSignal_TheWireFormProducersAreAClosedList,
// in manager/delivery) and owed this one.
//
// THE RULE: a signal channel whose NAME any messageBus.deliverSignal producer uses
// must be received into `any` (or a *[]byte) and normalised — never straight into a
// concrete struct.
//
// WHY, measured on a real dev server and not reasoned about: deliverSignal hands the
// Temporal client raw []byte, the default data converter tags it binary/plain, and
// the SDK's ByteSlicePayloadConverter can assign such a payload to nothing but a
// *[]byte. A concrete-struct receive target therefore holds it NOT AT ALL: the SDK
// logs "Corrupted signal received on channel X" and DROPS the message, which is not
// an error any workflow can see. All three lease channels did exactly that, so the
// whole stage-4b2 main-write lease was inert in production — every merge tail armed a
// two-hour budget and then ran unleased — while every unit test passed, because the
// tests all signalled a STRUCT, a wire form production never produces.
//
// THREE DESIGN DECISIONS, each measured rather than chosen.
//
//  1. NAME-KEYED, NOT ID-KEYED. Id-keying is not computable: every bus target is a
//     runtime value (pumpWorkflowID(in.ProjectID), deliveryActivityWorkflowID(...),
//     fmt.Sprintf("%s:delinquency", customerID)). It is also WEAKER exactly where
//     strictness is wanted — operatorPauseRequested's two producers reach DIFFERENT
//     execution ids, so an id-keyed gate would have cleared projectsupervision.go:48,
//     the latent instance stage 4b3 Task 2 fixed.
//
//  2. THE DATAFLOW STARTS AT GetSignalChannel, NOT AT .Receive. deliveryactivity.go
//     also builds workflow.NewChannel inboxes and futures that a receive-first gate
//     would have to tell apart by type; starting at the signal channel makes the
//     question syntactic.
//
//  3. UNANALYSABLE IS A FAILURE, NOT A PASS. If the analysis cannot follow a
//     bus-named channel from its GetSignalChannel to every receive target, it FAILS
//     and says so. A gate that silently skips what it cannot parse is precisely the
//     artifact this one exists to replace: green, and blind in the one class it was
//     built for.
//
// AST-ONLY, DELIBERATELY (go/parser over a file set, no go/types, no packages.Load).
// Every rule here is syntactic — a call shape, an argument position, a declared type
// name — and the only identifier resolution needed is a const's STRING VALUE, which
// is a package-level lookup rather than a type inference. AST-only is also what makes
// the testdata corpus possible at all: a fixture that must compile could not state a
// shape production no longer has. NOTE, against the brief: paramguard_arch_test.go
// does NOT carry a .go.txt corpus — it uses packages.Load over real packages, and
// internal/ had no testdata directory before this commit. The .go.txt extension is
// chosen on its own merits (the go tool ignores testdata/ entirely, and the .txt tail
// keeps gofmt and any future source walker off it too), not on that precedent.
//
// MODULE-SCOPED (package internal_test, in server/internal) because the corpus spans
// packages: the fifth producer is in manager/billing and its consumer is in
// manager/operations, and a package test in manager/delivery cannot see either.
//
// WHAT THIS GATE CANNOT SEE — the honest limits, written here rather than left to be
// discovered. The first four are BLIND SPOTS; the rest are deliberate scope.
//
// THIS LIST WAS INCOMPLETE ONCE, and the omitted entry was the only one that failed
// GREEN. A review defeated the gate in production code with two same-named locals in
// two blocks of one function (see blind spot 4 and testdata/wireform/bad_shadowed.go.txt),
// and the shape passed every gate in the wave. The list is a claim about coverage, so
// an addition to it is a change to the gate: an entry is earned by MEASUREMENT, and
// each one below now states which way it resolves.
//
//  1. THE CORPUS IS server/internal, NON-TEST, NON-GENERATED. A receive in a
//     *_test.go or a *.gen.go, or anywhere outside server/internal, is not checked.
//     Measured at this commit: zero GetSignalChannel calls in generated code and zero
//     outside internal/, so the exclusions cost nothing TODAY. If codegen ever emits a
//     signal receive, this gate will not see it.
//  2. A CHANNEL READ OFF A STRUCT FIELD whose receiver's type cannot be determined
//     syntactically — from a parameter, a `var x T`, or an `x := T{}`. The pump's
//     pumpChannels is followed exactly because its holders ARE declared that way; a
//     channel arriving through an interface, a map or a type the gate cannot name
//     would not be. The seed-reachability rule is the net under this: a bus-delivered
//     channel that reaches NO receive at all is a finding, so a lost channel reads as
//     RED rather than as silence.
//  3. A METHOD IS RESOLVED BY NAME WITHIN ITS PACKAGE, because no types are loaded.
//     Two methods of one name in one package BOTH receive the taint. That
//     over-approximates toward red and never away from it, but it can name a second,
//     innocent site in a finding.
//  4. A LOCAL'S SCOPE IS ITS FUNCTION OR ITS FuncLit, NOT ITS BLOCK. Two `var x T` of
//     one name in two blocks of one body — an if/else, two switch cases, two select
//     comm clauses, a workflow.GetVersion's two arms — share one key in the declared-
//     type map, and the gate cannot tell them apart. Until this commit that was a hole
//     that failed GREEN (last write won, so a `var raw any` written anywhere later in
//     the body cleared a struct receive four lines earlier — a review shipped exactly
//     that into projectsupervision.go and the gate did not move). declare() now
//     resolves a same-name conflict toward NOT-`any`, which INVERTS the blind spot:
//     the gate still cannot tell the two locals apart, but it now judges both by the
//     stricter declaration, so a legitimate `any` receive can be reddened by an
//     unrelated struct of the same name. A false positive, remedied by renaming one
//     local; testdata/wireform/bad_shadowed.go.txt pins both the defeat and the cost.
//
// Every one of the four now over-approximates toward RED, which is the property the
// paragraph after this list asserts. Blind spot 4 is the only one that ever did not.
//
// Deliberately out of scope:
//
//   - WHETHER THE NORMALISATION IS CORRECT. The gate checks that the receive target is
//     `any` or a []byte; whether the decode after it is right belongs to the
//     per-manager tests (pauseSignalReason, pumpDecodeSignal, decodeDelinquencySignal).
//   - REFLECTION, interface dispatch, and a channel handed to another module. These
//     are not missed — they land as "cannot follow", which is a finding.
//   - A CHANNEL NAME THAT IS NOT A STRING LITERAL. Also a finding, not a skip: the
//     producer gate's own rule restated on the consumer side.
//   - AN UNKNOWN RECEIVE TARGET reads as NOT-`any`, i.e. as a finding. Every
//     uncertainty in this file is resolved toward red on purpose — including the
//     AMBIGUOUS one blind spot 4 describes, which was resolved the other way until
//     the commit that added that entry.
//
// NO ALLOWLIST AND NO SANCTIONED-EXCEPTION SHAPE. R4 reserved one exception — a
// pre-change arm behind a workflow.GetVersion fence — and stage 4b3 Task 3 discharged
// all five pump fences under the founder's no-production-users ruling, leaving that
// shape with zero instances. An exception mechanism with no instance is an untested
// escape hatch in a gate whose entire failure mode is silence, so it is not shipped;
// testdata/wireform/bad_fenced.go.txt holds the decision as a FLAGGED fixture. The
// four helpers the plan would have named as "sanctioned decoders" are not named
// either: the analysis follows INTO them and checks them on their merits, which is
// strictly stronger than trusting them by name.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The measured corpus. ASSERTED, NOT ASSUMED.
// ---------------------------------------------------------------------------

// The wire-form corpus as measured at stage 4b3 Task 4, after Tasks 1, 2 and 3.
// A gate that walks a directory is one `git mv` away from walking nothing and
// reporting success, so every one of these is checked.
//
//	wantSignalChannelSites — workflow.GetSignalChannel call sites in non-test,
//	  non-generated code under server/internal. (An earlier recon said 13; the
//	  thirteenth is prose, at deliveryactivity.go:3729.)
//	wantBusProducerSites   — MessageBusDeliverSignal call sites.
//	wantBusChannelSites    — of the 12, those whose NAME a producer uses.
//
// A change to any of these is a change to this gate's coverage and must be made
// deliberately, here, in the same commit as the code that moved it.
const (
	wantSignalChannelSites = 12
	wantBusProducerSites   = 5
	wantBusChannelSites    = 6
)

// wantBusDeliveredNames is the closed set of signal NAMES that cross the bus as raw
// bytes. It is the consumer-side mirror of deliverSignalWireFormProducers in
// manager/delivery's package test, which pins the four names that package produces;
// this one adds billing's applyDelinquencyPolicy, which no package test can see.
var wantBusDeliveredNames = []string{
	"activityFinished",
	"activityLeaseGranted",
	"activityLeaseRequested",
	"applyDelinquencyPolicy",
	"operatorPauseRequested",
}

// ---------------------------------------------------------------------------
// The gate.
// ---------------------------------------------------------------------------

func TestSignalWireFormConsumers(t *testing.T) {
	g := loadWireCorpus(t, ".")
	names := busDeliveredSignalNames(t, g)
	got := slices.Sorted(maps.Keys(names))
	if !slices.Equal(got, wantBusDeliveredNames) {
		t.Fatalf("the bus-delivered signal names are %v; the closed set is %v — "+
			"update wantBusDeliveredNames in the same commit as the producer that moved",
			got, wantBusDeliveredNames)
	}
	g.busNames = names

	findings := analyseSignalConsumers(g)

	if n := len(g.channelSites); n != wantSignalChannelSites {
		t.Errorf("the wire-form corpus moved: %d workflow.GetSignalChannel site(s), want %d — "+
			"update wantSignalChannelSites in the same commit as the code that moved it", n, wantSignalChannelSites)
	}
	if n := g.busChannelSiteCount(); n != wantBusChannelSites {
		t.Errorf("the wire-form corpus moved: %d of the signal channels carry a bus-delivered name, want %d",
			n, wantBusChannelSites)
	}
	if n := len(g.producerSites); n != wantBusProducerSites {
		t.Errorf("the wire-form corpus moved: %d messageBus.deliverSignal producer site(s), want %d",
			n, wantBusProducerSites)
	}
	// THE VACUITY LOG. A green run of an arch gate says nothing unless it also says how
	// much it looked at, and this one's whole hazard is passing over a corpus it never
	// reached. sinks is the number of receive targets the analysis actually classified;
	// a drop here with the counts above unchanged means the dataflow stopped following
	// something it used to follow.
	t.Logf("checked %d signal channel(s) (%d bus-delivered) against %d producer(s); classified %d receive target(s)",
		len(g.channelSites), g.busChannelSiteCount(), len(g.producerSites), g.totalSinks())
	if g.totalSinks() < wantBusChannelSites {
		t.Errorf("the analysis classified only %d receive target(s) for %d bus-delivered channel(s); "+
			"it is no longer reaching the receives it is supposed to check", g.totalSinks(), wantBusChannelSites)
	}
	for _, f := range findings {
		t.Errorf("%s", f.String())
	}
}

// TestSignalWireFormConsumers_IsRedOnEveryKnownInstance is the mitigation R6 made
// mandatory, and it is not review advice: this gate's whole failure mode is passing
// while the defect stands, and stage 4b2 proved that a wire-form test which signals
// the wrong form certifies nothing at all. So the analyser is run over a corpus of
// the KNOWN instances, in the three receive forms production really uses, and the
// expected finding set is EXACT — one per bad fixture, ZERO for the good one. A
// change that makes this test pass by finding FEWER is a change that broke the gate.
func TestSignalWireFormConsumers_IsRedOnEveryKnownInstance(t *testing.T) {
	// The fixtures are parsed with the PRODUCTION bus-name set, so the corpus states
	// shapes and the rule states names — a fixture cannot invent a name into the rule.
	prod := loadWireCorpus(t, ".")
	names := busDeliveredSignalNames(t, prod)

	g := loadWireFixtures(t, filepath.Join("testdata", "wireform"))
	g.busNames = names
	findings := analyseSignalConsumers(g)

	want := map[string]int{
		"bad_receive.go.txt":      1, // FORM 1: plain blocking Receive into a struct
		"bad_receiveasync.go.txt": 1, // FORM 2: ReceiveAsync into a struct
		"bad_addreceive.go.txt":   1, // FORM 3: the AddReceive closure's param
		"bad_fenced.go.txt":       1, // the GetVersion pre-change arm, NOT sanctioned
		"bad_shadowed.go.txt":     3, // the block-scope defeat: 2 real targets + 1 documented false positive
		"bad_unanalysable.go.txt": 2, // returned out of the corpus; handed to an unresolvable callee
		"good_normalised.go.txt":  0, // every shape production uses, all silent
	}
	// EVERY FIXTURE ON DISK IS NAMED IN want, or a fixture that produces no finding
	// could be added and never counted — the same silence, one directory over.
	var onDisk []string
	for _, u := range g.units {
		onDisk = append(onDisk, filepath.Base(u.Path))
	}
	if named := slices.Sorted(maps.Keys(want)); !slices.Equal(slices.Sorted(slices.Values(onDisk)), named) {
		t.Fatalf("the fixture corpus is %v; the expectation set names %v — every fixture carries an "+
			"expected finding count, including zero", onDisk, named)
	}

	got := map[string]int{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.Site, ":", 2)[0])]++
	}
	for _, f := range findings {
		t.Logf("fixture finding: %s", f.String())
	}
	if len(got) == 0 {
		t.Fatal("the analyser found nothing at all over the fixture corpus; it is checking air")
	}
	for file, n := range want {
		if got[file] != n {
			t.Errorf("%s: the analyser reported %d finding(s), want %d — "+
				"FEWER means the gate went blind on a form it is supposed to see; MORE means it "+
				"reddens on a shape production uses legitimately. Both are defects in the gate, "+
				"not in the fixture.", file, got[file], n)
		}
		delete(got, file)
	}
	for file, n := range got {
		t.Errorf("%s: %d finding(s) from a fixture the expectation set does not name — "+
			"add it to want, with the reason", file, n)
	}
}

// TestSignalPayloadsSetNoContentType is the free fourth rule (R5).
// messagebus.ExecutionPayload.ContentType is declared once
// (internal/utility/messagebus/contract.gen.go:23) and read by NOTHING —
// messagebus.go:162 passes payload.Bytes alone — so a producer that sets it is the
// strongest available signal that its author believed the transport serialised for
// them, which is the belief the whole wire-form defect was made of.
//
// It is NOT a duplicate of Task 1's pin. That one parses one file
// (manager/billing/shortfallsweep.go, from manager/operations' package test) and
// billing's own G1 counts payloads at runtime through a fake; this covers all five
// producers and any sixth, module-wide.
//
// PARSED, NEVER GREPPED, and that is a measurement: Task 1 wrote the strings.Contains
// form first and it reddened on shortfallsweep.go's own COMMENT explaining why the
// file sets no ContentType — the gate firing on the explanation of its own rule. An
// *ast.KeyValueExpr key cannot false-positive that way.
func TestSignalPayloadsSetNoContentType(t *testing.T) {
	g := loadWireCorpus(t, ".")
	for _, f := range payloadContentTypeFindings(g) {
		t.Errorf("%s", f.String())
	}
}

// ---------------------------------------------------------------------------
// wireFinding
// ---------------------------------------------------------------------------

// wireFinding is one violation. Channel is the expression the gate was tracking,
// Name the signal name it resolved to, Why the rule and the remedy.
type wireFinding struct{ Site, Channel, Name, Why string }

func (f wireFinding) String() string {
	return fmt.Sprintf("%s: channel %q (signal %q) %s", f.Site, f.Channel, f.Name, f.Why)
}

// whyUnfollowable is the message the inverted rule exists to print.
const whyUnfollowable = "is delivered by messageBus.deliverSignal, and this gate cannot follow it to its " +
	"receive target. A bus payload is raw []byte tagged binary/plain and a concrete-struct target holds it " +
	"NOT AT ALL — the SDK logs \"Corrupted signal\" and drops the message, silently. Receive into `any` and " +
	"normalise (see pumpReceiveSignal, pumpPauseRequested, decodeDelinquencySignal), or restructure so the " +
	"receive is visible here. This gate refuses what it cannot analyse ON PURPOSE: the whole class it exists " +
	"to catch is invisible"

// whyStructTarget is the message for the defect itself.
const whyStructTarget = "is delivered by messageBus.deliverSignal and is received into a target that is " +
	"neither `any` nor *[]byte. Such a payload is raw []byte tagged binary/plain, and the SDK's " +
	"ByteSlicePayloadConverter can assign it to NOTHING else: the message is dropped, the SDK logs " +
	"\"Corrupted signal\", and no workflow can see it. Receive into `var raw any` and normalise both wire " +
	"forms (see pauseSignalReason and pumpDecodeSignal)"

// ---------------------------------------------------------------------------
// The corpus: parsing and the package-level index.
// ---------------------------------------------------------------------------

// wireUnit is one parsed source file. Scope is its dir plus its declared package
// name — dir alone would be enough for production, where a directory holds one
// package, but the fixture corpus deliberately puts one package per file in one
// directory so each fixture is a self-contained statement of a shape.
type wireUnit struct {
	Path  string
	Scope string
	File  *ast.File
}

// wireFuncInfo is one function or method, keyed by its declaration site.
type wireFuncInfo struct {
	decl  *ast.FuncDecl
	unit  *wireUnit
	id    string
	order string
}

// wireChannelSite is one workflow.GetSignalChannel call. Var is the local the
// channel was assigned to, or "" when the call's result is not assigned to a plain
// identifier — which is itself unfollowable and reported as such.
type wireChannelSite struct {
	Site     string
	Name     string
	Var      string
	FuncID   string
	Bus      bool
	Resolved bool
}

// wireSeeds is a set of channel-site keys: which GetSignalChannel call(s) a tracked
// expression can be carrying. Carried along the dataflow so a finding can name the
// channel it came from, and so the reachability net below can tell which seeds ever
// reached a receive at all.
type wireSeeds map[string]bool

type wireGraph struct {
	fset  *token.FileSet
	units []*wireUnit

	consts  map[string]map[string]string          // scope -> const ident -> string value
	funcs   map[string]map[string][]*wireFuncInfo // scope -> func name -> decls
	structs map[string]map[string]bool            // scope -> struct type name
	byID    map[string]*wireFuncInfo

	busNames      map[string]string // signal value -> producer site
	producerSites []string
	channelSites  []wireChannelSite

	paramTaint map[string]map[int]wireSeeds // funcID -> param index -> seeds
	fieldTaint map[string]wireSeeds         // "scope|Type.Field" -> seeds

	sinkSeeds   map[string]int
	seedFlagged map[string]bool
	findings    []wireFinding
	grew        bool
}

// loadWireCorpus parses every non-test, non-generated .go file under root, skipping
// testdata. root is server/internal — the test's own directory.
func loadWireCorpus(t *testing.T, root string) *wireGraph {
	t.Helper()
	fset := token.NewFileSet()
	var units []*wireUnit
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".gen.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		units = append(units, &wireUnit{Path: path, Scope: filepath.Dir(path) + "|" + f.Name.Name, File: f})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(units) == 0 {
		t.Fatalf("%s: parsed no files; this gate would pass vacuously", root)
	}
	return newWireGraph(fset, units)
}

// loadWireFixtures parses the .go.txt corpus. Each fixture declares its own package
// name so its consts and types cannot collide with a sibling's.
func loadWireFixtures(t *testing.T, dir string) *wireGraph {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(dir, "*.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s holds no .go.txt fixtures; the meta-test would pass vacuously", dir)
	}
	sort.Strings(entries)
	fset := token.NewFileSet()
	var units []*wireUnit
	for _, path := range entries {
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		units = append(units, &wireUnit{Path: path, Scope: dir + "|" + f.Name.Name, File: f})
	}
	return newWireGraph(fset, units)
}

// newWireGraph builds the package-level index the analysis reads: string consts,
// function declarations and local struct type names, all keyed by scope.
//
// It stands in for the brief's analyseSignalConsumers(fset, files) signature: the
// graph IS the file set plus the files plus the one index they need, and threading
// the index through every helper as three more parameters bought nothing.
func newWireGraph(fset *token.FileSet, units []*wireUnit) *wireGraph {
	g := &wireGraph{
		fset:        fset,
		units:       units,
		consts:      map[string]map[string]string{},
		funcs:       map[string]map[string][]*wireFuncInfo{},
		structs:     map[string]map[string]bool{},
		byID:        map[string]*wireFuncInfo{},
		paramTaint:  map[string]map[int]wireSeeds{},
		fieldTaint:  map[string]wireSeeds{},
		sinkSeeds:   map[string]int{},
		seedFlagged: map[string]bool{},
	}
	for _, u := range units {
		g.indexDecls(u)
	}
	return g
}

func (g *wireGraph) indexDecls(u *wireUnit) {
	for _, decl := range u.File.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			fi := &wireFuncInfo{decl: d, unit: u, id: g.pos(d.Pos()), order: g.pos(d.Pos())}
			g.byID[fi.id] = fi
			if g.funcs[u.Scope] == nil {
				g.funcs[u.Scope] = map[string][]*wireFuncInfo{}
			}
			g.funcs[u.Scope][d.Name.Name] = append(g.funcs[u.Scope][d.Name.Name], fi)
		case *ast.GenDecl:
			g.indexGenDecl(u, d)
		}
	}
}

func (g *wireGraph) indexGenDecl(u *wireUnit, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			if d.Tok != token.CONST {
				continue
			}
			for i, name := range s.Names {
				if i >= len(s.Values) {
					continue
				}
				lit, ok := s.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if g.consts[u.Scope] == nil {
					g.consts[u.Scope] = map[string]string{}
				}
				g.consts[u.Scope][name.Name] = v
			}
		case *ast.TypeSpec:
			if _, ok := s.Type.(*ast.StructType); !ok {
				continue
			}
			if g.structs[u.Scope] == nil {
				g.structs[u.Scope] = map[string]bool{}
			}
			g.structs[u.Scope][s.Name.Name] = true
		}
	}
}

func (g *wireGraph) pos(p token.Pos) string {
	pp := g.fset.Position(p)
	return pp.Filename + ":" + strconv.Itoa(pp.Line)
}

// allFuncs returns every indexed function in a deterministic order.
func (g *wireGraph) allFuncs() []*wireFuncInfo {
	out := make([]*wireFuncInfo, 0, len(g.byID))
	for _, fi := range g.byID {
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out
}

// totalSinks is how many receive targets the emitting pass classified — the
// vacuity measure TestSignalWireFormConsumers logs.
func (g *wireGraph) totalSinks() int {
	n := 0
	for _, c := range g.sinkSeeds {
		n += c
	}
	return n
}

func (g *wireGraph) busChannelSiteCount() int {
	n := 0
	for _, c := range g.channelSites {
		if c.Bus {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The producers: which signal NAMES cross the bus as raw bytes.
// ---------------------------------------------------------------------------

// busDeliveredSignalNames maps each bus-delivered signal VALUE to the producer site
// that delivers it. It reuses the producer gate's argument shape
// (deliverSignalNameArg in manager/delivery's package test): the NAME is the third
// argument, spelled messagebus.SignalName(<ident>), and the ident is resolved to its
// string value in the DECLARING package's consts — which is why this gate is
// module-scoped. billing's name is a local literal const and delivery's is another,
// and the two packages cannot see each other.
//
// A name that does not resolve to a string literal is a FATAL, not a skip: a
// computed name is a channel nobody can check, which is the producer gate's own rule
// restated on the consumer side.
func busDeliveredSignalNames(t *testing.T, g *wireGraph) map[string]string {
	t.Helper()
	out := map[string]string{}
	g.producerSites = nil
	for _, u := range g.units {
		ast.Inspect(u.File, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "MessageBusDeliverSignal" || len(call.Args) < 3 {
				return true
			}
			site := g.pos(call.Pos())
			g.producerSites = append(g.producerSites, site)
			ident, ok := wireSignalNameArg(call.Args[2])
			if !ok {
				t.Fatalf("%s delivers a signal whose NAME is not messagebus.SignalName(<const ident>) — "+
					"the wire-form rule is keyed on the name, so a computed name is a channel nobody can check", site)
			}
			value, ok := g.consts[u.Scope][ident]
			if !ok {
				t.Fatalf("%s delivers signal %s, which this gate cannot resolve to a string literal in %s — "+
					"a name it cannot read is a channel it cannot check", site, ident, u.Scope)
			}
			out[value] = site
			return true
		})
	}
	sort.Strings(g.producerSites)
	if len(out) == 0 {
		t.Fatal("no messageBus.deliverSignal producer found; this gate would pass vacuously")
	}
	return out
}

// wireSignalNameArg reads messagebus.SignalName(<ident>) and reports the ident.
func wireSignalNameArg(arg ast.Expr) (string, bool) {
	conv, ok := arg.(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 {
		return "", false
	}
	sel, ok := conv.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "SignalName" {
		return "", false
	}
	id, ok := conv.Args[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// payloadContentTypeFindings flags every messagebus.ExecutionPayload composite
// literal that sets ContentType. See TestSignalPayloadsSetNoContentType.
func payloadContentTypeFindings(g *wireGraph) []wireFinding {
	var out []wireFinding
	for _, u := range g.units {
		ast.Inspect(u.File, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || wireTypeName(lit.Type) != "ExecutionPayload" {
				return true
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "ContentType" {
					continue
				}
				out = append(out, wireFinding{
					Site:    g.pos(kv.Pos()),
					Channel: "ExecutionPayload.ContentType",
					Name:    "-",
					Why: "is set by this producer, and the field is READ BY NOTHING. It is declared once " +
						"(internal/utility/messagebus/contract.gen.go:23) and messagebus.go:162 passes payload.Bytes " +
						"alone, so setting it changes no byte on the wire — its only effect is on the reader, who is " +
						"told the transport negotiated a content type for them. It did not: the payload crosses as " +
						"binary/plain raw bytes. Delete the field from the literal",
				})
			}
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Site < out[j].Site })
	return out
}

// wireTypeName is the base type name of a composite-literal or declaration type,
// with any package qualifier, pointer or generic index stripped.
func wireTypeName(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.StarExpr:
			e = v.X
		case *ast.ParenExpr:
			e = v.X
		case *ast.IndexExpr:
			e = v.X
		case *ast.IndexListExpr:
			e = v.X
		case *ast.SelectorExpr:
			return v.Sel.Name
		case *ast.Ident:
			return v.Name
		default:
			return ""
		}
	}
}

// ---------------------------------------------------------------------------
// The analyser.
// ---------------------------------------------------------------------------

// analyseSignalConsumers is the shared analyser both tests call.
//
// It runs the taint to a FIXPOINT first, silently, because the dataflow is
// interprocedural — a channel reaches pumpPauseRequested's body only after
// pumpPausedAtRunStart's parameter has been tainted, which happens in a different
// function — and only then makes ONE emitting pass, so a finding is reported once
// and against fully-propagated taint.
func analyseSignalConsumers(g *wireGraph) []wireFinding {
	g.collectChannelSites()
	for range 24 {
		g.grew = false
		for _, fi := range g.allFuncs() {
			newWireEnv(g, fi).propagate()
		}
		if !g.grew {
			break
		}
	}
	g.findings = nil
	g.sinkSeeds = map[string]int{}
	g.seedFlagged = map[string]bool{}
	for _, fi := range g.allFuncs() {
		e := newWireEnv(g, fi)
		e.propagate()
		e.walk(true)
	}
	g.reportUnresolvedChannelNames()
	g.reportUnreachedSeeds()
	sort.Slice(g.findings, func(i, j int) bool {
		if g.findings[i].Site != g.findings[j].Site {
			return g.findings[i].Site < g.findings[j].Site
		}
		return g.findings[i].Why < g.findings[j].Why
	})
	return g.findings
}

// collectChannelSites finds every workflow.GetSignalChannel call and resolves its
// name argument. This is decision 2: the dataflow starts HERE, not at a receive,
// because workflow.NewChannel inboxes and futures are also received on and telling
// them apart at the receive would need types the gate deliberately does not load.
func (g *wireGraph) collectChannelSites() {
	g.channelSites = nil
	for _, fi := range g.allFuncs() {
		assigned := map[ast.Expr]string{}
		ast.Inspect(fi.decl, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				assigned[as.Rhs[0]] = id.Name
			}
			return true
		})
		ast.Inspect(fi.decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "GetSignalChannel" || len(call.Args) < 2 {
				return true
			}
			name, resolved := g.constValue(fi.unit.Scope, call.Args[1])
			g.channelSites = append(g.channelSites, wireChannelSite{
				Site:     g.pos(call.Pos()),
				Name:     name,
				Var:      assigned[call],
				FuncID:   fi.id,
				Bus:      resolved && g.busNames[name] != "",
				Resolved: resolved,
			})
			return true
		})
	}
	sort.Slice(g.channelSites, func(i, j int) bool { return g.channelSites[i].Site < g.channelSites[j].Site })
}

// constValue resolves a signal-name expression to its string value: a package-level
// string const, or a literal written at the call.
func (g *wireGraph) constValue(scope string, e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		s, ok := g.consts[scope][v.Name]
		return s, ok
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	}
	return "", false
}

func (g *wireGraph) reportUnresolvedChannelNames() {
	for _, cs := range g.channelSites {
		if cs.Resolved {
			continue
		}
		g.findings = append(g.findings, wireFinding{
			Site: cs.Site, Channel: cs.Var, Name: "<unresolved>",
			Why: "opens a signal channel whose NAME this gate cannot resolve to a string literal. The " +
				"wire-form rule is keyed on the name, so a computed name is a channel nobody can check — " +
				"the producer gate's own rule, restated on the consumer side. Name it with a package-level " +
				"string const",
		})
	}
}

// reportUnreachedSeeds is the NET under the whole analysis, and it is what makes the
// inverted rule real rather than aspirational: a bus-delivered channel that this gate
// followed to NO receive at all has not been cleared, it has been LOST. Without this,
// every blind spot in the propagation above would read as a pass.
func (g *wireGraph) reportUnreachedSeeds() {
	for _, cs := range g.channelSites {
		if !cs.Bus || g.sinkSeeds[cs.Site] > 0 || g.seedFlagged[cs.Site] {
			continue
		}
		name := cs.Var
		if name == "" {
			name = "<not assigned to a variable>"
		}
		g.findings = append(g.findings, wireFinding{
			Site: cs.Site, Channel: name, Name: cs.Name,
			Why: "is delivered by messageBus.deliverSignal, and this gate followed it to NO receive target " +
				"at all. Either the channel is opened and never read — a defect of its own — or it leaves " +
				"this analysis somewhere the gate cannot see, and a channel it cannot see is one it cannot " +
				"clear. Restructure so the receive is visible, or extend the analyser in the same commit",
		})
	}
}

func (g *wireGraph) taintParam(fi *wireFuncInfo, idx int, seeds wireSeeds) bool {
	m := g.paramTaint[fi.id]
	if m == nil {
		m = map[int]wireSeeds{}
		g.paramTaint[fi.id] = m
	}
	if m[idx] == nil {
		m[idx] = wireSeeds{}
	}
	return g.absorb(m[idx], seeds)
}

func (g *wireGraph) taintField(key string, seeds wireSeeds) bool {
	if g.fieldTaint[key] == nil {
		g.fieldTaint[key] = wireSeeds{}
	}
	return g.absorb(g.fieldTaint[key], seeds)
}

func (g *wireGraph) absorb(dst, src wireSeeds) bool {
	grew := false
	for s := range src {
		if !dst[s] {
			dst[s] = true
			grew = true
		}
	}
	if grew {
		g.grew = true
	}
	return grew
}

// ---------------------------------------------------------------------------
// wireEnv: one function's local view.
// ---------------------------------------------------------------------------

// wireEnv analyses ONE function. Everything it derives from the syntax — parents,
// lexical scopes, declared types — is rebuilt per pass and is cheap; the only state
// that crosses functions is the graph's paramTaint and fieldTaint.
//
// SCOPES ARE PER-FuncLit, and that is not a nicety. pumpPark binds THREE AddReceive
// closures whose parameter is called `c` and whose bodies declare `var raw any`,
// `var req activityLeaseRequest` and `var fin activityFinishedSignal`. A flat
// name→type map would let the last `var raw any` answer for all three, and the gate
// would clear a struct target it is looking straight at. Scope 0 is the function
// body; each FuncLit gets its own, chained to its parent.
//
// THEY ARE NOT FULLY LEXICAL: a plain BLOCK opens no scope, so two blocks of one body
// still share one key. That was a hole that failed green until declare() was given its
// conflict rule; the rule, and why it was preferred to a scope per block, are in
// declare's own comment, and the residual imprecision is blind spot 4 in the header.
type wireEnv struct {
	g  *wireGraph
	fi *wireFuncInfo

	parents  map[ast.Node]ast.Node
	scopeOf  map[ast.Node]int
	scopePar []int
	nodes    []ast.Expr
	imports  map[string]bool

	local map[string]wireSeeds // scopeKey -> seeds
	decls map[string]ast.Expr  // scopeKey -> declared type
	types map[string]string    // scopeKey -> declared struct type NAME
}

func scopeKey(scope int, name string) string { return strconv.Itoa(scope) + "|" + name }

func newWireEnv(g *wireGraph, fi *wireFuncInfo) *wireEnv {
	e := &wireEnv{
		g: g, fi: fi,
		parents:  map[ast.Node]ast.Node{},
		scopeOf:  map[ast.Node]int{},
		scopePar: []int{-1},
		imports:  map[string]bool{},
		local:    map[string]wireSeeds{},
		decls:    map[string]ast.Expr{},
		types:    map[string]string{},
	}
	for _, imp := range fi.unit.File.Imports {
		e.imports[wireImportName(imp)] = true
	}
	e.index()
	e.seed()
	return e
}

func wireImportName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	p, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	return path.Base(p)
}

// index walks the declaration once, recording each node's parent and lexical scope,
// the declared type of every named parameter and `var`, and the expression list the
// classification pass iterates.
func (e *wireEnv) index() {
	var stack []ast.Node
	cur := 0
	scopes := []int{0}
	e.declareFields(e.fi.decl.Recv, 0)
	e.declareFields(e.fi.decl.Type.Params, 0)
	ast.Inspect(e.fi.decl, func(n ast.Node) bool {
		if n == nil {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if _, ok := last.(*ast.FuncLit); ok {
				scopes = scopes[:len(scopes)-1]
				cur = scopes[len(scopes)-1]
			}
			return true
		}
		if len(stack) > 0 {
			e.parents[n] = stack[len(stack)-1]
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			e.scopePar = append(e.scopePar, cur)
			cur = len(e.scopePar) - 1
			scopes = append(scopes, cur)
			e.declareFields(lit.Type.Params, cur)
		}
		e.scopeOf[n] = cur
		e.declareStmt(n, cur)
		if x, ok := n.(ast.Expr); ok {
			e.nodes = append(e.nodes, x)
		}
		stack = append(stack, n)
		return true
	})
}

func (e *wireEnv) declareFields(fl *ast.FieldList, scope int) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			e.declare(scope, n.Name, f.Type)
		}
	}
}

func (e *wireEnv) declareStmt(n ast.Node, scope int) {
	switch v := n.(type) {
	case *ast.ValueSpec:
		if v.Type == nil {
			return
		}
		for _, nm := range v.Names {
			e.declare(scope, nm.Name, v.Type)
		}
	case *ast.AssignStmt:
		if len(v.Lhs) != 1 || len(v.Rhs) != 1 {
			return
		}
		id, ok := v.Lhs[0].(*ast.Ident)
		if !ok {
			return
		}
		if cl, ok := v.Rhs[0].(*ast.CompositeLit); ok {
			e.declare(scope, id.Name, cl.Type)
		}
	}
}

// declare records the syntactic type of one name in one scope, and NEVER lets a
// later declaration of that name ERASE an earlier stricter one.
//
// THE DEFEAT THIS CLOSES, which a review found by writing it in production code.
// The scopes above are opened for a FuncLit and for nothing else, so two blocks of
// one function body — an if/else, two switch cases, two select comm clauses, a
// GetVersion's two arms — share one scope key. The map was last-write-wins, so:
//
//	var raw operatorPauseSignal   // the receive target, a struct: the defect
//	pauseCh.Receive(ctx, &raw)
//	if !decoded { var raw any; _ = raw }   // silences the gate, four lines later
//
// compiled, vetted, linted, and left this gate GREEN with its vacuity log unchanged,
// because okReceiveTarget reads e.decls AFTER index() has finished and saw only the
// LAST `var raw`. The natural two-arm workflow.GetVersion shape — a pre-change arm's
// struct and a new arm's `var raw any`, same local name — is that shape written by
// accident, which is exactly the shape Task 3 removed and a future wave will re-add.
//
// WHY THE CONFLICT RULE AND NOT PER-BLOCK SCOPES. Opening a scope for every
// *ast.BlockStmt / CaseClause / CommClause was the other candidate. It is the more
// faithful model and it is the one that is easier to get WRONG later, for two
// measured reasons. First, it is an ENUMERATION, and the enumeration is already
// incomplete: `if x := T{}; ok {…}` and `for x := T{}; …` declare into the statement's
// own implicit scope, which is not a BlockStmt, so two such statements side by side in
// one block would still collide — the same hole, one construct over, and every future
// construct is another chance to miss one. Second, scopeOf drives the TAINT binds as
// well as the declarations, so narrowing it would make `ch = …` inside an if-block
// bind into a scope that dies at the closing brace — a channel that flows OUT of a
// block would lose its taint, which is a NEW way to go quiet. The conflict rule is
// construct-blind: it holds for every block shape there is and every one there will
// be, and it moves in one direction only.
//
// THE COST, stated rather than hidden: two genuinely different locals of one name in
// one function, one of them `any`, now answer as the stricter one, so a legitimate
// `any` receive can be reddened by an unrelated struct named the same. That is a
// FALSE POSITIVE, remedied by renaming one local, and it is the direction this file
// resolves every uncertainty in — see blind spot 4 in the header.
func (e *wireEnv) declare(scope int, name string, typ ast.Expr) {
	if name == "" || name == "_" || typ == nil {
		return
	}
	key := scopeKey(scope, name)
	// Resolve toward NOT-`any`: a lenient declaration may be replaced, a strict one
	// never is.
	if prev, ok := e.decls[key]; ok && !wireDeclClearsAReceive(prev) {
		return
	}
	e.decls[key] = typ
	if tn := wireTypeName(typ); tn != "" {
		e.types[scopeKey(scope, name)] = tn
	} else {
		delete(e.types, scopeKey(scope, name))
	}
}

// seed plants the taint this function starts with: the channels it opens itself, and
// whatever a caller has already pushed onto its parameters.
func (e *wireEnv) seed() {
	params := wireParamNames(e.fi.decl)
	for idx, seeds := range e.g.paramTaint[e.fi.id] {
		if idx < 0 || idx >= len(params) || params[idx] == "" || params[idx] == "_" {
			continue
		}
		e.bind(0, params[idx], seeds)
	}
	for _, cs := range e.g.channelSites {
		if cs.FuncID == e.fi.id && cs.Bus && cs.Var != "" {
			e.bind(0, cs.Var, wireSeeds{cs.Site: true})
		}
	}
}

// wireParamNames flattens a declaration's parameter names in call order. The
// receiver is NOT a parameter: a method call's argument 0 is its first declared
// parameter.
func wireParamNames(decl *ast.FuncDecl) []string {
	var out []string
	if decl.Type.Params == nil {
		return out
	}
	for _, f := range decl.Type.Params.List {
		if len(f.Names) == 0 {
			out = append(out, "")
			continue
		}
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

func (e *wireEnv) bind(scope int, name string, seeds wireSeeds) bool {
	key := scopeKey(scope, name)
	if e.local[key] == nil {
		e.local[key] = wireSeeds{}
	}
	grew := false
	for s := range seeds {
		if !e.local[key][s] {
			e.local[key][s] = true
			grew = true
		}
	}
	return grew
}

// propagate runs the intra-function taint to a local fixpoint, silently.
func (e *wireEnv) propagate() {
	for range 8 {
		if !e.walk(false) {
			return
		}
	}
}

func (e *wireEnv) lookupLocal(scope int, name string) wireSeeds {
	for s := scope; s >= 0; s = e.scopePar[s] {
		if v := e.local[scopeKey(s, name)]; len(v) > 0 {
			return v
		}
	}
	return nil
}

func (e *wireEnv) lookupDecl(scope int, name string) ast.Expr {
	for s := scope; s >= 0; s = e.scopePar[s] {
		if v, ok := e.decls[scopeKey(s, name)]; ok {
			return v
		}
	}
	return nil
}

func (e *wireEnv) lookupType(scope int, name string) string {
	for s := scope; s >= 0; s = e.scopePar[s] {
		if v, ok := e.types[scopeKey(s, name)]; ok {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Classification: every use of a tracked channel, or a finding.
// ---------------------------------------------------------------------------

// walk visits every expression in the function, and for every one that is carrying
// a bus-delivered channel decides what is being DONE with it. There is no "ignore"
// arm: an occurrence this switch does not recognise is a finding.
func (e *wireEnv) walk(emit bool) bool {
	grew := false
	for _, x := range e.nodes {
		seeds := e.taintOf(x)
		if len(seeds) == 0 || e.isDeclarationSite(x) {
			continue
		}
		if e.classify(x, seeds, emit) {
			grew = true
		}
	}
	return grew
}

// taintOf reports which channel seeds an expression is carrying: a local or
// parameter directly, or a struct FIELD read off a receiver whose type is declared
// syntactically in this function.
func (e *wireEnv) taintOf(x ast.Expr) wireSeeds {
	switch v := x.(type) {
	case *ast.Ident:
		return e.lookupLocal(e.scopeOf[x], v.Name)
	case *ast.SelectorExpr:
		base, ok := v.X.(*ast.Ident)
		if !ok {
			return nil
		}
		tn := e.lookupType(e.scopeOf[x], base.Name)
		if tn == "" || !e.g.structs[e.fi.unit.Scope][tn] {
			return nil
		}
		return e.g.fieldTaint[e.fi.unit.Scope+"|"+tn+"."+v.Sel.Name]
	}
	return nil
}

// isDeclarationSite skips the two places an identifier is spelled without being
// used: a parameter or field NAME, and the selector half of `x.field`.
func (e *wireEnv) isDeclarationSite(x ast.Expr) bool {
	switch p := e.parents[x].(type) {
	case *ast.Field:
		return true
	case *ast.SelectorExpr:
		return p.Sel == x
	}
	return false
}

func (e *wireEnv) classify(x ast.Expr, seeds wireSeeds, emit bool) bool {
	p := e.parents[x]
	for {
		pe, ok := p.(*ast.ParenExpr)
		if !ok {
			break
		}
		x, p = pe, e.parents[pe]
	}
	switch pv := p.(type) {
	case *ast.SelectorExpr:
		return e.classifyMethod(pv, x, seeds, emit)
	case *ast.CallExpr:
		return e.classifyArg(pv, x, seeds, emit)
	case *ast.KeyValueExpr:
		return e.classifyFieldStore(pv, x, seeds, emit)
	case *ast.AssignStmt:
		return e.classifyAssign(pv, x, seeds, emit)
	case *ast.ValueSpec:
		return e.classifyValueSpec(pv, x, seeds, emit)
	}
	e.flag(x, seeds, whyUnfollowable, emit)
	return false
}

// wireReceiveTargetIndex maps an SDK ReceiveChannel method to the argument position
// of its value pointer. These four are the whole of the receiving surface; any other
// method called on a tracked channel is a finding, because a method the gate cannot
// classify is a receive target it cannot see.
func wireReceiveTargetIndex(method string) (int, bool) {
	switch method {
	case "Receive":
		return 1, true
	case "ReceiveAsync", "ReceiveAsyncWithMoreFlag":
		return 0, true
	case "ReceiveWithTimeout":
		return 2, true
	}
	return 0, false
}

func (e *wireEnv) classifyMethod(sel *ast.SelectorExpr, x ast.Expr, seeds wireSeeds, emit bool) bool {
	call, ok := e.parents[sel].(*ast.CallExpr)
	if sel.X != x || !ok || call.Fun != sel {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	idx, ok := wireReceiveTargetIndex(sel.Sel.Name)
	if !ok {
		e.flag(x, seeds, "is delivered by messageBus.deliverSignal, and this gate does not recognise the method "+
			sel.Sel.Name+" called on it. It knows the SDK's four receive methods (Receive, ReceiveAsync, "+
			"ReceiveAsyncWithMoreFlag, ReceiveWithTimeout) and nothing else, because a method it cannot "+
			"classify is a receive target it cannot see", emit)
		return false
	}
	if emit {
		for s := range seeds {
			e.g.sinkSeeds[s]++
		}
	}
	if idx >= len(call.Args) {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	if !e.okReceiveTarget(call.Args[idx]) {
		e.flag(x, seeds, whyStructTarget, emit)
	}
	return false
}

// okReceiveTarget reports whether a receive's value pointer is one of the two shapes
// the SDK's ByteSlicePayloadConverter can actually fill: &v for a `var v any`, or a
// []byte in either spelling. Everything else — a struct, a named type, a map — drops
// the message.
func (e *wireEnv) okReceiveTarget(arg ast.Expr) bool {
	scope := e.scopeOf[arg]
	if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.AND {
		id, ok := u.X.(*ast.Ident)
		return ok && wireIsAnyOrBytes(e.lookupDecl(scope, id.Name))
	}
	if id, ok := arg.(*ast.Ident); ok {
		star, ok := e.lookupDecl(scope, id.Name).(*ast.StarExpr)
		return ok && wireIsAnyOrBytes(star.X)
	}
	return false
}

// wireDeclClearsAReceive reports whether a DECLARED type is one that okReceiveTarget
// would clear — `any`/[]byte (passed as &v) or a pointer to one (passed as v). It is
// the leniency half of declare's conflict rule, and it must stay the exact inverse of
// what okReceiveTarget flags: if it called something lenient that okReceiveTarget
// clears, a `var p *any` could pin the key and a later struct of the same name would
// be silenced — the defeat, re-opened one indirection down.
func wireDeclClearsAReceive(t ast.Expr) bool {
	if wireIsAnyOrBytes(t) {
		return true
	}
	star, ok := t.(*ast.StarExpr)
	return ok && wireIsAnyOrBytes(star.X)
}

func wireIsAnyOrBytes(t ast.Expr) bool {
	switch v := t.(type) {
	case *ast.Ident:
		return v.Name == "any"
	case *ast.InterfaceType:
		return v.Methods == nil || len(v.Methods.List) == 0
	case *ast.ArrayType:
		if v.Len != nil {
			return false
		}
		el, ok := v.Elt.(*ast.Ident)
		return ok && el.Name == "byte"
	}
	return false
}

func (e *wireEnv) classifyArg(call *ast.CallExpr, x ast.Expr, seeds wireSeeds, emit bool) bool {
	idx := slices.IndexFunc(call.Args, func(a ast.Expr) bool { return a == x })
	if idx < 0 {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddReceive" && idx == 0 {
		return e.bindSelectorCallback(call, x, seeds, emit)
	}
	callees := e.resolveCallees(call.Fun)
	if len(callees) == 0 {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	grew := false
	for _, fi := range callees {
		if idx >= len(wireParamNames(fi.decl)) {
			e.flag(x, seeds, whyUnfollowable, emit)
			continue
		}
		if e.g.taintParam(fi, idx, seeds) {
			grew = true
		}
	}
	return grew
}

// bindSelectorCallback is receive FORM 3: workflow.Selector.AddReceive hands the
// channel to a callback, and the receive happens on the callback's PARAMETER. A gate
// that did not follow this would see the pump's park and the child's grant arm as
// channels with no receive at all.
func (e *wireEnv) bindSelectorCallback(call *ast.CallExpr, x ast.Expr, seeds wireSeeds, emit bool) bool {
	if len(call.Args) < 2 {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	lit, ok := call.Args[1].(*ast.FuncLit)
	if !ok || lit.Type.Params == nil || len(lit.Type.Params.List) == 0 || len(lit.Type.Params.List[0].Names) == 0 {
		e.flag(x, seeds, "is handed to Selector.AddReceive with a callback this gate cannot read — it must be a "+
			"function literal whose first parameter names the channel, or the receive inside it is invisible here", emit)
		return false
	}
	name := lit.Type.Params.List[0].Names[0].Name
	if name == "_" {
		return false // the callback ignores the channel; any receive is on the outer variable
	}
	return e.bind(e.scopeOf[lit], name, seeds)
}

// resolveCallees finds the declarations a call may reach, WITHIN the calling file's
// package. A method is matched by NAME because the gate loads no types: two methods
// of one name in one package both receive the taint, which over-approximates toward
// RED and never away from it.
func (e *wireEnv) resolveCallees(fun ast.Expr) []*wireFuncInfo {
	switch v := fun.(type) {
	case *ast.Ident:
		return e.g.funcs[e.fi.unit.Scope][v.Name]
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok && e.imports[id.Name] {
			return nil // another package: out of the corpus, therefore unfollowable
		}
		return e.g.funcs[e.fi.unit.Scope][v.Sel.Name]
	case *ast.IndexExpr:
		return e.resolveCallees(v.X)
	case *ast.IndexListExpr:
		return e.resolveCallees(v.X)
	case *ast.ParenExpr:
		return e.resolveCallees(v.X)
	}
	return nil
}

func (e *wireEnv) classifyFieldStore(kv *ast.KeyValueExpr, x ast.Expr, seeds wireSeeds, emit bool) bool {
	cl, ok := e.parents[kv].(*ast.CompositeLit)
	key, keyOK := kv.Key.(*ast.Ident)
	if kv.Value != x || !ok || !keyOK {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	tn := wireTypeName(cl.Type)
	if tn == "" || !e.g.structs[e.fi.unit.Scope][tn] {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	return e.g.taintField(e.fi.unit.Scope+"|"+tn+"."+key.Name, seeds)
}

func (e *wireEnv) classifyAssign(as *ast.AssignStmt, x ast.Expr, seeds wireSeeds, emit bool) bool {
	if slices.Contains(as.Lhs, x) {
		// The DEFINITION site of the tracked variable (`ch := workflow.GetSignalChannel(…)`,
		// or a later reassignment). Nothing is being done with the channel here; the taint is
		// monotone, so a reassignment can only widen it.
		return false
	}
	idx := slices.IndexFunc(as.Rhs, func(r ast.Expr) bool { return r == x })
	if idx < 0 || len(as.Lhs) != len(as.Rhs) {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	id, ok := as.Lhs[idx].(*ast.Ident)
	if !ok {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	if id.Name == "_" {
		return false
	}
	return e.bind(e.scopeOf[as], id.Name, seeds)
}

func (e *wireEnv) classifyValueSpec(vs *ast.ValueSpec, x ast.Expr, seeds wireSeeds, emit bool) bool {
	idx := slices.IndexFunc(vs.Values, func(r ast.Expr) bool { return r == x })
	if idx < 0 || idx >= len(vs.Names) {
		e.flag(x, seeds, whyUnfollowable, emit)
		return false
	}
	if vs.Names[idx].Name == "_" {
		return false
	}
	return e.bind(e.scopeOf[vs], vs.Names[idx].Name, seeds)
}

func (e *wireEnv) flag(x ast.Expr, seeds wireSeeds, why string, emit bool) {
	if !emit {
		return
	}
	for s := range seeds {
		e.g.seedFlagged[s] = true
	}
	e.g.findings = append(e.g.findings, wireFinding{
		Site:    e.g.pos(x.Pos()),
		Channel: wireExprText(x),
		Name:    e.g.seedNames(seeds),
		Why:     why,
	})
}

// seedNames names the signal(s) a tracked expression may be carrying, so a finding
// says WHICH channel it is about and not merely where it is.
func (g *wireGraph) seedNames(seeds wireSeeds) string {
	set := map[string]bool{}
	for _, cs := range g.channelSites {
		if seeds[cs.Site] {
			set[cs.Name] = true
		}
	}
	return strings.Join(slices.Sorted(maps.Keys(set)), ",")
}

func wireExprText(x ast.Expr) string {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return wireExprText(v.X) + "." + v.Sel.Name
	}
	return "<expression>"
}
