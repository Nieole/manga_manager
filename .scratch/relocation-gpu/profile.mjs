import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const fixtureDir = dirname(fileURLToPath(import.meta.url));
const repo = resolve(fixtureDir, '../..');
const outputDir = join(repo, '.tmp/relocation-gpu');
const mode = process.argv[2] || 'regression';
if (!['regression', 'minimal', 'isolation', 'probes'].includes(mode)) throw new Error('Unknown profile mode');
const { chromium } = await import(pathToFileURL(join(outputDir, 'runtime/node_modules/playwright-core/index.mjs')));
const files = [['repro.html', 'gpu-repro.local.html'], ['repro.tsx', 'gpu-repro.local.tsx']];
const created = [];
let context;
try {
  mkdirSync(outputDir, { recursive: true });
  for (const [source, destination] of files) {
    const sourcePath = join(fixtureDir, source);
    const destinationPath = join(repo, 'web', destination);
    if (existsSync(destinationPath)) {
      if (!readFileSync(sourcePath).equals(readFileSync(destinationPath))) throw new Error('Refusing to replace existing fixture: ' + destinationPath);
    } else {
      copyFileSync(sourcePath, destinationPath);
      created.push(destinationPath);
    }
  }
  const profileDir = join(outputDir, 'chrome-profile').replaceAll('\\', '/');
  context = await chromium.launchPersistentContext(profileDir, {
    executablePath: join(process.env.PROGRAMFILES || 'C:/Program Files', 'Google/Chrome/Application/chrome.exe'),
    headless: false,
    viewport: { width: 2560, height: 1215 },
    args: ['--window-size=2560,1300', '--disable-backgrounding-occluded-windows', '--disable-renderer-backgrounding', '--disable-background-timer-throttling'],
  });
  const page = context.pages()[0];
  await page.goto('http://127.0.0.1:18084/gpu-repro.local.html' + (mode === 'minimal' ? '?cards=0' : ''));
  await page.getByRole('button', { name: 'Open fixture modal', exact: true }).waitFor();
  const browserPID = execFileSync('powershell.exe', ['-NoProfile', '-Command', "(Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'chrome.exe' -and ($_.CommandLine -replace '\\\\','/').Contains($env:RELOCATION_GPU_PROFILE) -and $_.CommandLine -notmatch '--type=' } | Select-Object -First 1 -ExpandProperty ProcessId)"], {
    encoding: 'utf8', env: { ...process.env, RELOCATION_GPU_PROFILE: profileDir },
  }).trim();
  if (!browserPID) throw new Error('Benchmark browser process not found');
  const results = [];
  const sample = async (label) => {
    await page.waitForTimeout(1500);
    const output = execFileSync('powershell.exe', ['-NoProfile', '-File', join(fixtureDir, 'measure-gpu.ps1'), '-Label', mode + '-' + label, '-Seconds', '8', '-BrowserProcessId', browserPID, '-OutputPath', join(outputDir, 'measurements.jsonl')], { encoding: 'utf8' }).trim();
    const result = JSON.parse(output);
    results.push(result);
    console.log(JSON.stringify(result));
    writeFileSync(join(outputDir, mode + '.json'), JSON.stringify(results, null, 2));
  };
  await sample('closed');
  await page.getByRole('button', { name: 'Open fixture modal', exact: true }).click();
  await page.getByRole('dialog').waitFor();
  await sample('open-original');
  if (mode === 'isolation') {
    await page.getByRole('combobox', { name: 'Probe' }).selectOption('no-modal-blur');
    await sample('open-no-modal-blur');
    await page.getByRole('combobox', { name: 'Probe' }).selectOption('original');
    await sample('open-restored');
  } else if (mode === 'probes') {
    for (const probe of ['no-blur', 'original', 'no-animation', 'original', 'no-poll']) {
      await page.getByRole('combobox', { name: 'Probe' }).selectOption(probe);
      await sample('open-' + probe);
    }
  } else {
    await page.getByRole('button', { name: 'Close fixture modal', exact: true }).click();
    await sample('closed-repeat');
    await page.getByRole('button', { name: 'Open fixture modal', exact: true }).click();
    await sample('open-repeat');
  }
  await page.screenshot({ path: join(outputDir, mode + '-open.png') });
  const delta = results[1].mean - results[0].mean;
  console.log(JSON.stringify({ verdict: delta >= 10 ? 'RED' : 'GREEN', closed: results[0].mean, opened: results[1].mean, delta }));
  if (mode === 'regression' && (delta >= 10 || results[3].mean - results[2].mean >= 10)) throw new Error('GPU regression: opening relocation dialog adds >=10 percentage points');
} finally {
  await context?.close();
  for (const path of created) unlinkSync(path);
}
