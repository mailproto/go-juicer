// Generates random email-like documents, inlines each with juice, and writes
// one JSON object per line for TestDifferential to replay against Go.
//
//   node differential.mjs --seed 1 --count 10000 > corpus.jsonl
//
// The generator sticks to what this package implements and steers clear of
// its deliberate divergences from juice (README), so every mismatch it finds
// is a bug or an undocumented divergence.
import juice from 'juice';

const arg = (name, def) => {
  const i = process.argv.indexOf(`--${name}`);
  return i < 0 ? def : Number(process.argv[i + 1]);
};
const seed = arg('seed', 1);
const count = arg('count', 1000);

// mulberry32: small, fast and seedable, so a corpus can be regenerated exactly.
let state = seed >>> 0;
function rand() {
  state = (state + 0x6d2b79f5) >>> 0;
  let t = state;
  t = Math.imul(t ^ (t >>> 15), t | 1);
  t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}
const chance = (p) => rand() < p;
const int = (lo, hi) => lo + Math.floor(rand() * (hi - lo + 1));
const pick = (xs) => xs[Math.floor(rand() * xs.length)];
const times = (lo, hi, f) => Array.from({ length: int(lo, hi) }, f);
const sp = () => pick(['', '', '', ' ', '\n', '  ']);

const CLASSES = ['a', 'b', 'c', 'btn', 'x-y', 'Hero'];
const IDS = ['i1', 'i2', 'main'];
const TAGS = ['table', 'tr', 'td', 'th', 'tbody', 'thead', 'caption', 'p', 'div', 'span', 'a', 'b',
  'strong', 'em', 'h1', 'h2', 'ul', 'li', 'font', 'center', 'blockquote', 'section'];
const VOID = ['img', 'br', 'hr', 'col', 'input'];
const WORDS = ['hello', 'world', 'Sale', 'now', '50%', 'off', 'a&amp;b', '&nbsp;', '&copy;', '&#39;',
  'x & y', '"quoted"', "it's", 'é', '→', '{{name}}', '<%= x %>'];

const VALUES = {
  'color': ['red', '#123', '#AbCdEf', 'rgb(1, 2,3)', 'var(--c)', 'var(--missing, blue)', 'VAR(--c)', 'inherit'],
  'background-color': ['#fff', 'red', 'transparent', 'var(--c)'],
  'background-image': ['url(a.png)', 'url("b.png")', "url('c.png')", 'none', 'linear-gradient(red, blue)', 'url( d.png )'],
  'background': ['#fff url(x.png) no-repeat', 'red'],
  'width': ['100px', '50%', 'auto', '0', '600px', '100% '],
  'height': ['80px', 'auto', '10%'],
  'text-align': ['center', 'left', 'right'],
  'vertical-align': ['top', 'middle'],
  'margin': ['0', '0 auto', '1px 2px 3px 4px'],
  'padding': ['0', '10px', '0 0 12px 0'],
  'font-family': ['"Helvetica Neue", Arial, sans-serif', "'Open Sans'", 'Georgia'],
  'font-size': ['12px', '1.2em', '100%'],
  'font-weight': ['bold', '700'],
  'border': ['1px solid #000', '0', 'none'],
  'display': ['block', 'none', 'inline-block'],
  'line-height': ['1.5', '20px'],
  'mso-line-height-rule': ['exactly'],
  'Color': ['blue'],
  // Read by ::before/::after with inlinePseudoElements.
  'content': ['"x"', "'\\2192 '", 'none', 'url(a.png)', '"(" attr(class) ")"', 'counter(n) ". "',
    'counter(n, upper-roman)', '"a" var(--c, "b")', '"<&>"'],
  'counter-reset': ['n', 'n 3', 'n -1 m'],
  'counter-increment': ['n', 'n 2', 'm'],
};
const PROPS = Object.keys(VALUES);

