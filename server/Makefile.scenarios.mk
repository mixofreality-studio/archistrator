# Deterministic component testing (spec 2026-10-02 §4.3, §5.3) — included by
# server/Makefile.
#
# This is archistrator's own copy of method-assets' scaffold
# Makefile.scenarios.mk.tmpl (v0.10.0), with one difference: a seated app's module
# lives at its repo root, but archistrator's Go module is server/, so the paths are
# server-relative. testgen's defaults already assume that layout
# (-project ../.aiarch/state/project.json, -root ., -uitests ../uitests/generated),
# so no flags are passed. testgen runs at the framework-go-app-generator version
# go.mod pins, not an @version of its own, so a pin bump moves it with the rest of
# the codegen.
#
#   gen-tests        — (re)write every *_scenarios.gen_test.go and
#                      uitests/generated/**/*.spec.gen.ts from the committed
#                      per-component test plans; seed any missing hooks file
#                      (never overwrites one); prune generated files the plan
#                      no longer produces
#   gen-tests-check  — drift gate: fails on a stale or orphan generated file, a
#                      hook the plan references that is missing, or an orphan
#                      hook (spec §5.3). testgen -check compares on its own, so
#                      unlike the other gen-*-check gates there is no git diff.
#   test-scenarios   — run every TestScenario_* as a `go test -json` stream into
#                      test-results/go-test.jsonl (what the venue uploads); exits
#                      with go test's status, which a bare `| tee` would mask
#                      (and `set -o pipefail` is not portable to dash). -p 1:
#                      every package's scenario host opens the one fixed
#                      Temporal SQLite file (.temporal/aiarch-test.db), so two
#                      packages at once fail with SQLITE_BUSY (see `test` in
#                      Makefile)
.PHONY: gen-tests gen-tests-check test-scenarios

TESTGEN := github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/cmd/testgen

gen-tests:
	GOWORK=off go run $(TESTGEN)

gen-tests-check:
	GOWORK=off go run $(TESTGEN) -check

test-scenarios:
	mkdir -p test-results
	GOWORK=off go test -p 1 ./internal/... -run 'TestScenario_' -json > test-results/go-test.jsonl 2>&1; rc=$$?; cat test-results/go-test.jsonl; exit $$rc
