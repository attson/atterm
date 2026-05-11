#!/usr/bin/env node
import { readFileSync, writeFileSync } from 'node:fs';

const [, , file, version] = process.argv;
if (!file || !version) {
  console.error('usage: set-wails-version.mjs <wails.json> <version-without-v>');
  process.exit(1);
}

if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version)) {
  console.error(`invalid product version: ${version}`);
  process.exit(1);
}

const data = JSON.parse(readFileSync(file, 'utf8'));
data.info ??= {};
data.info.productVersion = version;
writeFileSync(file, `${JSON.stringify(data, null, 2)}\n`);
