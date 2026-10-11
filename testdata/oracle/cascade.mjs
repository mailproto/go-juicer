// Checks that inlining preserves the cascade, with a browser as the judge.
// Each document is rendered twice in headless Chrome, as written and as
// inlined, and every element's computed style must come out the same.
//
// stdin:  one {"name","original","inlined"} JSON object per line
// stdout: one {"name","diffs"} object per line; diffs is empty when the two agree
//
// Chrome comes from CHROME_PATH. Scripts are disabled, network requests are
// blocked, the viewport is fixed at 1024x768, and pseudo-classes such as
// :hover are never active, so the only differences left are the ones inlining
// introduced.
import readline from 'node:readline';
import puppeteer from 'puppeteer-core';

// Elements that never render, matching the package's nonVisualElements.
const SKIP = ['head', 'title', 'base', 'link', 'style', 'meta', 'script', 'noscript'];
const MAX_DIFFS = 8;
// Values that come out of layout rather than the cascade, and so shift with
// any change to whitespace or wrappers; the aspect-ratio presentational hint
// that juice's width/height attributes add; and custom properties, which do
// not render and are dropped once resolved.
const IGNORE = new Set(['width', 'height', 'inline-size', 'block-size', 'perspective-origin',
  'transform-origin', 'aspect-ratio']);
const ignored = (p) => IGNORE.has(p) || p.startsWith('--');
const TIMEOUT_MS = 20000;

const launch = () => puppeteer.launch({
  executablePath: process.env.CHROME_PATH,
  headless: true,
  args: ['--no-sandbox', '--disable-gpu'],
});
let browser = await launch();

let page;
async function newPage() {
  if (page) await page.close().catch(() => {});
  page = await browser.newPage();
  await page.setJavaScriptEnabled(false);
  // Animated values depend on when they are read; hold every animation at
  // its start so both renders are sampled at the same point.
  const cdp = await page.createCDPSession();
  await cdp.send('Animation.enable');
  await cdp.send('Animation.setPlaybackRate', { playbackRate: 0 });
  await page.setViewport({ width: 1024, height: 768 });
  await page.setRequestInterception(true);
  page.on('request', (r) => (r.url() === 'about:blank' || r.url().startsWith('data:') ? r.continue() : r.abort()));
}
await newPage();

// Computed values for every rendered element, in one fixed property order.
async function computed(html) {
  await page.setContent(html, { waitUntil: 'domcontentloaded', timeout: TIMEOUT_MS });
  return page.evaluate((skip) => {
    const props = Array.from(getComputedStyle(document.documentElement));
    const els = [];
    for (const el of document.querySelectorAll('*')) {
      if (skip.includes(el.localName)) continue;
      const cs = getComputedStyle(el);
      els.push([el.localName, props.map((p) => cs.getPropertyValue(p))]);
    }
    return { props, els };
  }, SKIP);
}

function compare(a, b) {
  if (a.els.length !== b.els.length) return [`structure: ${a.els.length} elements vs ${b.els.length}`];
  const diffs = [];
  for (let i = 0; i < a.els.length && diffs.length < MAX_DIFFS; i++) {
    const [tag, x] = a.els[i];
    const [tagB, y] = b.els[i];
    if (tag !== tagB) return [`structure: element ${i} is <${tag}> vs <${tagB}>`];
    for (let k = 0; k < x.length && diffs.length < MAX_DIFFS; k++) {
      if (x[k] !== y[k] && !ignored(a.props[k])) diffs.push(`<${tag}> #${i} ${a.props[k]}: ${x[k]} → ${y[k]}`);
    }
  }
  return diffs;
}

const withTimeout = (p) =>
  Promise.race([p, new Promise((_, reject) => setTimeout(() => reject(new Error('timed out')), TIMEOUT_MS))]);

// A fresh page, or a fresh browser if the old one has died and cannot open
// one: a crashed Chrome otherwise leaves every later call waiting forever.
async function recover() {
  try {
    await withTimeout(newPage());
  } catch {
    browser.process()?.kill('SIGKILL');
    browser = await launch();
    page = undefined;
    await newPage();
  }
}

let n = 0;
for await (const line of readline.createInterface({ input: process.stdin })) {
  if (!line) continue;
  const { name, original, inlined } = JSON.parse(line);
  let diffs;
  try {
    diffs = await withTimeout((async () => compare(await computed(original), await computed(inlined)))());
  } catch (err) {
    diffs = [`error: ${err.message}`];
    await recover();
  }
  process.stdout.write(JSON.stringify({ name, diffs }) + '\n');
  if (++n % 100 === 0) process.stderr.write(`cascade.mjs: ${n} documents\n`);
}
await browser.close();
