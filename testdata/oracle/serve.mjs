// Inlines one {"html","options"} JSON object per input line with juice and
// answers with one {"out"} or {"err"} line, so a Go test can query juice many
// times without paying Node's startup for each.
import readline from 'node:readline';
import juice from 'juice';

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
