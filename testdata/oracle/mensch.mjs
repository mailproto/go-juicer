// Generates goldens for the mensch port from the mensch juice 9 depends on:
// the lexer's tokens, and each top-level node stringified as juice 9 preserves
// it. Inputs are testdata/mensch/in/*.css; goldens go to testdata/mensch/out.
//
//   node mensch.mjs            regenerate
//   node mensch.mjs --check    verify committed goldens match pinned mensch
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';

// mensch is juice 9's dependency, so resolve it from there.
const require = createRequire(createRequire(import.meta.url).resolve('juice-9'));
const mensch = require('mensch');
const lex = require('mensch/lib/lexer');

const DIR = path.resolve(import.meta.dirname, '..', 'mensch');
const check = process.argv.includes('--check');
const drift = [];
const written = new Set();

for (const f of (await fs.readdir(path.join(DIR, 'in'))).filter((f) => f.endsWith('.css')).sort()) {
  const css = await fs.readFile(path.join(DIR, 'in', f), 'utf8');
  const tokens = lex(css).map(({ type, name, value, text, prefix }) => ({ type, name, value, text, prefix }));
  const rules = mensch.parse(css, { comments: true }).stylesheet.rules;
  const text = rules.map((r) => mensch.stringify({ stylesheet: { rules: [r] } }, { comments: false, indentation: '  ' }));
  const body = JSON.stringify({ tokens, text }, null, 1) + '\n';
  const out = path.join(DIR, 'out', f.replace(/\.css$/, '.json'));
  written.add(out);
  if (check) {
    if ((await fs.readFile(out, 'utf8').catch(() => null)) !== body) drift.push(f);
  } else {
    await fs.mkdir(path.dirname(out), { recursive: true });
    await fs.writeFile(out, body);
  }
}

for (const f of await fs.readdir(path.join(DIR, 'out')).catch(() => [])) {
  const p = path.join(DIR, 'out', f);
  if (written.has(p)) continue;
  if (check) drift.push(`${f} (stale)`);
  else await fs.rm(p);
}

if (check && drift.length) {
  console.error(`mensch goldens out of date (${drift.length}):\n  ${drift.join('\n  ')}`);
  process.exit(1);
}
console.log(`${check ? 'checked' : 'wrote'} ${written.size} mensch goldens (mensch ${require('mensch/package.json').version})`);