function decl(inline) {
  if (!inline && chance(0.08)) return `--c:${sp()}${pick(['#abc', 'green', 'rgb(0,0,0)'])}`;
  const prop = pick(PROPS);
  let v = pick(VALUES[prop]);
  if (chance(0.15)) v += pick([' !important', '!important', ' !IMPORTANT', ' ! important']);
  if (!inline && chance(0.05)) v = pick(['/* c */ ', '']) + v + pick([' /* t */', '']);
  return `${prop}${sp()}:${sp()}${v}`;
}
const decls = (inline) => times(inline ? 1 : 0, 4, () => (!inline && chance(0.03) ? '/* juice ignore next */' : '') + decl(inline)).join(`;${sp()}`) + (chance(0.5) ? ';' : '');

function compound() {
  let s = '';
  if (chance(0.6)) s += pick([...TAGS, 'img', '*']);
  if (chance(0.5)) s += '.' + pick(CLASSES);
  if (chance(0.1)) s += '.' + pick(CLASSES);
  if (chance(0.15)) s += '#' + pick(IDS);
  if (chance(0.1)) s += pick(['[width]', '[width="600"]', '[class~=a]', '[href^=http]', '[data-x]', '[align=center]']);
  if (chance(0.15)) {
    s += pick([':first-child', ':last-child', ':nth-child(2n+1)', ':nth-child(odd)', ':not(.a)', ':is(.a, .b)',
      ':where(p)', ':hover', ':focus', ':first-of-type', ':empty', ':only-child', ':visited', '::before', ':after']);
  }
  return s || pick(TAGS);
}
function selector() {
  return times(1, 2, () => times(1, 3, compound).join(pick([' ', ' ', ' > ', ' + ', ' ~ ', '>']))
    + (chance(0.08) ? pick(['::before', '::after', ':before']) : ''))
    .join(`,${sp()}`);
}
const rule = (depth = 0) => {
  let body = decls(false);
  // CSS nesting, which juice flattens through postcss-nesting.
  if (depth < 2 && chance(0.12)) {
    body += ';' + times(1, 2, () => nested(depth + 1)).join(sp());
    if (chance(0.3)) body += sp() + decls(false);
  }
  return `${selector()}${sp()}{${sp()}${body}${sp()}}`;
};
function nested(depth) {
  if (chance(0.2)) return `@media ${pick(['print', '(max-width:600px)'])}{${decls(false)}${chance(0.3) ? ';' + rule(depth) : ''}}`;
  const sel = pick(['& ', '&', '', '> ', '& > ', '&:hover', '&.' + pick(CLASSES), pick(TAGS) + '&', '&:first-child', ':not(&) ']);
  return rule(depth).replace(/^[^{]*/, (s) => sel + (sel.endsWith('&') || sel.endsWith(':hover') || sel.endsWith('-child') ? '' : compound()) + sp());
}

function atRule() {
  return pick([
    () => `@media ${pick(['only screen and (max-width:600px)', '(prefers-color-scheme: dark)', 'print'])}{${times(1, 2, rule).join('')}}`,
    () => `@font-face{font-family:"X";src:url(x.woff) format("woff")}`,
    () => `@keyframes k{from{opacity:0}50%{opacity:.5}to{opacity:1}}`,
    () => `@import url(x.css);`,
    () => `@supports (display:grid){${rule()}}`,
    () => `@layer base;`,
    () => `@layer ${pick(['base', 'theme', ''])}{${times(1, 2, rule).join('')}}`,
    () => `@container ${pick(['(min-width:400px)', 'card (max-width: 30em)'])}{${times(1, 2, rule).join('')}}`,
    () => `@charset "utf-8";`,
  ])();
}
function stylesheet() {
  const parts = times(1, 7, () => (chance(0.15) ? atRule() : rule()));
  if (chance(0.1)) parts.splice(int(0, parts.length), 0, '/* comment */');
  // juice's ignore comments, including unbalanced ones.
  if (chance(0.06)) parts.splice(int(0, parts.length), 0, pick(['/* juice ignore next */', '/* juice start ignore */', '/* juice end ignore */']));
  if (chance(0.02)) parts.unshift('/* juice ignore */');
  if (chance(0.1)) parts.unshift(`:root{--c:${pick(['#abc', 'teal'])}}`);
  return parts.join(pick(['', '\n', ' ']));
}

function attrs(tag) {
  const out = [];
  const add = (k, v) => out.push(v === null ? k : pick([`${k}="${v}"`, `${k}="${v}"`, `${k}='${v}'`, `${k}=${v}`]));
  if (chance(0.5)) add('class', times(1, 2, () => pick(CLASSES)).join(' '));
  if (chance(0.15)) add('id', pick(IDS));
  if (chance(0.25)) out.push(`style="${decls(true)}"`);
  if (['table', 'td', 'th', 'img'].includes(tag) && chance(0.2)) add('width', pick(['600', '100%', '50']));
  if (chance(0.08)) add('align', 'center');
  if (chance(0.05)) add('bgcolor', '#eee');
  if (chance(0.05)) add('data-x', 'y');
  if (chance(0.04)) out.push(pick(['data-juice-important', 'data-juice-important="false"', 'data-juice-important="true"']));
  if (chance(0.03)) out.push(pick(['data-juice-duplicates', 'data-juice-duplicates="false"']));
  if (tag === 'a') add('href', pick(['https://example.com/?a=1&b=2', '#i1', 'mailto:x@example.com']));
  if (tag === 'img') add('src', 'https://example.com/x.png');
  if (chance(0.03)) out.push('hidden');
  if (chance(0.02)) out.push(out[0] ?? 'class="a"'); // a repeated attribute
  return out.length ? ' ' + out.join(' ') : '';
}

function node(depth) {
  const r = rand();
  if (depth > 4 || r < 0.3) return times(1, 3, () => pick(WORDS)).join(' ');
  if (r < 0.36) return `<${pick(VOID)}${attrs('img')}>`;
  if (r < 0.38) return pick(['<!-- note -->', '<!--[if mso]><table><tr><td><![endif]-->', '<!--[if !mso]><!--><b>x</b><!--<![endif]-->']);
  if (r < 0.40) return pick(['</span>', '</p>', '</div>', '<p>', '<td>']); // stray or unclosed
  const tag = pick(TAGS);
  const open = chance(0.03) ? tag.toUpperCase() : tag;
  const kids = times(0, 3, () => node(depth + 1)).join(sp());
  const close = chance(0.05) ? '' : `</${tag}>`;
  return `<${open}${attrs(tag)}>${kids}${close}`;
}

function styleTag() {
  const attr = pick(['', '', ' type="text/css"', ' data-embed', ' media="screen"']);
  return `<style${attr}>${stylesheet()}</style>`;
}

function document() {
  const styles = times(1, 2, styleTag).join('');
  const body = times(1, 4, () => node(0)).join(sp());
  return pick([
    () => `${styles}${body}`,
    () => `<!DOCTYPE html><html><head><meta charset="utf-8">${styles}</head><body>${body}</body></html>`,
    () => `<html><head><title>t</title>${styles}</head><body style="margin:0">${body}${chance(0.3) ? styleTag() : ''}</body></html>`,
    () => `<table><tr><td>${styles}${body}</td></tr></table>`,
  ])();
}

// Options with a Go equivalent. Each is set occasionally, to its non-default.
const OPTIONS = {
  preserveImportant: true, removeStyleTags: false, applyWidthAttributes: false,
  applyHeightAttributes: false, applyAttributesTableElements: false, preserveMediaQueries: false,
  preserveFontFaces: false, preserveKeyFrames: false, preservePseudos: false, resolveCSSVariables: false,
  preserveContainerQueries: false, preserveLayers: false,
  inlinePseudoElements: true, xmlMode: true, inlineDuplicateProperties: true,
};

for (let i = 0; i < count; i++) {
  const html = document();
  const options = {};
  for (const [k, v] of Object.entries(OPTIONS)) if (chance(0.1)) options[k] = v;
  if (chance(0.05)) options.preservedSelectors = times(1, 2, () => pick(['.a', 'btn', '#main', 'td', 'x-y >']));
  if (chance(0.15)) {
    options.extraCss = stylesheet();
    if (chance(0.3)) options.insertPreservedExtraCss = pick([false, 'td', 'p', 'body']);
  }
  const row = { seed, i, html, options };
  try {
    row.out = juice(html, options);
  } catch (err) {
    row.err = `${err.name}: ${err.message}`;
  }
  process.stdout.write(JSON.stringify(row) + '\n');
}
