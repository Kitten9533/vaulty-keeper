#!/usr/bin/env node
// npm/scripts/build.mjs — assemble the npm package payload from `make release`
// output. Run after `make release`; reads the built archives from release/,
// extracts each platform binary into npm/platform/ and syncs the version in
// npm/package.json with internal/cli/cli.go. Does not publish anything.
//
// Usage: node npm/scripts/build.mjs
// Then:  (cd npm && npm pack && npm publish)

import { execFileSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '../..');
const release = join(root, 'release');
const pkgDir = join(root, 'npm');
const platformDir = join(pkgDir, 'platform');

const cliGo = readFileSync(join(root, 'internal/cli/cli.go'), 'utf8');
const m = /const Version = "([^"]+)"/.exec(cliGo);
if (!m) {
  console.error('build: cannot find Version in internal/cli/cli.go');
  process.exit(1);
}
const version = m[1];

const platforms = [
  { dir: 'darwin-x64', file: `vaulty-keeper-${version}-macos-x86_64.tar.gz`, member: 'vaulty-keeper', isZip: false },
  { dir: 'darwin-arm64', file: `vaulty-keeper-${version}-macos-arm64.tar.gz`, member: 'vaulty-keeper', isZip: false },
  { dir: 'linux-x64', file: `vaulty-keeper-${version}-linux-x86_64.tar.gz`, member: 'vaulty-keeper', isZip: false },
  { dir: 'linux-arm64', file: `vaulty-keeper-${version}-linux-arm64.tar.gz`, member: 'vaulty-keeper', isZip: false },
  { dir: 'win32-x64', file: `vaulty-keeper-${version}-windows-x86_64.zip`, member: 'vaulty-keeper.exe', isZip: true },
];

const missing = platforms.filter((p) => !existsSync(join(release, p.file)));
if (missing.length) {
  console.error('build: missing release archives (run `make release` first):');
  for (const p of missing) console.error(`  - ${p.file}`);
  process.exit(1);
}

rmSync(platformDir, { recursive: true, force: true });
mkdirSync(platformDir, { recursive: true });

for (const p of platforms) {
  const src = join(release, p.file);
  const dstDir = join(platformDir, p.dir);
  mkdirSync(dstDir, { recursive: true });
  if (p.isZip) {
    execFileSync('unzip', ['-o', '-j', src, p.member, '-d', dstDir], { stdio: 'inherit' });
  } else {
    execFileSync('tar', ['-xzf', src, '-C', dstDir, p.member], { stdio: 'inherit' });
  }
  const binPath = join(dstDir, p.member);
  chmodSync(binPath, 0o755);
}

const pkgJsonPath = join(pkgDir, 'package.json');
const pkg = JSON.parse(readFileSync(pkgJsonPath, 'utf8'));
pkg.version = version;
writeFileSync(pkgJsonPath, JSON.stringify(pkg, null, 2) + '\n');

console.log(`build: extracted ${platforms.length} platform binaries for v${version} into npm/platform/`);
console.log(`build: npm/package.json version set to ${version}`);
