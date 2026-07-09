#!/usr/bin/env node
// Screenshots pprof's web UI to produce a flame graph image. `go tool pprof`
// has no CLI flag that renders a flame graph directly to SVG/PNG (only the
// classic node-link call graph via -svg/-png/-dot, which is graphviz-based).
// The flame graph view in `go tool pprof -http` is client-side JS
// (d3-flame-graph) with no static-render mode, so this drives a headless
// Chromium against the pprof server instead of trying to reimplement the
// layout algorithm.
//
// CommonJS on purpose: Node's ESM resolver ignores NODE_PATH, and this
// container's Playwright install is only reachable via NODE_PATH (there is
// no local node_modules/package.json in this repo).
//
// `go tool pprof -http=127.0.0.1:0 ...` picks an ephemeral port but its
// startup log literally prints the requested ":0", not the bound port, and
// `go tool pprof` is itself a wrapper that forks the real pprof binary as a
// child (not an exec-replace) -- so this polls `pgrep`/`lsof` across the
// whole process subtree to find the actual listening port, and kills that
// same subtree (not just the direct child) when done.
//
// Usage: node flamegraph.js <bin> <profile> <out.png> [--route flamegraph|top]
'use strict';
const { spawn, execSync } = require('node:child_process');
const { chromium } = require('playwright');

const argv = process.argv.slice(2);
let route = 'flamegraph';
const routeIdx = argv.indexOf('--route');
if (routeIdx !== -1) {
  route = argv[routeIdx + 1];
  argv.splice(routeIdx, 2);
}
const outPng = argv.pop();
const pprofArgs = argv;

if (pprofArgs.length === 0 || !outPng) {
  console.error('usage: flamegraph.js <bin> <profile> <out.png> [--route flamegraph|top]');
  process.exit(1);
}

function descendants(rootPid) {
  const pids = [rootPid];
  for (let i = 0; i < pids.length; i++) {
    try {
      const out = execSync(`pgrep -P ${pids[i]}`, { encoding: 'utf8' }).trim();
      if (out) out.split('\n').forEach((s) => pids.push(Number(s)));
    } catch {
      /* pgrep exits non-zero when a pid has no children */
    }
  }
  return pids;
}

function findListenPort(rootPid) {
  for (const pid of descendants(rootPid)) {
    try {
      const lsof = execSync(`lsof -a -p ${pid} -i -P -n`, { encoding: 'utf8' });
      const m = lsof.match(/TCP (?:\*|127\.0\.0\.1):(\d+)\s+\(LISTEN\)/);
      if (m) return m[1];
    } catch {
      /* lsof exits non-zero when the pid has no matching fds yet */
    }
  }
  return null;
}

function killTree(rootPid) {
  for (const pid of descendants(rootPid)) {
    try {
      process.kill(pid, 'SIGKILL');
    } catch {
      /* already gone */
    }
  }
}

const proc = spawn('go', ['tool', 'pprof', '-http=127.0.0.1:0', '-no_browser', ...pprofArgs], {
  stdio: ['ignore', 'pipe', 'pipe'],
});
let stderrBuf = '';
proc.stdout.on('data', (d) => (stderrBuf += d.toString()));
proc.stderr.on('data', (d) => (stderrBuf += d.toString()));

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  const deadline = Date.now() + 20000;
  let port = null;
  while (!port && Date.now() < deadline) {
    port = findListenPort(proc.pid);
    if (!port) await sleep(300);
  }
  if (!port) {
    console.error('pprof http server did not start in time:\n' + stderrBuf);
    killTree(proc.pid);
    process.exit(1);
  }
  const url = `http://127.0.0.1:${port}`;

  const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
  try {
    const page = await browser.newPage({ viewport: { width: 1800, height: 1100 }, deviceScaleFactor: 2 });
    const path = route === 'top' || route === '' ? '/ui/' : `/ui/${route}`;
    await page.goto(url + path, { waitUntil: 'networkidle', timeout: 15000 });
    // the flame graph / graph view render async after the JSON payload loads
    await page.waitForTimeout(800);
    await page.screenshot({ path: outPng, fullPage: true });
    console.log('wrote', outPng);
  } finally {
    await browser.close();
    killTree(proc.pid);
  }
})().catch((e) => {
  console.error(e);
  killTree(proc.pid);
  process.exit(1);
});
