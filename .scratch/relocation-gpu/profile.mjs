import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const fixtureDir = dirname(fileURLToPath(import.meta.url));
const repo = resolve(fixtureDir, '../..');
const outputDir = join(repo, '.tmp/relocation-gpu');
const mode = process.argv[2] || 'regression';
if (!['regression', 'minimal', 'isolation', 'probes', 'shared-probes', 'shared', 'shared-probes-2', 'shared-tiles', 'layout'].includes(mode)) throw new Error('Unknown profile mode');
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
    args: ['--window-size=2560,1300', '--disable-features=CalculateNativeWinOcclusion', '--disable-backgrounding-occluded-windows', '--disable-renderer-backgrounding', '--disable-background-timer-throttling'],
  });
  const page = context.pages()[0];
  await page.goto('http://127.0.0.1:18084/gpu-repro.local.html' + (mode === 'minimal' ? '?cards=0' : mode.startsWith('shared') || mode === 'layout' ? '?dialog=shared' : ''));
  await page.bringToFront();
  await page.getByRole('button', { name: 'Open fixture modal', exact: true }).waitFor();
  if (mode === 'layout') {
    const layouts = [];
    for (const viewport of [{ width: 390, height: 844 }, { width: 1280, height: 720 }, { width: 2560, height: 1215 }]) {
      await page.setViewportSize(viewport);
      await page.getByRole('button', { name: 'Open fixture modal', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Shared modal probe', exact: true });
      await dialog.waitFor();
      const geometry = await dialog.evaluate(el => {
        const rect = el.getBoundingClientRect();
        const host = el.parentElement.parentElement;
        const blurArea = [...host.querySelectorAll('*')].filter(e => getComputedStyle(e).backdropFilter === 'blur(4px)').reduce((area, e) => area + e.getBoundingClientRect().width * e.getBoundingClientRect().height, 0);
        return { left: rect.left, right: rect.right, viewportWidth: innerWidth, coverage: blurArea / (innerWidth * innerHeight) };
      });
      if (geometry.left < 0 || geometry.right > geometry.viewportWidth || Math.abs(geometry.coverage - 1) > 0.01) throw new Error('Invalid modal/blur viewport geometry');
      await dialog.getByRole('button', { name: 'Open nested modal' }).click();
      if (await page.getByRole('dialog').count() !== 2) throw new Error('Nested modal missing');
      await page.keyboard.press('Escape');
      if (await page.getByRole('dialog').count() !== 1 || await page.evaluate(() => document.body.style.overflow) !== 'hidden') throw new Error('Nested Escape/scroll lock regression');
      await page.keyboard.press('Escape');
      if (await page.getByRole('dialog').count() !== 0 || await page.evaluate(() => document.body.style.overflow) === 'hidden') throw new Error('Final modal cleanup regression');
      layouts.push({ ...viewport, ...geometry });
    }
    writeFileSync(join(outputDir, 'layout.json'), JSON.stringify(layouts, null, 2));
    console.log(JSON.stringify({ verdict: 'PASS', layouts }));
  } else {
  const browserPID = execFileSync('powershell.exe', ['-NoProfile', '-Command', "(Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'chrome.exe' -and ($_.CommandLine -replace '\\\\','/').Contains($env:RELOCATION_GPU_PROFILE) -and $_.CommandLine -notmatch '--type=' } | Select-Object -First 1 -ExpandProperty ProcessId)"], {
    encoding: 'utf8', env: { ...process.env, RELOCATION_GPU_PROFILE: profileDir },
  }).trim();
  if (!browserPID) throw new Error('Benchmark browser process not found');
  const results = [];
  const sample = async (label) => {
    await page.bringToFront();
    await page.waitForTimeout(1500);
    const output = execFileSync('powershell.exe', ['-NoProfile', '-File', join(fixtureDir, 'measure-gpu.ps1'), '-Label', mode + '-' + label, '-Seconds', '8', '-BrowserProcessId', browserPID, '-OutputPath', join(outputDir, 'measurements.jsonl')], { encoding: 'utf8' }).trim();
    const result = JSON.parse(output);
    results.push(result);
    console.log(JSON.stringify(result));
    await page.screenshot({ path: join(outputDir, mode + '-' + label + '.png') });
    writeFileSync(join(outputDir, mode + '.json'), JSON.stringify(results, null, 2));
  };
  await sample('closed');
  await page.getByRole('button', { name: 'Open fixture modal', exact: true }).click();
  await page.getByRole('dialog').waitFor();
  await sample('open-original');
  if (mode === 'shared') {
    const appearance = await page.evaluate(() => {
      const overlay = document.querySelector('[role="dialog"]').parentElement.parentElement;
      const filtered = [...overlay.querySelectorAll('*')].filter(el => getComputedStyle(el).backdropFilter.includes('blur('));
      return {
        filters: filtered.map(el => ({ filter: getComputedStyle(el).backdropFilter, width: el.getBoundingClientRect().width, height: el.getBoundingClientRect().height })),
        viewportArea: innerWidth * innerHeight,
        foregroundAnimation: getComputedStyle(document.querySelector('[data-testid="modal-spinner"]')).animationPlayState,
        backgroundAnimation: getComputedStyle(document.querySelector('#root .animate-spin')).animationPlayState,
      };
    });
    writeFileSync(join(outputDir, 'shared-appearance.json'), JSON.stringify(appearance, null, 2));
    if (!appearance.filters.length || appearance.filters.some(f => f.filter !== 'blur(4px)') || appearance.filters.reduce((n, f) => n + f.width * f.height, 0) < appearance.viewportArea * 0.99) throw new Error('Visible full-viewport 4px blur must remain enabled');
    if (appearance.foregroundAnimation !== 'running' || appearance.backgroundAnimation !== 'running') throw new Error('Foreground and background animation must keep running');
  }
  if (mode === 'shared') {
    await page.getByRole('combobox', { name: 'Probe' }).selectOption('single-blur');
    await sample('single-blur-positive-control');
    await page.getByRole('combobox', { name: 'Probe' }).selectOption('original');
    await sample('tiled-repeat');
    if (results[2].mean - results[1].mean < 10) throw new Error('Inconclusive GPU control: single full-viewport blur must add >=10 points');
  } else if (mode === 'shared-tiles') {
    for (const probe of ['tiles-16', 'tiles-50', 'tiles-64', 'tiles-128']) {
      await page.getByRole('combobox', { name: 'Probe' }).selectOption(probe);
      await sample('open-' + probe);
    }
  } else if (mode === 'shared-probes-2') {
    for (const probe of ['root-static', 'scale-2', 'scale-4', 'steps-12', 'tiles']) {
      await page.getByRole('combobox', { name: 'Probe' }).selectOption(probe);
      await sample('open-' + probe);
    }
  } else if (mode === 'shared-probes') {
    for (const probe of ['pause-background', 'original', 'layer', 'original', 'root-filter']) {
      await page.getByRole('combobox', { name: 'Probe' }).selectOption(probe);
      await sample('open-' + probe);
    }
  } else if (mode === 'isolation') {
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
    if (mode === 'regression') {
      await page.getByRole('combobox', { name: 'Probe' }).selectOption('single-blur');
      await sample('single-blur-positive-control');
      await page.getByRole('combobox', { name: 'Probe' }).selectOption('original');
      await sample('tiled-final');
      if (results[4].mean - results[1].mean < 10) throw new Error('Inconclusive GPU control for relocation');
    }
  }
  await page.screenshot({ path: join(outputDir, mode + '-open.png') });
  const delta = results[1].mean - results[0].mean;
  console.log(JSON.stringify({ verdict: delta >= 10 ? 'RED' : 'GREEN', closed: results[0].mean, opened: results[1].mean, delta }));
  if (mode === 'regression' && (delta >= 10 || results[3].mean - results[2].mean >= 10)) throw new Error('GPU regression: opening dialog adds >=10 percentage points');
  if (mode === 'shared' && (delta >= 10 || results[3].mean - results[0].mean >= 10)) throw new Error('GPU regression: shared blurred dialog adds >=10 percentage points');
  }
} finally {
  await context?.close();
  for (const path of created) unlinkSync(path);
}
