// Generates the instrument marks used by the depth ladder in hero-lab.html.
//
// Each logo is drawn as vector geometry — the Ethereum octahedron's facets and
// seams, the Bitcoin B with its two prongs, Solana's three counter-slanted bars
// — and rasterised onto a braille dot lattice: 2x4 dots per character, eight
// times the resolution of one glyph per cell. The dot pitch is measured from
// the subset font first, so sampling matches the glyph the browser will draw.
//
//   node pitch/proto/gen-asset-marks.mjs        (needs the lab served on :8099)
//
// Prints the metrics and a JSON blob to paste into the ASSETS table.
// The ASCII (# : .) marks in that table came from an earlier single-glyph pass
// and are kept as the no-webfont fallback.

import { chromium } from '/opt/node22/lib/node_modules/playwright/index.mjs';
const b = await chromium.launch();
const page = await b.newPage();
await page.goto('http://127.0.0.1:8099/pitch/proto/hero-lab.html', { waitUntil: 'load' });
const out = await page.evaluate(async () => {
  const ff = new FontFace("Braille", "url(/pitch/proto/dejavu-braille.woff2)");
  await ff.load(); document.fonts.add(ff);

  // ── Measure the dot lattice of a full cell so sampling matches the glyph
  const F = 200;
  const cv = new OffscreenCanvas(F * 2, F * 3);
  const g = cv.getContext("2d");
  g.fillStyle = "#000"; g.font = F + "px Braille"; g.textBaseline = "alphabetic";
  g.fillText("⣿", 0, F * 2);
  const d = g.getImageData(0, 0, cv.width, cv.height).data;
  const colHit = [], rowHit = [];
  for (let x = 0; x < cv.width; x++) { let s = 0; for (let y = 0; y < cv.height; y++) s += d[(y * cv.width + x) * 4 + 3]; colHit.push(s); }
  for (let y = 0; y < cv.height; y++) { let s = 0; for (let x = 0; x < cv.width; x++) s += d[(y * cv.width + x) * 4 + 3]; rowHit.push(s); }
  const runs = (arr) => { const r = []; let st = -1; for (let i = 0; i < arr.length; i++) { if (arr[i] > 0 && st < 0) st = i; else if (arr[i] === 0 && st >= 0) { r.push([st, i - 1]); st = -1; } } if (st >= 0) r.push([st, arr.length - 1]); return r; };
  const cols = runs(colHit), rows = runs(rowHit);
  const mid = (r) => (r[0] + r[1]) / 2;
  const dotDX = mid(cols[1]) - mid(cols[0]);
  const dotDY = (mid(rows[3]) - mid(rows[0])) / 3;
  const originX = mid(cols[0]);                 // first dot centre from the text origin
  const originY = mid(rows[0]) - F * 2;         // relative to the alphabetic baseline
  const M = { dotDX: dotDX / F, dotDY: dotDY / F, originX: originX / F, originY: originY / F,
              cellW: (dotDX * 2) / F, cellH: (dotDY * 4) / F };

  // ── Rasterise a shape onto the dot lattice, 2x4 dots per character
  const BITS = [[0, 0, 0x01], [0, 1, 0x02], [0, 2, 0x04], [0, 3, 0x40],
                [1, 0, 0x08], [1, 1, 0x10], [1, 2, 0x20], [1, 3, 0x80]];
  function braille(vw, vh, rowsChars, draw, thresh = 0.42) {
    const DR = rowsChars * 4;                                   // dot rows
    const pxPerDotY = 8, pxPerDotX = Math.max(1, Math.round(pxPerDotY * (M.dotDX / M.dotDY)));
    const scale = (DR * pxPerDotY) / vh;
    const DC = Math.ceil((vw * scale) / pxPerDotX);
    const chars = Math.ceil(DC / 2);
    const c2 = new OffscreenCanvas(chars * 2 * pxPerDotX, DR * pxPerDotY);
    const gg = c2.getContext("2d");
    gg.save(); gg.scale(scale * (pxPerDotY / pxPerDotY), scale); gg.translate(0, 0);
    // x is scaled separately to respect the dot aspect
    gg.setTransform(scale * (pxPerDotY / pxPerDotX), 0, 0, scale, 0, 0);
    draw(gg); gg.restore();
    const im = gg.getImageData(0, 0, c2.width, c2.height).data;
    const cov = (dx, dy) => {
      let s = 0, n = 0;
      for (let y = dy * pxPerDotY; y < (dy + 1) * pxPerDotY; y++)
        for (let x = dx * pxPerDotX; x < (dx + 1) * pxPerDotX; x++) { s += im[(y * c2.width + x) * 4 + 3] / 255; n++; }
      return s / n;
    };
    const lines = [];
    for (let r = 0; r < rowsChars; r++) {
      let line = "";
      for (let c = 0; c < chars; c++) {
        let bits = 0;
        for (const [bx, by, bit] of BITS) if (cov(c * 2 + bx, r * 4 + by) >= thresh) bits |= bit;
        line += String.fromCharCode(0x2800 + bits);
      }
      lines.push(line.replace(/⠀+$/, ""));
    }
    return lines;
  }

  const res = { metrics: M, art: {} };
  res.art.eth = braille(256, 417, 9, (g) => {
    const p = (pts, a) => { g.globalAlpha = a; g.fillStyle = "#000"; g.beginPath(); g.moveTo(pts[0][0], pts[0][1]); pts.slice(1).forEach(q => g.lineTo(q[0], q[1])); g.closePath(); g.fill(); };
    p([[128, 0], [256, 212], [128, 288]], 1); p([[128, 0], [0, 212], [128, 288]], 1);
    p([[128, 312], [256, 236], [128, 417]], 1); p([[128, 312], [0, 236], [128, 417]], 1);
    // knock the facet seams back out so the octahedron shows its edges
    g.globalCompositeOperation = "destination-out"; g.globalAlpha = 1;
    g.lineWidth = 13; g.strokeStyle = "#000";
    g.beginPath(); g.moveTo(128, 0); g.lineTo(128, 288); g.moveTo(0, 212); g.lineTo(128, 288); g.lineTo(256, 212); g.stroke();
    g.beginPath(); g.moveTo(128, 312); g.lineTo(128, 417); g.moveTo(0, 236); g.lineTo(128, 312); g.lineTo(256, 236); g.stroke();
    g.globalCompositeOperation = "source-over";
  });
  res.art.btc = braille(46, 64, 9, (g) => {
    g.fillStyle = "#000"; g.font = "bold 52px 'DejaVu Sans', sans-serif";
    g.textAlign = "center"; g.textBaseline = "middle"; g.fillText("B", 23, 33);
    g.fillRect(8, 2, 6, 12); g.fillRect(20, 2, 6, 12);
    g.fillRect(8, 50, 6, 12); g.fillRect(20, 50, 6, 12);
  });
  res.art.sol = braille(100, 80, 6, (g) => {
    g.fillStyle = "#000";
    const bar = (p) => { g.beginPath(); g.moveTo(p[0][0], p[0][1]); p.slice(1).forEach(q => g.lineTo(q[0], q[1])); g.closePath(); g.fill(); };
    bar([[16, 2], [100, 2], [84, 22], [0, 22]]);
    bar([[0, 30], [84, 30], [100, 50], [16, 50]]);
    bar([[16, 58], [100, 58], [84, 78], [0, 78]]);
  });
  return res;
});
console.log("metrics", JSON.stringify(out.metrics));
for (const [k, v] of Object.entries(out.art)) { console.log("\n=== " + k + " ==="); v.forEach(l => console.log("|" + l + "|")); }
console.log("\n" + JSON.stringify(out.art));
await b.close();
