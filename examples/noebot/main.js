// Thin consumer only. Install the private noebot artifact separately.
import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';

if (process.argv.length > 3) {
  console.error('Usage: noe run --no-inst main.js [config.json]');
  process.exit(2);
}
const home = process.env.NOEBOT_HOME;
if (typeof home !== 'string' || !home.trim()) {
  console.error('NOEBOT_HOME_REQUIRED: Set NOEBOT_HOME to an absolute extracted noebot installation root.');
  process.exit(1);
}
if (!path.isAbsolute(home)) {
  console.error('NOEBOT_HOME_ABSOLUTE: NOEBOT_HOME must be absolute; no checkout fallback is used.');
  process.exit(1);
}
let main;
try {
  const root = fs.realpathSync(home);
  if (!fs.statSync(root).isDirectory()) throw new Error();
  const entry = fs.realpathSync(path.join(root,'app/main.mjs'));
  if (!entry.startsWith(root + path.sep) || !fs.statSync(entry).isFile()) throw new Error();
  ({main} = await import(pathToFileURL(entry).href));
  if (typeof main !== 'function') throw new Error();
} catch (_) {
  // Do not expose installation paths, import stacks or the invoking workspace.
  console.error('NOEBOT_INSTALL_INVALID: NOEBOT_HOME must contain an extracted noebot installation with app/main.mjs.');
  process.exit(1);
}
try {
  process.exit(await main(process.argv[2],{env:process.env}));
} catch (error) {
  console.error((error.code || 'STARTUP_FAILED') + ': ' + error.message);
  process.exit(1);
}
