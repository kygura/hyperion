// Regenerates the ASCII asset marks used by the depth ladder in hero-lab.html.
// Rasterises each logo's real geometry and samples coverage per character cell
// at a 0.6:1 cell ratio, mapping to the ladder's own glyphs (# : .).
//   node pitch/proto/gen-asset-marks.mjs
// Paste the printed JSON into the ASSETS table in hero-lab.html.
import { chromium } from '/opt/node22/lib/node_modules/playwright/index.mjs';
const b = await chromium.launch();
const page = await b.newPage();
const out = await page.evaluate(() => {
  const CH = 10, CW = 6;   // a character cell is 0.6 wide x 1.0 tall
  function render(vw, vh, rows, ramp, draw) {
    const scale = (rows * CH) / vh;
    const cols = Math.ceil((vw * scale) / CW);
    const cv = new OffscreenCanvas(cols * CW, rows * CH);
    const g = cv.getContext("2d");
    g.save(); g.scale(scale, scale); draw(g); g.restore();
    const img = g.getImageData(0, 0, cv.width, cv.height).data;
    const lines = [];
    for (let r = 0; r < rows; r++) {
      let line = "";
      for (let c = 0; c < cols; c++) {
        let sum = 0, n = 0;
        for (let y = r * CH; y < (r + 1) * CH; y++)
          for (let x = c * CW; x < (c + 1) * CW; x++) { sum += img[(y * cv.width + x) * 4 + 3] / 255; n++; }
        const cov = sum / n;
        let ch = " ";
        for (const [thr, gl] of ramp) if (cov >= thr) { ch = gl; break; }
        line += ch;
      }
      lines.push(line.replace(/\s+$/, ""));
    }
    return lines;
  }
  const ramp = [[0.55, "#"], [0.26, ":"], [0.09, "."]];
  const res = {};

  // Ethereum — octahedron, outer facets solid, inner facets half
  res.eth = render(256, 417, 12, [[0.72, "#"], [0.34, ":"], [0.12, "."]], (g) => {
    const p = (pts, a) => {
      g.globalAlpha = a; g.fillStyle = "#000"; g.beginPath();
      g.moveTo(pts[0][0], pts[0][1]); pts.slice(1).forEach(q => g.lineTo(q[0], q[1])); g.closePath(); g.fill();
    };
    p([[128, 0], [256, 212], [128, 288]], 1);
    p([[128, 0], [0, 212], [128, 288]], 0.55);
    p([[128, 312], [256, 236], [128, 417]], 1);
    p([[128, 312], [0, 236], [128, 417]], 0.55);
  });

  // Bitcoin — disc with the B knocked out as a real glyph, plus the ₿ prongs
  res.btc = render(46, 64, 12, ramp, (g) => {
    g.fillStyle = "#000";
    g.font = "bold 52px 'DejaVu Sans', sans-serif";
    g.textAlign = "center"; g.textBaseline = "middle";
    g.fillText("B", 23, 33);
    g.fillRect(8, 2, 6, 12); g.fillRect(20, 2, 6, 12);
    g.fillRect(8, 50, 6, 12); g.fillRect(20, 50, 6, 12);
  });

  // Solana — three bars, middle counter-slanted
  res.sol = render(100, 80, 8, [[0.45,"#"],[0.2,":"]], (g) => {
    g.fillStyle = "#000";
    const bar = (pts) => { g.beginPath(); g.moveTo(pts[0][0], pts[0][1]); pts.slice(1).forEach(q => g.lineTo(q[0], q[1])); g.closePath(); g.fill(); };
    bar([[16, 2], [100, 2], [84, 22], [0, 22]]);
    bar([[0, 30], [84, 30], [100, 50], [16, 50]]);
    bar([[16, 58], [100, 58], [84, 78], [0, 78]]);
  });
  return res;
});
for (const [k, v] of Object.entries(out)) {
  console.log("\n=== " + k + " (" + v.length + "x" + Math.max(...v.map(l => l.length)) + ") ===");
  v.forEach(l => console.log("|" + l));
}
console.log("\n" + JSON.stringify(out));
await b.close();
