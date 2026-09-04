#!/usr/bin/env node
// Static checks for the embedded UI frontend. Catches the class of bugs that
// Go tests can't: JS syntax errors, DOM id references that don't exist in
// index.html, local variables shadowing a global function that is then called
// (e.g. relTime's `const t = new Date(...)` shadowing the i18n helper `t()`),
// and i18n keys referenced via t('...') that are missing from the dictionaries.
//
// Run: node scripts/check-ui.mjs   (also wired into `make test` and CI)

import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const JS = join(root, 'internal/ui/static/app.js');
const HTML = join(root, 'internal/ui/static/index.html');

let failures = 0;
const fail = (msg) => { failures++; console.error(`  ✗ ${msg}`); };
const ok = (msg) => console.log(`  ✓ ${msg}`);

// ---- 1. syntax ----
console.log('check-ui: syntax');
try {
  execFileSync(process.execPath, ['--check', JS], { stdio: 'pipe' });
  ok('app.js parses');
} catch (e) {
  fail(`app.js syntax: ${e.stderr}`);
}

const js = readFileSync(JS, 'utf8');
const html = readFileSync(HTML, 'utf8');

// findBlockEnd returns the index of the matching closing brace, skipping
// braces inside string literals (i18n values contain '{n}' placeholders).
function findBlockEnd(s, open) {
  let depth = 0, inStr = null;
  for (let i = open; i < s.length; i++) {
    const c = s[i];
    if (inStr) {
      if (c === '\\') { i++; continue; }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === "'" || c === '"') { inStr = c; continue; }
    if (c === '{') depth++;
    else if (c === '}') { depth--; if (depth === 0) return i; }
  }
  return -1;
}

// topLevelCode blanks out everything nested inside braces (string-aware), so
// the result contains only top-level declarations for name collection.
function topLevelCode(js) {
  let depth = 0, inStr = null, out = '';
  for (let i = 0; i < js.length; i++) {
    const c = js[i];
    if (inStr) {
      if (c === '\\') { i++; out += ' '; continue; }
      if (c === inStr) inStr = null;
      out += ' ';
      continue;
    }
    if (c === "'" || c === '"') { inStr = c; out += ' '; continue; }
    if (c === '{') { depth++; out += ' '; continue; }
    if (c === '}') { depth--; out += ' '; continue; }
    out += depth === 0 ? c : ' ';
  }
  return out;
}

