# Deterministic component testing (spec 2026-10-02 §4.3, §5.3) — included by
# server/Makefile.
#
# This is archistrator's own copy of method-assets' scaffold
# Makefile.scenarios.mk.tmpl (binding model v2), with one difference: a seated app's module
# lives at its repo root, but archistrator's Go module is server/, so the paths are
# server-relative. testgen's defaults already assume that layout
# (-project ../.aiarch/state/project.json, -root ., -uitests ../uitests/generated),
# so no flags are passed. testgen runs at the framework-go-app-generator version
# go.mod pins, not an @version of its own, so a pin bump moves it with the rest of
# the codegen.
#
#   gen-tests        — (re)write every *_scenarios.gen_test.go and
#                      uitests/generated/**/*.spec.gen.ts from the committed
#                      per-component test plans; prune generated files the plan
#                      no longer produces. Nothing it writes is agent-owned: the
#                      subject of every scenario is built by the generated
#                      internal/scenariocompose package, and there are no hooks.
#   gen-tests-check  — drift gate: fails on a stale or orphan generated file, or on
#                      a hooks file (retired: ARCH-TEST-COMPOSE). testgen -check
#                      compares on its own, so unlike the other gen-*-check gates
#                      there is no git diff.
#   test-scenarios   — run every TestScenario_* as a `go test -json` stream into
#                      test-results/go-test.jsonl (what the venue uploads); exits
#                      with go test's status, which a bare `| tee` would mask
#                      (and `set -o pipefail` is not portable to dash). -p 1:
#                      every package's scenario host opens the one fixed
#                      Temporal SQLite file (.temporal/aiarch-test.db), so two
#                      packages at once fail with SQLITE_BUSY (see `test` in
#                      Makefile). -timeout $(GO_TEST_TIMEOUT) (Makefile): one
#                      scenario package outlasts go test's 10m default
#   test-discriminate — the null-subject run (TP-DISCRIMINATE): re-run every Go scenario once
#                      per stimulus operation with that operation nulled; a scenario that still
#                      passes would pass without the behavior it claims to test, and is printed
#                      as `TP-DISCRIMINATE <component> <scenario> <op>` (exit 1). Run it on a
#                      tree gen-tests-check has just passed. Machine-client scenarios null the
#                      Manager operation a call reaches in the app the test launches; web scenarios
#                      run through the scenario host (`go tool webscenarios`) with the Manager
#                      operation their step drives nulled in the server it launches.
#   surface-check    — for each browser surface, run its generated surface.spec.gen.ts alone (the
#                      `surface` Playwright project): every non-repeating element of the surface's
#                      UI design must be on its screen by the role and accessible name the design
#                      gives it. The scenario flows depend on it. Needs `npm ci` and the Playwright
#                      browser in ../uitests. Both web targets run under the scenario host
#                      (`go tool webscenarios`), which boots the composed app into the host's
#                      substrates, serves the arrange endpoint and tears everything down itself:
#                      Playwright's own webServer would SIGKILL the app and leak its Postgres
#                      container.
#   test-web         — run every browser surface's generated scenarios (the `scenarios` Playwright
#                      project, after the surface check it depends on) under the scenario host.
.PHONY: gen-tests gen-tests-check test-scenarios test-discriminate surface-check test-web

TESTGEN := github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/cmd/testgen

gen-tests:
	GOWORK=off go run $(TESTGEN)

gen-tests-check:
	GOWORK=off go run $(TESTGEN) -check

test-discriminate:
	GOWORK=off go run $(TESTGEN) -discriminate

test-scenarios:
	mkdir -p test-results
	GOWORK=off go test -p 1 -timeout $(GO_TEST_TIMEOUT) ./internal/... -run 'TestScenario_' -json > test-results/go-test.jsonl 2>&1; rc=$$?; cat test-results/go-test.jsonl; exit $$rc

surface-check:
	@found=0; for cfg in ../uitests/generated/*/playwright.config.gen.ts; do \
		[ -e "$$cfg" ] || continue; found=1; \
		GOWORK=off go tool webscenarios -C ../uitests -config "$${cfg#../uitests/}" -project surface || exit 1; \
	done; [ "$$found" = 1 ] || echo "surface-check: no browser surface to check"

test-web:
	@found=0; for cfg in ../uitests/generated/*/playwright.config.gen.ts; do \
		[ -e "$$cfg" ] || continue; found=1; \
		GOWORK=off go tool webscenarios -C ../uitests -config "$${cfg#../uitests/}" -project scenarios || exit 1; \
	done; [ "$$found" = 1 ] || echo "test-web: no browser surface to run"
