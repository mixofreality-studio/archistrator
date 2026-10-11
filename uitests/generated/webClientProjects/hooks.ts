// Hooks for webClientProjects scenario tests. Generated ONCE by testgen; owned by the construction agent.
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import * as fs from 'node:fs';
import * as http from 'node:http';
import * as net from 'node:net';
import * as os from 'node:os';
import * as path from 'node:path';
import { expect, test, type Page } from '@playwright/test';

// Hooked steps (scenario / seq → binding JSON):
//   manage-projects-P1 / 3: {"seq":3,"operation":"SetReviewPolicy","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"review-policy-option-checkpoints\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"review-policy-option-checkpoints\"},{\"visible\":\"review-policy-floor-note\"}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//   manage-projects-P1 / 4: {"seq":4,"operation":"SubmitResearchInput","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"home-base-open-plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"research-input-title\",\"value\":\"Founder brief\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"research-input-text\",\"value\":\"Teams lose track of commitments made in meetings; capture each action item with an owner and a due date, and remind the owner before it slips.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"research-input-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-screen\"}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//   manage-projects-P2 / 3: {"seq":3,"operation":"SetReviewPolicy","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"review-policy-option-checkpoints\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"review-policy-option-checkpoints\"},{\"visible\":\"review-policy-floor-note\"}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//   manage-projects-P2 / 4: {"seq":4,"operation":"SubmitResearchInput","inputs":[{"name":"action","value":"{\"kind\":\"click\",\"target\":\"home-base-open-plan\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"plan-row-requirements\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"research-input-title\",\"value\":\"Founder brief\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"fill\",\"target\":\"research-input-text\",\"value\":\"Teams lose track of commitments made in meetings; capture each action item with an owner and a due date, and remind the owner before it slips.\"}","schemaRef":"#/$defs/UIAction"},{"name":"action","value":"{\"kind\":\"click\",\"target\":\"research-input-submit\"}","schemaRef":"#/$defs/UIAction"}],"expect":{"result":"[{\"visible\":\"activity-screen\"}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}

// ── What every scenario runs against ─────────────────────────────────────────
//
// THE STACK is the real one, and these hooks own it (the generated config has no
// webServer block: hooks.before brings up whatever a scenario needs). Every scenario
// gets a stack of its own, so nothing one scenario did is read by the next — the
// LOCAL profile keeps ONE project per state repo, so a second CreateProject against
// the first scenario's repo would be refused:
//
//   - a fresh Temporal dev server (in memory);
//   - a fresh project repo: the LOCAL profile's state repo, an empty `main`;
//   - the Go server (cmd/server, built from this checkout) on the LOCAL profile over
//     both, dev-mode auth, at a port chosen once per run;
//   - the SPA: PLAYWRIGHT_TEST_BASE_URL when the harness serves it (its dev server
//     must proxy /api to UITESTS_API_PORT), otherwise the Vite dev server of this
//     checkout's webApp, started once per run and proxying /api to that port;
//   - Postgres is the harness's (UITESTS_POSTGRES_URL), shared.
//
// THE ONE DOUBLE is the coding agent: the `claude` first on the server's PATH is a
// stub that answers the boot preflight's `--version` and otherwise stays in flight
// until the server that started it is gone. SubmitResearchInput starts the design
// walk, whose first draft job is dispatched to it; no model is ever reached, and no
// manage-projects observable depends on a draft finishing.
//
// ARRANGEMENT is only the stack: every scenario starts signed in on an empty project
// catalog and does everything else through the SPA, as the generated spec drives it.

const REPO_ROOT = path.resolve(import.meta.dirname, '../../..');

const POSTGRES_URL =
  process.env['UITESTS_POSTGRES_URL'] ??
  'postgres://archistrator:archistrator@localhost:5432/archistrator?sslmode=disable';
/** Where each scenario's stack keeps its repo, logs and venue. */
const WORK_ROOT =
  process.env['UITESTS_WORK_DIR'] ?? path.join(os.tmpdir(), 'uitests-webclientprojects');

/** Bringing a stack up (a cold build included) is not the scenario's time. */
const ARRANGE_BUDGET_MS = 10 * 60_000;
/**
 * Server round trips (git-backed project state, a Temporal walk start) are slower
 * than Playwright's 5s default assertion timeout.
 */
const SERVER_TIMEOUT = 30_000;

// ── Processes ────────────────────────────────────────────────────────────────

const children = new Set<ChildProcess>();

process.on('exit', () => {
  for (const c of children) c.kill('SIGKILL');
});

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      srv.close(() => {
        if (addr !== null && typeof addr === 'object') resolve(addr.port);
        else reject(new Error('uitests: no free port'));
      });
    });
  });
}

