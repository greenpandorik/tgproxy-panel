// Fails if ru.json and en.json do not contain exactly the same set of leaf keys.
// A missing key silently renders the raw key path in the UI, which no type check catches.
// Run with `npm run i18n:check`.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const load = (name) => JSON.parse(readFileSync(join(here, '..', 'src', 'i18n', name), 'utf8'));

function leafKeys(obj, prefix = '') {
  const out = new Set();
  for (const [k, v] of Object.entries(obj)) {
    const key = `${prefix}${k}`;
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      for (const nested of leafKeys(v, `${key}.`)) out.add(nested);
    } else {
      out.add(key);
    }
  }
  return out;
}

const ru = leafKeys(load('ru.json'));
const en = leafKeys(load('en.json'));
const missingInEn = [...ru].filter((k) => !en.has(k)).sort();
const missingInRu = [...en].filter((k) => !ru.has(k)).sort();

console.log(`ru: ${ru.size} keys, en: ${en.size} keys`);
if (missingInEn.length > 0) console.error(`missing in en.json:\n  ${missingInEn.join('\n  ')}`);
if (missingInRu.length > 0) console.error(`missing in ru.json:\n  ${missingInRu.join('\n  ')}`);
if (missingInEn.length > 0 || missingInRu.length > 0) process.exit(1);
console.log('i18n parity OK');
