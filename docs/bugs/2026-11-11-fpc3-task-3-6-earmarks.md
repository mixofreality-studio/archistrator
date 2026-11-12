# FPC Plan 3, Task 3.6 earmarks (2026-11-11)

Review of commit e5ebd95b left these items open. None blocks Plan 3; each has an owner.

1. **CheckKind is not a sum type (GC-2).** `server/cmd/aiarch-state-mcp/checks.go` keeps
   `CheckKindRule/Command/Smoke/Scenario` as a string constant family and
   `checkRunner.Run` keeps a `default:` arm over it. Pre-existing shape that Task 3.6
   extended. Fold into Plan 6's typed task-spec work: make the check kind a
   `//sumtype:decl` sealed interface with a generated `UnmarshalJSON` that refuses
   unknown kinds, and drop the `default` arm.
2. **App lint debt** (pre-existing, lines Task 3.6 does not touch), to clear before the
   app lint gate is relied on:
   - `server/cmd/aiarch-state-mcp/run009_test.go:89` unused func `literalInput`
   - `server/cmd/aiarch-state-mcp/freshplankey_test.go:98` gocyclo 16 on `TestFreshPlanRecordedUnderItsIDRunsGreen`
   - `server/internal/taskspec_test.go:82` gocyclo 16 on `TestTaskSpecForServiceConstructionCommands`
3. **Own-project mcpClient scenarios skipped.** Two (2) generated mcpClient scenarios in
   `server/internal/client/mcp/operations/client_scenarios.gen_test.go` are dated E19
   skips, both until 2026-12-31.
