// Generates golden files from juice, the reference implementation.
//
// This is the ONLY thing that may write testdata/out. The Go test suite never
// regenerates goldens: doing so would certify the implementation against itself
// and destroy the oracle.
//
// Every fixture is run through every pinned juice version (TARGETS) under every
// option variant (testdata/variants.json), into out/<target>/<variant>/.
//
//   node generate.mjs            regenerate all goldens
//   node generate.mjs --check    verify committed goldens match pinned juice
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const ROOT = path.resolve(import.meta.dirname, '..');
const IN = path.join(ROOT, 'in');
const OUT = path.join(ROOT, 'out');

// Target name -> package name in package.json.
const TARGETS = { 'juice-12': 'juice', 'juice-9': 'juice-9' };

// Module-global settings on the juice client rather than per-call options.
// Without a reset between fixtures, one fixture's `client` overrides leak into
// every later golden.
const GLOBALS = [
  'ignoredPseudos', 'widthElements', 'heightElements', 'tableElements',
  'nonVisualElements', 'styleToAttribute', 'excludedProperties', 'codeBlocks',
];

const variants = JSON.parse(await fs.readFile(path.join(ROOT, 'variants.json'), 'utf8'));

async function* fixtures(dir) {
  let entries;
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch (e) {
    if (e.code === 'ENOENT') return;
    throw e;
  }
  for (const e of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* fixtures(p);
    else if (e.name.endsWith('.html')) yield p;
  }
}

async function* files(dir) {
  for (const e of await fs.readdir(dir, { withFileTypes: true }).catch(() => [])) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* files(p);
    else yield p;
  }
}

const check = process.argv.includes('--check');
const drift = [];
const written = new Set();
let count = 0;

for (const [target, pkg] of Object.entries(TARGETS)) {
  const { default: juice } = await import(pkg);
  const version = require(`${pkg}/package.json`).version;
  const stamp = `juice ${version}\nnode ${process.versions.node}\n`;
  const defaults = Object.fromEntries(
    GLOBALS.filter((k) => juice[k] !== undefined).map((k) => [k, structuredClone(juice[k])]),
  );

  for await (const file of fixtures(IN)) {
    const rel = path.relative(IN, file);
    let cfg = {};
    try {
      cfg = JSON.parse(await fs.readFile(file.replace(/\.html$/, '.json'), 'utf8'));
    } catch (e) {
      if (e.code !== 'ENOENT') throw new Error(`${rel}: bad options sidecar: ${e.message}`);
    }
    const html = await fs.readFile(file, 'utf8');

    for (const [variant, options] of Object.entries(variants)) {
      Object.assign(juice, structuredClone(defaults), cfg.client ?? {});
      let body, outPath;
      try {
        body = juice(html, { ...options, ...cfg.options });
        outPath = path.join(OUT, target, variant, rel);
      } catch (err) {
        body = `${err.name}: ${err.message}\n`;
        outPath = path.join(OUT, target, variant, rel.replace(/\.html$/, '.err'));
      }
      count++;
      written.add(outPath);

      if (check) {
        const prev = await fs.readFile(outPath, 'utf8').catch(() => null);
        if (prev !== body) drift.push(path.relative(OUT, outPath));
      } else {
        await fs.mkdir(path.dirname(outPath), { recursive: true });
        await fs.writeFile(outPath, body);
      }
    }
  }

  const stampPath = path.join(OUT, target, 'VERSION');
  written.add(stampPath);
  if (check) {
    const prev = await fs.readFile(stampPath, 'utf8').catch(() => null);
    if (prev !== stamp) drift.push(`${target}/VERSION (have ${JSON.stringify(prev)}, want ${JSON.stringify(stamp)})`);
  } else {
    await fs.mkdir(path.dirname(stampPath), { recursive: true });
    await fs.writeFile(stampPath, stamp);
  }
}

// A golden no fixture produces is stale, and would otherwise linger unnoticed.
for await (const f of files(OUT)) {
  if (written.has(f)) continue;
  if (check) drift.push(`${path.relative(OUT, f)} (stale)`);
  else await fs.rm(f);
}

const targets = Object.keys(TARGETS).join(', ');
if (check) {
  if (drift.length) {
    console.error(`goldens out of date (${drift.length}):\n  ${drift.join('\n  ')}`);
    console.error('\nrun `make goldens` and review the diff');
    process.exit(1);
  }
  console.log(`goldens up to date (${count} across ${targets})`);
} else {
  console.log(`wrote ${count} goldens across ${targets}`);
}
