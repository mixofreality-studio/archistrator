# appgen Step 8: composegen — the generated composition root

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Checkbox steps.

**Goal:** `cmd/server/main.go`'s run() wiring becomes generated (`main.gen.go`): construction order, dependency threading, worker registration, server assembly — driven by the deployment model + contracts. Hand code shrinks to (a) per-component VARIANT CONSTRUCTORS folded into their packages, (b) one explicit `hooks.go` seam per container for the genuinely-compositional policy, (c) the existing hand seam files (authz.go, managerlog.go, mcp_mount.go, temporallog.go, config_adapter.go).

**HONEST ARCHITECTURE (supersedes the spec's "zero handwritten" ideal, consistent with its own "variants remain handwritten where genuinely policy"):** the emitter never contains policy; it emits calls to (1) generated DI constructors, (2) catalog satellite constructors, (3) package variant constructors named by bindings, and (4) a typed Hooks interface the hand hooks.go implements. Hook points (each justified): ResolveProfile(cfg) string (legacy toggles → profile; step-7 note), RepoResolvers (projectID→RepoRef policy), CredentialMinters glue if not foldable, WrapManagers (the logging seam), ExtraMounts (userinfo + /mcp assembly), ProvidesExtra (the git-store 3-port type assertion — unexpressible in the model). Everything else — ordering, threading, nil-dormancy wiring from presence semantics, conditional construction-worker registration, telemetry+shutdown scaffolding — is EMITTED.

**Variant constructor convention (bindings' `variant` → Go):** `New<Variant><Interface>` in the component's package, signature = (catalog satellite values for the binding's infra keys..., settings deps...) — the folding task defines each concrete signature and records it; the emitter derives the call from the binding + a small per-variant signature registry emitted... NO — keep it simpler: variants take a single generated `<pkg>DepsFor<Variant>` struct? DECISION for Task P1 design: variants take POSITIONAL args in binding order (infra values then settings), documented per variant in the folding inventory; the emitter and the folding task share the plan's inventory table as the contract.

## Global Constraints
- BEHAVIOR IDENTITY: same env ⇒ same boot behavior as today's hand main.go (worker set, dormancy warns, listen/shutdown). Gates: boot rig all-profile checks + live systemtests slice + full -short suite + Chrome.
- Fold-first, generate-second: Task A1 (folding) lands with the HAND main.go still in place (rewritten to call the new package variants — behavior-identical, reviewable); Task P1 (emitter) + A2 (adoption) then replace run().
- The folding inventory (build* → package variant) is authored from cmd/server/main.go as of step-7 HEAD; every moved function keeps its doc comments (provenance trimmed per repo comment norms).
- Platform release at the end (app-generator/v0.5.0); same branch/tag/push discipline; both repos appgen-step8.

### Task A1 (archistrator): policy folding into packages
Move from cmd/server into component packages as variant constructors (inventory — verify each against current main.go before moving):
- projectstate: NewGitLocalProjectStateAccess(repoURL) / NewGitHubProjectStateAccess(sc SourceControlCatalogAccess, account, apiBaseURL-derived webHost) — absorbing gitRepoLocator, local/cloudProjectCatalog, local/cloudCredentialMinter, projectStateGitAdapter, cloudPerProjectRepoURL/gitWebHost helpers (projectstate_git_adapter.go largely relocates).
- artifact: NewLocalGitArtifactAccess(repoURL) / NewGitHubArtifactAccess(appID, pem, apiBase, owner, repoURL, installationID) absorbing buildArtifactAccess + artifact_auth.go glue.
- constructionpipeline: NewGitHubActionsPipeline variant wrapper absorbing buildConstructionPipeline.
- sourcecontrol: NewGitHubSourceControl variant absorbing buildSourceControl (returns both surfaces).
- operatedruntime: profile selection absorbed into the existing NewProfiledOperatedRuntimeAccess call shape (variant Local/Real per binding).
- construction dry-run stubs (dryRunPipeline/dryRunArtifacts from construction_dryrun.go) → their RA packages as NewDryRunPipeline/NewDryRunArtifacts variants; selectConstructionDeps logic → emitted presence/profile wiring + a hook only if irreducible.
- security: authenticatedOnlyPDP → security-adjacent hand file stays (it's archistrator policy — keep in cmd/server hooks) — DECIDE: utilities/security package would make it platform; keep app-side.
Hand main.go rewritten to call the variants (build* funcs die); NO behavior change; encapsulation allowlist updates insertion-only as needed. Gates: full suite + boot rig + live slice.

### Task P1 (platform): composegen emitter
framework-go-app-generator/composegen: Generate(m, Config{ContainerKey, ModulePath, PackageName, EnvPrefix}) → main.gen.go with: imports; runGenerated(cfg *Config, hooks Hooks, logger) error implementing the ordered walk (telemetry catalog call → infra satellites per infrastructure decls → RA variants per bindings + resolved profile → engines (pure New<Engine>() from contracts' engine impls) → security via hook-provided PDP option → managers via generated DI constructors with deps threaded per contracts' deps lists (component deps from constructed values; plain deps from hooks/settings — plain-dep sourcing table: client.Client from the temporal satellite; funcs/scalars from Hooks/Config) → RegisterManagerWorker per manager with conditional registration from presence semantics → handlers+NewServer+ExtraMounts → http serve/shutdown); Hooks interface emitted; DormantWarnings logged. Golden (greenfield fixture extended with a minimal main-able deployment) + compile sandbox (build against stub packages proving the call shapes). The archistrator fidelity proof lives in A2 (real adoption), not goldens.

### Task A2 (archistrator): adoption
cmd/appgen emits main.gen.go for archistrator-server; cmd/server keeps: main.go (tiny: logger + LoadConfig + hooks + runGenerated call), hooks.go (the Hooks impl: profile resolution from legacy toggles incl. the postgres unconditional check + conditional construction-creds, repo resolvers, logging wrap, extra mounts, provides type-assertions), and the seam files. run()/all build* remnants DELETED. gen-main-check drift gate (Make + CI). Gates: parity of boot logs/worker set across profiles (dev-local, dryrun, cloud-shaped env vs old binary — compare the old binary's boot log from git stash build), full suite, live slice, boot rig + Chrome, playwright quick pass.

### Wrap: reviews per task; platform merge + tag app-generator/v0.5.0; archistrator merge → main + push; spec Step-8 outcome; memory/ledger.
