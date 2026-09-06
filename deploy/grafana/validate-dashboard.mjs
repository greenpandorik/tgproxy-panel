#!/usr/bin/env node
// Validates deploy/grafana/tgwp-panel.json:
//  1. every panel object has a non-null "datasource"
//  2. no "${DS_...}" input reference other than "${DS_PROMETHEUS}" appears anywhere in the file
//
// Usage: node deploy/grafana/validate-dashboard.mjs [path/to/dashboard.json]

import { readFileSync } from 'node:fs';

const path = process.argv[2] ?? new URL('./tgwp-panel.json', import.meta.url).pathname;
const raw = readFileSync(path, 'utf8');
const dashboard = JSON.parse(raw);

const errors = [];

// Rule 1: every panel must declare a datasource.
const panels = dashboard.panels ?? [];
if (panels.length === 0) {
  errors.push('dashboard has no panels');
}
for (const panel of panels) {
  const title = panel.title ?? `(id ${panel.id})`;
  if (panel.datasource == null) {
    errors.push(`panel "${title}" has no datasource`);
  } else if (panel.datasource.uid !== '${DS_PROMETHEUS}') {
    errors.push(`panel "${title}" datasource.uid is "${panel.datasource.uid}", expected "\${DS_PROMETHEUS}"`);
  }
  for (const target of panel.targets ?? []) {
    if (target.datasource == null) {
      errors.push(`panel "${title}" target ${target.refId} has no datasource`);
    } else if (target.datasource.uid !== '${DS_PROMETHEUS}') {
      errors.push(`panel "${title}" target ${target.refId} datasource.uid is "${target.datasource.uid}", expected "\${DS_PROMETHEUS}"`);
    }
  }
}

// Rule 2: no other ${DS_*} input reference anywhere in the raw JSON text.
const dsRefs = raw.match(/\$\{DS_[A-Za-z0-9_]*\}/g) ?? [];
for (const ref of dsRefs) {
  if (ref !== '${DS_PROMETHEUS}') {
    errors.push(`unexpected datasource input reference: ${ref}`);
  }
}
if (dsRefs.length === 0) {
  errors.push('no ${DS_PROMETHEUS} references found at all');
}

// __inputs must declare DS_PROMETHEUS as a datasource input.
const inputs = dashboard.__inputs ?? [];
const dsInput = inputs.find((i) => i.name === 'DS_PROMETHEUS');
if (!dsInput) {
  errors.push('__inputs is missing a DS_PROMETHEUS entry');
} else if (dsInput.type !== 'datasource') {
  errors.push(`__inputs.DS_PROMETHEUS.type is "${dsInput.type}", expected "datasource"`);
}

if (errors.length > 0) {
  console.error(`FAIL: ${path}`);
  for (const e of errors) console.error(`  - ${e}`);
  process.exit(1);
}

console.log(`OK: ${path} — ${panels.length} panels, all datasource refs use \${DS_PROMETHEUS}`);
