#!/usr/bin/env node
// Fails the build if a dangerous HTML/JS-injection sink is introduced into the
// client source. The client deliberately renders via <template> clones and
// textContent (no innerHTML), which is what keeps it XSS-free even though it has
// no framework auto-escaping. This guard preserves that invariant: tokens live
// in localStorage, so a single injected script would exfiltrate the refresh
// token. Keep the client sink-free.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('../js', import.meta.url));
const SINKS = [
  /\.innerHTML\s*=/,
  /\.outerHTML\s*=/,
  /insertAdjacentHTML\s*\(/,
  /document\.write\s*\(/,
  /\beval\s*\(/,
  /new\s+Function\s*\(/,
];

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (extname(p) === '.js') out.push(p);
  }
  return out;
}

let failures = 0;
for (const file of walk(ROOT)) {
  const lines = readFileSync(file, 'utf8').split('\n');
  lines.forEach((line, i) => {
    for (const re of SINKS) {
      if (re.test(line)) {
        console.error(`XSS sink guard: ${file}:${i + 1}: ${line.trim()}`);
        failures++;
      }
    }
  });
}

if (failures > 0) {
  console.error(`\n${failures} dangerous sink(s) found. Render via textContent / <template> clones instead.`);
  process.exit(1);
}
console.log('XSS sink guard: clean (no innerHTML/eval sinks in js/).');