function run(cmd: string, args: string[], cwd: string, env?: NodeJS.ProcessEnv): string {
  return execFileSync(cmd, args, {
    cwd,
    env: env ?? process.env,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

function git(cwd: string, ...args: string[]): string {
  return run(
    'git',
    ['-c', 'user.name=uitests', '-c', 'user.email=uitests@aiarch.local', ...args],
    cwd
  );
}

/** A binary the stack runs: the harness's prebuilt one, or built once per run from this checkout. */
const built = new Map<string, string>();
function binary(envName: string, pkg: string): string {
  const fromEnv = process.env[envName];
  if (fromEnv !== undefined && fromEnv !== '') return fromEnv;
  const cached = built.get(pkg);
  if (cached !== undefined) return cached;
  const out = path.join(WORK_ROOT, 'bin', path.basename(pkg));
  fs.mkdirSync(path.dirname(out), { recursive: true });
  run('go', ['build', '-o', out, pkg], path.join(REPO_ROOT, 'server'), {
    ...process.env,
    GOWORK: 'off',
  });
  built.set(pkg, out);
  return out;
}

function spawnLogged(
  cmd: string,
  args: string[],
  logFile: string,
  opts: { cwd: string; env: NodeJS.ProcessEnv }
): ChildProcess {
  const log = fs.openSync(logFile, 'a');
  const child = spawn(cmd, args, { cwd: opts.cwd, env: opts.env, stdio: ['ignore', log, log] });
  fs.closeSync(log);
  children.add(child);
  child.once('exit', () => children.delete(child));
  return child;
}

async function stopChild(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()));
  child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
  await exited;
  clearTimeout(timer);
}

async function waitFor(
  what: string,
  windowMs: number,
  probe: () => Promise<boolean>
): Promise<void> {
  const deadline = Date.now() + windowMs;
  while (Date.now() < deadline) {
    if (await probe()) return;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`uitests: ${what} did not happen within ${String(windowMs / 1000)}s`);
}

function httpOK(url: string): Promise<boolean> {
  return new Promise((resolve) => {
    const req = http.get(url, (res) => {
      res.resume();
      resolve(res.statusCode !== undefined && res.statusCode < 300);
    });
    req.on('error', () => resolve(false));
    req.setTimeout(2_000, () => req.destroy());
  });
}

function tcpOpen(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.connect(port, '127.0.0.1');
    sock.once('connect', () => {
      sock.destroy();
      resolve(true);
    });
    sock.once('error', () => resolve(false));
  });
}

// ── The SPA ──────────────────────────────────────────────────────────────────

/** The API port every scenario's server listens on: the port the SPA proxies /api to. */
let apiPort: number | undefined;
async function serverPort(): Promise<number> {
  if (apiPort !== undefined) return apiPort;
  const fromEnv = process.env['UITESTS_API_PORT'];
  apiPort = fromEnv !== undefined && fromEnv !== '' ? Number(fromEnv) : await freePort();
  return apiPort;
}

let spaURL: string | undefined;
async function spa(): Promise<string> {
  if (spaURL !== undefined) return spaURL;
  const fromEnv = process.env['PLAYWRIGHT_TEST_BASE_URL'];
  if (fromEnv !== undefined && fromEnv !== '') {
    spaURL = fromEnv;
    return spaURL;
  }
  const port = await freePort();
  const webApp = path.join(REPO_ROOT, 'webApp');
  fs.mkdirSync(WORK_ROOT, { recursive: true });
  const vite = spawnLogged(
    path.join(webApp, 'node_modules', '.bin', 'vite'),
    ['--host', '127.0.0.1', '--port', String(port), '--strictPort'],
    path.join(WORK_ROOT, 'vite.log'),
    {
      cwd: webApp,
      env: {
        ...process.env,
        ARCHISTRATOR_API_PROXY_TARGET: `http://127.0.0.1:${String(await serverPort())}`,
      },
    }
  );
  const url = `http://127.0.0.1:${String(port)}`;
  await waitFor('the SPA dev server answering', 120_000, async () => {
    if (vite.exitCode !== null) {
      throw new Error(
        `uitests: the SPA dev server exited ${String(vite.exitCode)} (${WORK_ROOT}/vite.log)`
      );
    }
    return httpOK(url);
  });
  spaURL = url;
  return url;
}

// ── The stack ────────────────────────────────────────────────────────────────

type Stack = { temporal: ChildProcess; server: ChildProcess };
let stack: Stack | undefined;

async function stopStack(): Promise<void> {
  const s = stack;
  stack = undefined;
  if (s === undefined) return;
  await stopChild(s.server);
  await stopChild(s.temporal);
}

/**
 * The project repo the LOCAL profile keeps the project in: a non-bare repository
 * (the venue requires it: its episode traces sit beside the git dir, never in it)
 * holding one empty commit on `main`, whose own checkout stays on a branch of its
 * own, so every project branch (main included) is only ever pushed to.
 */
function newProjectRepo(dir: string): string {
  const repo = path.join(dir, 'project');
  fs.mkdirSync(repo, { recursive: true });
  git(repo, 'init', '-q', '-b', 'checkout');
  git(repo, 'commit', '-q', '--allow-empty', '-m', 'seed');
  git(repo, 'branch', 'main');
  return repo;
}

/**
 * The venue's `claude`: answers the boot preflight's `--version`; any job it is
 * handed stays in flight until the server that started it is gone.
 */