// ---- 2. DOM ids referenced in JS exist in index.html ----
console.log('check-ui: DOM id references');
{
  const refs = new Set();
  for (const m of js.matchAll(/\$\('([^']+)'\)/g)) refs.add(m[1]);
  for (const m of js.matchAll(/getElementById\('([^']+)'\)/g)) refs.add(m[1]);
  for (const m of js.matchAll(/querySelector\('#([A-Za-z0-9_-]+)'\)/g)) refs.add(m[1]);
  const ids = new Set(html.matchAll(/id="([^"]+)"/g) ? [...html.matchAll(/id="([^"]+)"/g)].map((m) => m[1]) : []);
  const missing = [...refs].filter((r) => !ids.has(r));
  if (missing.length) missing.forEach((m) => fail(`JS references missing id: #${m}`));
  else ok(`${refs.size} JS-referenced ids all present in index.html`);
}

// ---- 3. local variables shadowing a global callable that is then called ----
console.log('check-ui: variable shadowing');
{
  // global (top-level) names: function declarations and const/let/var at depth 0
  const globals = new Map(); // name -> kind
  {
    const top = topLevelCode(js);
    for (const m of top.matchAll(/\bfunction\s+([A-Za-z_$][\w$]*)\s*\(/g)) globals.set(m[1], 'function');
    for (const m of top.matchAll(/\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=/g)) {
      if (!globals.has(m[1])) globals.set(m[1], 'const');
    }
  }

  // find every function body; for each, check local declarations vs globals
  const fnRe = /\bfunction\s+([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{/g;
  let m;
  while ((m = fnRe.exec(js))) {
    const name = m[1];
    const open = m.index + m[0].lastIndexOf('{');
    // match braces to find body end
    let depth = 0, end = -1;
    for (let i = open; i < js.length; i++) {
      if (js[i] === '{') depth++;
      else if (js[i] === '}') { depth--; if (depth === 0) { end = i; break; } }
    }
    if (end < 0) continue;
    const body = js.slice(open + 1, end);

    // local declarations: const/let/var NAME and nested function NAME, with their offset
    const locals = [];
    for (const d of body.matchAll(/\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=/g)) locals.push({ n: d[1], at: d.index });
    for (const d of body.matchAll(/\bfunction\s+([A-Za-z_$][\w$]*)\s*\(/g)) locals.push({ n: d[1], at: d.index });

    for (const { n, at } of locals) {
      if (!globals.has(n)) continue; // no global to shadow
      const after = body.slice(at);
      // a call of the form `n(...)` that is not `new n(`, `.n(`, `obj[n](` or an assignment
      const callRe = new RegExp(`(?<![.\\w$"'])\\b${n}\\s*\\(`, 'g');
      let hit = callRe.exec(after);
      if (hit) {
        // skip `new n(` and `foo.n(`
        if (/new\s+/.test(after.slice(0, hit.index)) && after.slice(hit.index).startsWith(`${n}(`)) {
          // check not preceded by `new `
          const before = after.slice(0, hit.index);
          if (/new\s+$/.test(before)) { hit = callRe.exec(after); continue; }
        }
        if (hit) {
          fail(`${name}(): local '${n}' shadows global ${globals.get(n)} and is called afterwards — rename the local`);
        }
      }
    }
  }
  ok('no shadowing-call pattern found');
}

// ---- 4. i18n keys referenced via t('...') exist in both dictionaries ----
console.log('check-ui: i18n keys');
{
  const dicts = [];
  const i18nStart = js.indexOf('const I18N = {');
  if (i18nStart < 0) {
    fail('could not locate `const I18N = {`');
  } else {
    // Line-based parsing of the flat I18N dictionary: `en: {` / `zh: {` blocks
    // end at a line whose only content is `},` / `}`. Values may contain
    // '{n}' placeholders, so brace counting over the whole file is unreliable.
    const i18nLines = js.slice(i18nStart).split('\n');
    const blockOf = (lang) => {
      const open = i18nLines.findIndex((l) => new RegExp(`^\\s*${lang}:\\s*\\{\\s*$`).test(l));
      if (open < 0) return null;
      const keys = new Set();
      for (let i = open + 1; i < i18nLines.length; i++) {
        const ln = i18nLines[i].trim();
        if (ln.startsWith('}')) break;
        for (const k of ln.matchAll(/'([^']+)'\s*:/g)) keys.add(k[1]);
      }
      return keys;
    };
    const enKeys = blockOf('en');
    const zhKeys = blockOf('zh');
    if (enKeys) dicts.push({ name: 'en', keys: enKeys });
    if (zhKeys) dicts.push({ name: 'zh', keys: zhKeys });
    if (!enKeys) fail('I18N.en block not found');
    if (!zhKeys) fail('I18N.zh block not found');
  }
  const en = dicts.find((d) => d.name === 'en');
  const zh = dicts.find((d) => d.name === 'zh');
  if (!en || !zh) {
    fail(`could not locate I18N dictionaries (found: ${dicts.map((d) => d.name).join(', ') || 'none'})`);
  } else {
    const refs = new Set();
    for (const r of js.matchAll(/\bt\('([^']+)'/g)) refs.add(r[1]);
    for (const r of js.matchAll(/\bt\("([^"]+)"/g)) refs.add(r[1]);
    const missingEn = [...refs].filter((k) => !en.keys.has(k));
    const missingZh = [...refs].filter((k) => !zh.keys.has(k));
    if (missingEn.length) missingEn.forEach((k) => fail(`t('${k}') missing from I18N.en`));
    if (missingZh.length) missingZh.forEach((k) => fail(`t('${k}') missing from I18N.zh`));
    if (!missingEn.length && !missingZh.length) ok(`${refs.size} t() keys present in both I18N.en and I18N.zh`);
    const onlyEn = [...en.keys].filter((k) => !zh.keys.has(k));
    const onlyZh = [...zh.keys].filter((k) => !en.keys.has(k));
    if (onlyEn.length) fail(`key only in I18N.en: ${onlyEn.join(', ')}`);
    if (onlyZh.length) fail(`key only in I18N.zh: ${onlyZh.join(', ')}`);
    if (!onlyEn.length && !onlyZh.length) ok(`I18N.en / I18N.zh key sets identical (${en.keys.size})`);
  }
}

console.log(failures ? `\ncheck-ui: ${failures} problem(s)` : '\ncheck-ui: all checks passed');
process.exit(failures ? 1 : 0);
