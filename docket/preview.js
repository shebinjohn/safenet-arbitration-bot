// Usage: node docket/preview.js <dir> <shotdir>  — serves <dir> must already be on :8765
const { chromium } = require('playwright');
(async () => {
  const shots = process.argv[2] || '.';
  const browser = await chromium.launch();
  let fail = 0;
  for (const width of [1200, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } });
    const errors = [];
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    page.on('pageerror', e => errors.push(String(e)));
    await page.goto('http://localhost:8765/index.html', { waitUntil: 'networkidle' });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    await page.screenshot({ path: `${shots}/docket-${width}.png`, fullPage: false });
    const ids = await page.evaluate(() => Array.from(document.querySelectorAll('#docket tbody a[href^="#r-"]')).map(a => a.getAttribute('href')));
    const bad = [];
    for (const id of ids) {
      await page.evaluate(h => { location.hash = h; }, id);
      await page.waitForFunction(() => !document.querySelector('#report-body .status'), null, { timeout: 15000 }).catch(() => {});
      const ok = await page.evaluate(() => !!document.querySelector('#report-body h1, #report-body h2'));
      const ov = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      if (!ok || ov > 0) bad.push(`${id} rendered=${ok} overflow=${ov}`);
    }
    await page.evaluate(() => { location.hash = '#r-' + Object.keys({}).concat(document.querySelector('#docket tbody a').getAttribute('href').slice(3))[0]; });
    await page.waitForTimeout(500);
    await page.screenshot({ path: `${shots}/report-${width}.png`, fullPage: false });
    console.log(`${width}px: overflow=${overflow}px, console errors=${errors.length}, reports=${ids.length}, bad=${JSON.stringify(bad)}`);
    errors.forEach(e => console.log('  ' + e));
    if (overflow > 0 || errors.length || bad.length) fail = 1;
  }
  await browser.close();
  process.exit(fail);
})();
