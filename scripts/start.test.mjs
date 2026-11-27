import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

function run(version) {
  const root = mkdtempSync(join(tmpdir(), 'start-'));
  try {
    mkdirSync(join(root, 'scripts'));
    copyFileSync(join(here, 'start.sh'), join(root, 'scripts', 'start.sh'));
    writeFileSync(join(root, 'VERSION'), version);
    return spawnSync('bash', [join(root, 'scripts', 'start.sh')], {
      encoding: 'utf8',
      env: { PATH: '/usr/bin:/bin' },
      timeout: 5000,
    });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

test('start.sh refuses a VERSION that is not MAJOR.MINOR.PATCH', () => {
  for (const v of ['', '1.2', 'v1.2.3', '1.2.3-rc.1', '1.2.3 -X main.x=y', '1.2.3"; id #', '1.2.3\n4.5.6\n', '$(id)']) {
    const r = run(v);
    assert.equal(r.status, 1, JSON.stringify(v));
    assert.match(r.stderr, /VERSION must look like 1\.2\.3/, JSON.stringify(v));
  }
});

test('start.sh accepts a plain VERSION and gets past the version check', () => {
  for (const v of ['2.4.1', '2.4.1\n', '10.20.30\r\n']) {
    const r = run(v);
    assert.doesNotMatch(r.stderr, /VERSION must look like/, JSON.stringify(v));
  }
});

test('start.cmd validates every line of VERSION before using it', () => {
  const cmd = readFileSync(join(here, 'start.cmd'), 'utf8');
  assert.match(cmd, /findstr \/v \/r \/x \/c:"\[0-9\]\[0-9\]\*\\\.\[0-9\]\[0-9\]\*\\\.\[0-9\]\[0-9\]\*" VERSION/);
  assert.ok(cmd.indexOf('findstr /v') < cmd.indexOf('where go'));
  assert.equal(cmd.match(/set \/p VER=<VERSION/g).length, 1);
});
