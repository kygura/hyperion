# Pitch Deck — Hyperion

This directory contains the Hyperion pitch deck materials.

---

## Files

### `index.html`
The main landing page and pitch deck. Deployed at: TBD

**Do not rewrite.** The messaging is authored and final.

### `deck.md`
Markdown version of the pitch slides (optional reference).

### `media/`
Assets (GIFs, videos, images) embedded in the deck.

---

## Using the Deck

### For YC / Investors

1. **Deploy** `index.html` to a static host (Vercel, Cloudflare Pages, etc.)
2. **Demo URL** in the YC application form should point to the deployed URL
3. **Demo recording** (VHS TUI walk-through) should be embedded on the page

### For Internal Use

- Open `index.html` in a browser locally: `open pitch/index.html`
- Or serve locally: `python3 -m http.server --directory pitch/`

---

## Deployment

### Option 1: Vercel

```bash
# Install Vercel CLI if not already done
npm i -g vercel

# Deploy from project root
cd pitch
vercel --prod
```

Your live URL will be something like: `hyperion-pitch.vercel.app`

### Option 2: Cloudflare Pages

```bash
# Use Wrangler (Cloudflare CLI)
npm i -g wrangler

# Create a new Pages project
wrangler pages deploy pitch/ --project-name hyperion
```

### Option 3: GitHub Pages

Push `pitch/` to a `gh-pages` branch or use GitHub Actions to auto-deploy.

---

## Customization

If you need to update messaging:

1. **Do not edit `index.html` directly** — it's a compiled artifact
2. **Contact the original author** — the copy is authored and locked

If you need to update assets (videos, GIFs):

1. Replace the file in `media/`
2. Update references in `index.html` if the filename changed
3. Test locally before deploying

---

## Live Demo Recording

The deck should embed a demo recording showing:

1. TUI markets view
2. TUI reasoning theses
3. TUI decision journal navigation
4. Dashboard agent console (live WS event stream)

See `docs/YC-APPLICATION.md` section 5 for the VHS recording plan.

Once recorded:

1. Save output MP4 to `media/demo.mp4`
2. Update `index.html` to embed the video
3. Re-deploy

---

## Hosting Checklist

Before going live:

- [ ] Remove any `.env` or API keys from the repo
- [ ] Rotate any keys if the repo was ever public
- [ ] Test the deployed URL in a private browser (no cache)
- [ ] Verify all media loads (GIFs, videos)
- [ ] Test on mobile
- [ ] Verify the demo recording plays

---

## Next Steps

1. Record founder video (1 min, all founders, no script)
2. Record TUI demo (30 sec, VHS)
3. Deploy landing page
4. Add demo recording URL to YC application form
5. Share landing page URL with early users for feedback

---

*Pitch authored: 2026-07-07*  
*Do not modify the core messaging.*
