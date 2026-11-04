'use strict';
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const root = path.resolve(__dirname, '..');
const read = file => fs.readFileSync(path.join(root, file), 'utf8');

test('current documentation has working relative file and image links', () => {
  for (const file of ['README.md', 'docs/CODEX.md', 'docs/SCREENSHOTS.md']) {
    const text = read(file);
    for (const match of text.matchAll(/!?\[[^\]]*\]\(([^\s)]+)\)/g)) {
      const target = match[1];
      if (/^(?:https?:|#)/.test(target)) continue;
      const filename = target.split('#')[0];
      assert.ok(fs.existsSync(path.resolve(root, path.dirname(file), filename)), file + ' -> ' + target);
    }
  }
});

test('all README screenshots have recorded dimensions and matching hashes', () => {
  const manifest = JSON.parse(read('docs/screenshots/manifest.json'));
  assert.equal(manifest.schema_version, 1);
  assert.match(manifest.fixture_kind, /fictional/);
  assert.match(manifest.backend, /real Go app/);
  assert.equal(manifest.timezone, 'Europe/Copenhagen');
  assert.ok(Object.keys(manifest.images).length >= 9);
  const images = [...read('README.md').matchAll(/!\[[^\]]*\]\((docs\/screenshots\/[^)]+\.webp)\)/g)];
  assert.equal(images.length, 9);
  for (const [, filename] of images) {
    const item = manifest.images[path.basename(filename)];
    assert.ok(item, filename);
    const bytes = fs.readFileSync(path.join(root, filename));
    assert.equal(bytes.subarray(0, 4).toString(), 'RIFF');
    assert.equal(bytes.subarray(8, 12).toString(), 'WEBP');
    assert.ok(item.width >= 300 && item.height >= 100, filename);
    assert.equal(crypto.createHash('sha256').update(bytes).digest('hex'), item.sha256, filename);
  }
  for (const filename of ['web/index.html', 'web/app.js', 'web/style.css', 'main.go', 'planner.go']) {
    assert.match(manifest.source_hashes[filename], /^[a-f0-9]{64}$/);
  }
});

test('Codex handoff preserves the existing repository and safety boundaries', () => {
  const agents = read('AGENTS.md');
  const codex = read('docs/CODEX.md');
  assert.match(agents, /kw-aficionado\/familiehub/);
  assert.match(agents, /iPadOS 15/);
  assert.match(agents, /scripts\/check.sh/);
  assert.match(codex, /samme GitHub-repository/);
  assert.match(codex, /registrerer \*\*ikke i sig selv\*\*/);
  for (const file of ['scripts/setup-dev.sh', 'scripts/check.sh', 'scripts/capture-screenshots.py', 'scripts/screenshots-requirements.txt']) {
    assert.ok(fs.existsSync(path.join(root, file)), file);
  }
  assert.match(read('.gitignore'), /\/data-dev\//);
  assert.match(read('.gitignore'), /session\.key/);
});
