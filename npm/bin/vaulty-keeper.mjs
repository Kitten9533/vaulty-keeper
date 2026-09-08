#!/usr/bin/env node
// Platform dispatcher: spawns the prebuilt binary matching this machine.
// Binaries are embedded in the package (no postinstall, no runtime download);
// npm fetches the tarball over HTTPS rather than a browser, so macOS Gatekeeper
// and Windows SmartScreen do not apply their downloaded-file markers and no
// "unidentified developer" / "Windows protected your PC" prompt appears.

import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);

const mapping = {
  'darwin-x64': 'platform/darwin-x64/vaulty-keeper',
  'darwin-arm64': 'platform/darwin-arm64/vaulty-keeper',
  'linux-x64': 'platform/linux-x64/vaulty-keeper',
  'linux-arm64': 'platform/linux-arm64/vaulty-keeper',
  'win32-x64': 'platform/win32-x64/vaulty-keeper.exe',
};

const key = `${process.platform}-${process.arch}`;
const rel = mapping[key];
if (!rel) {
  console.error(`vaulty-keeper: no prebuilt binary for ${process.platform}/${process.arch}`);
  process.exit(1);
}

let binPath;
try {
  binPath = require.resolve(`../${rel}`);
} catch (err) {
  console.error(`vaulty-keeper: prebuilt binary missing (${rel}); reinstall the package or use a GitHub release archive`);
  process.exit(1);
}

const child = spawn(binPath, process.argv.slice(2), { stdio: 'inherit' });
child.on('error', (err) => {
  console.error(`vaulty-keeper: failed to run ${binPath}: ${err.message}`);
  process.exit(1);
});
child.on('exit', (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  process.exit(code ?? 1);
});