function openVenue(dir: string): string {
  const venueDir = path.join(dir, 'venue');
  fs.mkdirSync(venueDir, { recursive: true });
  const stub = `#!${process.execPath}
'use strict';
// The venue's \`claude\` (uitests/generated/webClientProjects/hooks.ts).
if (process.argv.includes('--version')) {
  process.stdout.write('0.0.0 (scenario agent)\\n');
  process.exit(0);
}
const parent = process.ppid;
setInterval(() => {
  try {
    process.kill(parent, 0);
  } catch {
    process.exit(1);
  }
}, 1000);
`;
  fs.writeFileSync(path.join(venueDir, 'claude'), stub, { mode: 0o755 });
  return venueDir;
}

/** Bring up this scenario's stack: a fresh Temporal, a fresh repo, the server over both. */
async function bootStack(id: string): Promise<void> {
  await stopStack();
  const serverBin = binary('UITESTS_SERVER_BIN', './cmd/server');
  const stateTool = binary('UITESTS_STATE_MCP_BIN', './cmd/aiarch-state-mcp');
  const port = await serverPort();
  const dir = path.join(WORK_ROOT, `${id}-${String(Date.now())}`);
  fs.mkdirSync(dir, { recursive: true });
  const repo = newProjectRepo(dir);
  const venueDir = openVenue(dir);

  const [tPort, tHttp, tMetrics] = [await freePort(), await freePort(), await freePort()];
  const temporal = spawnLogged(
    'temporal',
    [
      'server',
      'start-dev',
      '--headless',
      '--ip',
      '127.0.0.1',
      '--port',
      String(tPort),
      '--http-port',
      String(tHttp),
      '--metrics-port',
      String(tMetrics),
      '--namespace',
      'aiarch-test',
      '--log-level',
      'error',
    ],
    path.join(dir, 'temporal.log'),
    { cwd: dir, env: process.env }
  );
  await waitFor('Temporal accepting connections', 60_000, () => tcpOpen(tPort));

  if (await tcpOpen(port)) {
    throw new Error(`uitests: port ${String(port)} is taken; the scenario's server needs it`);
  }
  const server = spawnLogged(serverBin, [], path.join(dir, 'server.log'), {
    cwd: dir,
    env: {
      PATH: `${venueDir}${path.delimiter}${process.env['PATH'] ?? ''}`,
      HOME: process.env['HOME'] ?? dir,
      USER: process.env['USER'] ?? 'uitests',
      TMPDIR: process.env['TMPDIR'] ?? os.tmpdir(),
      ARCHISTRATOR_LISTEN_ADDR: `:${String(port)}`,
      ARCHISTRATOR_POSTGRES_URL: POSTGRES_URL,
      ARCHISTRATOR_TEMPORAL_HOSTPORT: `127.0.0.1:${String(tPort)}`,
      ARCHISTRATOR_TEMPORAL_NAMESPACE: 'aiarch-test',
      ARCHISTRATOR_AUTH_DEV_MODE: 'true',
      ARCHISTRATOR_DEV_SUBJECT: 'dev-architect',
      ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL: 'true',
      ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL: `file://${repo}`,
      ARCHISTRATOR_CONSTRUCTION_DRYRUN: 'true',
      ARCHISTRATOR_STATE_MCP_BIN: stateTool,
    },
  });
  stack = { temporal, server };
  await waitFor('the server answering /healthz', 120_000, async () => {
    if (server.exitCode !== null) {
      throw new Error(`uitests: the server exited ${String(server.exitCode)} (${dir}/server.log)`);
    }
    return httpOK(`http://127.0.0.1:${String(port)}/healthz`);
  });
}

export const hooks = {
  // Every manage-projects scenario starts signed in (dev-mode auth injects the
  // principal, so the catalog renders the user's label) on an empty project catalog.
  async before(id: string, page: Page): Promise<void> {
    const budget = test.info().timeout;
    const started = Date.now();
    test.setTimeout(budget + ARRANGE_BUDGET_MS);
    await bootStack(id);
    await page.goto(await spa());
    await expect(page.getByTestId('projects-landing-screen')).toBeVisible({
      timeout: SERVER_TIMEOUT,
    });
    await expect(page.getByTestId('user-label')).toBeVisible();
    test.setTimeout(budget + (Date.now() - started));
  },
  async step(id: string, seq: number, page: Page): Promise<void> {
    switch (seq) {
      case 3:
        // SetReviewPolicy: the control renders server truth, never a local echo,
        // so the chosen preset reads back checked only once the write committed.
        await expect(page.getByTestId('review-policy-option-checkpoints')).toHaveAttribute(
          'aria-checked',
          'true',
          { timeout: SERVER_TIMEOUT }
        );
        return;
      case 4:
        // SubmitResearchInput: the corpus is saved and the walk started, so the
        // research panel (shown only while the project holds no corpus) is gone
        // and no refusal line was rendered under it.
        await expect(page.getByTestId('research-input')).toBeHidden({ timeout: SERVER_TIMEOUT });
        await expect(page.getByTestId('activity-research-error')).toBeHidden();
        return;
      default:
        throw new Error('uitests: no hook for ' + id + '/' + String(seq));
    }
  },
};
