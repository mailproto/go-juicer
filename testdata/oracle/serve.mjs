// Inlines one {"html","options"} JSON object per input line with juice and
// answers with one {"out"} or {"err"} line, so a Go test can query juice many
// times without paying Node's startup for each.
//
//   node serve.mjs [juice-9]
import readline from 'node:readline';

const { default: juice } = await import(process.argv[2] === 'juice-9' ? 'juice-9' : 'juice');

for await (const line of readline.createInterface({ input: process.stdin })) {
  const { html, options } = JSON.parse(line);
  let res;
  try {
    res = { out: juice(html, options ?? {}) };
  } catch (err) {
    res = { err: `${err.name}: ${err.message}` };
  }
  process.stdout.write(JSON.stringify(res) + '\n');
}
