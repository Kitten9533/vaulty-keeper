#!/usr/bin/env node
// Static checks over the Markdown documentation tree (root READMEs, AGENTS.md
// and everything under docs/). Catches documentation regressions that Go tests
// can't: a doc that lost its bilingual sibling, a language-switch link pointing
// at the wrong file, an unclosed Markdown code fence, or a relative link to a
// file that no longer exists.
//
// Scope: README.md, README.zh-CN.md, AGENTS.md and docs/**/*.md. Files listed
// in SINGLE_ENTRY are intentionally single-language (agent/operating files);
// pairing and language-switch checks are skipped for them, fence and link
// checks still apply.
//
// Run: node scripts/check-docs.mjs   (also wired into `make test` and CI)

import { readdirSync, statSync, existsSync, readFileSync } from 'node:fs';
import { dirname, join, relative, basename } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');

// ---- scope ----
const scope = ['README.md', 'README.zh-CN.md', 'AGENTS.md'];
(function walk(dir) {
  for (const entry of readdirSync(dir).sort()) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p);
    else if (entry.endsWith('.md')) scope.push(relative(root, p));
  }
})(join(root, 'docs'));

// Intentional single-language files. AGENTS.md is agent operating constraints
// written in Chinese only; it is not a user-facing guide and has no sibling.
// Historical docs under docs/superpowers/ are all bilingual and need no entry.
const SINGLE_ENTRY = ['AGENTS.md'];

let failures = 0;
const fail = (msg) => { failures++; console.error(`  ✗ ${msg}`); };
const ok = (msg) => console.log(`  ✓ ${msg}`);

const siblingOf = (f) => (f.endsWith('.zh-CN.md') ? f.slice(0, -'.zh-CN.md'.length) + '.md' : f.slice(0, -'.md'.length) + '.zh-CN.md');
const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// ---- 1. bilingual pairing ----
console.log('check-docs: bilingual pairing');
{
  const have = new Set(scope);
  let paired = 0;
  for (const f of scope.sort()) {
    const sib = siblingOf(f);
    if (SINGLE_ENTRY.includes(f)) {
      ok(`${f} is a documented single-entry file (no sibling required)`);
      continue;
    }
    if (!have.has(sib)) {
      fail(`${f} has no sibling ${sib}`);
      continue;
    }
    if (f.endsWith('.md') && !f.endsWith('.zh-CN.md')) paired++;
  }
  if (paired) ok(`${paired} bilingual pair(s) complete`);
}

// ---- 2. language-switch links point at the sibling ----
console.log('check-docs: language-switch links');
{
  let checked = 0;
  for (const f of scope.sort()) {
    if (SINGLE_ENTRY.includes(f)) continue;
    const sib = siblingOf(f);
    const head = readFileSync(join(root, f), 'utf8').split('\n').slice(0, 10).join('\n');
    const re = new RegExp(`\\]\\(\\s*${escapeRe(basename(sib))}(?:#|\\))`);
    if (re.test(head)) {
      checked++;
    } else {
      fail(`${f} top block does not link its sibling ${sib}`);
    }
  }
  if (checked) ok(`${checked} paired file(s) link their sibling in the top block`);
}

// ---- 3. code fences: balanced AND matching delimiters ----
console.log('check-docs: code fences');
{
  let checked = 0;
  for (const f of scope.sort()) {
    const lines = readFileSync(join(root, f), 'utf8').split('\n');
    // CommonMark fence rules: an opening fence is 3+ backticks/tildes (with an
    // optional info string); a closing fence must use the same character with a
    // length >= the opening fence and nothing after it but whitespace. A shorter
    // or different-character fence line inside an open block is content. The
    // stack keeps {char,len} of open fences so a mismatched close length (e.g.
    // open with ````` ``` ```` and close with ```` ``` ````) is caught as an unclosed fence.
    const stack = [];
    for (let i = 0; i < lines.length; i++) {
      const m = /^( {0,3})(`{3,}|~{3,})/.exec(lines[i]);
      if (!m) continue;
      const marker = m[2];
      const char = marker[0];
      const len = marker.length;
      const rest = lines[i].slice(m[0].length).trim();
      if (stack.length) {
        const top = stack[stack.length - 1];
        if (top.char === char && len >= top.len && rest === '') {
          stack.pop();
        }
        // otherwise: content inside an open fence (shorter same-char, different
        // char, or a non-blank info line) — ignored, per CommonMark.
        continue;
      }
      stack.push({ char, len, line: i + 1 });
    }
    if (stack.length) {
      const opens = stack.map((s) => `${s.char.repeat(s.len)} at line ${s.line}`).join(', ');
      fail(`${f} has ${stack.length} unclosed fence(s): ${opens}`);
    } else {
      checked++;
    }
  }
  ok(`${checked} file(s) have balanced, matching code fences`);
}

// ---- 4. relative link targets exist ----
console.log('check-docs: relative link targets');
{
  let checked = 0;
  const linkRe = /\[[^\]]*\]\(([^)]+)\)/g;
  for (const f of scope.sort()) {
    const text = readFileSync(join(root, f), 'utf8');
    const dir = dirname(f);
    for (const m of text.matchAll(linkRe)) {
      const target = (m[1] || '').trim().split(/\s+/)[0];
      if (!target) continue;
      if (/^(https?:|mailto:|data:)/.test(target)) continue;
      if (/^#/.test(target)) continue;
      const path = target.split('#')[0];
      if (!path) continue;
      const resolved = join(root, dir, path);
      if (!existsSync(resolved)) {
        fail(`${f}: relative link target does not exist: ${target}`);
      } else {
        checked++;
      }
    }
  }
  ok(`${checked} relative link target(s) resolve`);
}

console.log(failures ? `\ncheck-docs: ${failures} problem(s)` : '\ncheck-docs: all checks passed');
process.exit(failures ? 1 : 0);
