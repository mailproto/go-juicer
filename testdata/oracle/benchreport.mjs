// Benchmarks this package and juice on the same documents, on this machine,
// and prints a markdown table. CI posts it to the job summary of every push
// to main. Speedups are the headline because both sides run on the same
// machine; absolute times are labelled with the hardware and versions that
// produced them.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import juice from 'juice';
import { genEmail } from './gen.mjs';
import { shapes } from './shapes.mjs';

const ROOT = path.resolve(import.meta.dirname, '../..');
const ROUNDS = 5;

// Go benchmark name -> [label, document]. The email is BenchmarkInline's.
const docs = {
  Inline: ['marketing email, 185 selectors', genEmail(60, 213)],
  'Shapes/tiny': ['1 rule, 1 element', shapes.tiny()],
  'Shapes/small': ['transactional, 14 rules', shapes.small()],
  'Shapes/manyRulesFewNodes': ['800 rules, 2 elements', shapes.manyRulesFewNodes()],
  'Shapes/fewRulesManyNodes': ['2 rules, 9k elements', shapes.fewRulesManyNodes()],
  'Shapes/unbucketable': ['120 attribute-only selectors', shapes.unbucketable()],
  'Shapes/deepDescendant': ['60 deep descendant chains', shapes.deepDescendant()],
  'Shapes/large': ['200 rules, 16k elements', shapes.large()],
};

const median = (xs) => xs.slice().sort((a, b) => a - b)[Math.floor(xs.length / 2)];

function timeJuice(doc) {
  const n = doc.length > 200000 ? 10 : doc.length > 20000 ? 40 : 300;
  for (let i = 0; i < Math.min(n, 20); i++) juice(doc, {});
  const rounds = [];
  for (let r = 0; r < ROUNDS; r++) {
    const t0 = process.hrtime.bigint();
    for (let i = 0; i < n; i++) juice(doc, {});
    rounds.push(Number(process.hrtime.bigint() - t0) / 1e6 / n);
  }
  return median(rounds);
}

function timeGo() {
  const out = execFileSync('go', ['test', '-run', '^$', '-bench', '^(BenchmarkInline|BenchmarkShapes)$',
    '-benchmem', '-count', String(ROUNDS), '.'], { cwd: ROOT, encoding: 'utf8' });
  const runs = {};
  for (const line of out.split('\n')) {
    const m = line.match(/^Benchmark(\S+?)(?:-\d+)?\s+\d+\s+([\d.]+) ns\/op.*?(\d+) allocs\/op/);
    if (m) (runs[m[1]] ??= []).push([Number(m[2]) / 1e6, Number(m[3])]);
  }
  return Object.fromEntries(Object.entries(runs).map(([k, v]) => [k, {
    ms: median(v.map((x) => x[0])),
    allocs: median(v.map((x) => x[1])),
  }]));
}

const fmtMs = (ms) => ms.toFixed(ms < 0.1 ? 3 : ms < 10 ? 2 : ms < 100 ? 1 : 0);
const fmtBytes = (n) => (n < 1024 ? `${n} B` : `${(n / 1024).toFixed(n < 10240 ? 1 : 0)} KB`);

const go = timeGo();
const rows = Object.entries(docs).map(([name, [label, doc]]) => {
  const g = go[name];
  if (!g) throw new Error(`no Go result for Benchmark${name}`);
  const j = timeJuice(doc);
  return { label, bytes: doc.length, go: g.ms, allocs: g.allocs, juice: j, x: j / g.ms };
});

const goVersion = execFileSync('go', ['env', 'GOVERSION'], { encoding: 'utf8' }).trim();
const juiceVersion = JSON.parse(fs.readFileSync(path.join(import.meta.dirname, 'node_modules/juice/package.json'))).version;
const slowest = rows.reduce((a, b) => (a.x < b.x ? a : b));
const table = [
  `${os.cpus()[0].model.trim()}, ${goVersion}, Node ${process.versions.node}, juice ${juiceVersion}. ` +
    `Median of ${ROUNDS} runs; ms per document.`,
  '',
  '| document | size | this package | juice | speedup | allocs |',
  '|---|---|---|---|---|---|',
  ...rows.map((r) => `| ${r.label} | ${fmtBytes(r.bytes)} | ${fmtMs(r.go)} | ${fmtMs(r.juice)} | ${r.x.toFixed(1)}x | ${r.allocs} |`),
  '',
  `Narrowest margin: ${slowest.x.toFixed(1)}x (${slowest.label}).`,
].join('\n');

console.log(table);
