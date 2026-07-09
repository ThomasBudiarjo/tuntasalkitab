---
name: verify
description: Build, run, and drive the Tuntas Alkitab app locally to verify changes end-to-end.
---

# Verifying Tuntas Alkitab

Go + chi + HTMX server-rendered app. No test suite for the UI — verify by running the server and driving it in a browser.

## Build & run

```bash
go build -o /tmp/bible-tracker .          # from repo root (module: bible-tracker)
DATABASE_PATH=/tmp/verify.db PORT=8493 /tmp/bible-tracker
```

- SQLite schema auto-migrates on startup (`schema.sql` is embedded). A fresh `DATABASE_PATH` gives a clean anonymous user (userID 0).
- Default port is 8493. No env vars required for local verification; Google auth routes exist but the tracker works anonymously.

## Drive it

- `GET /` — full page; `GET /month?month=N` — month partial; `POST /toggle/{dayOfYear}` — flips completion, returns `day_item` partial.
- Day rows are `#day-{dayOfYear}`; completed rows get class `completed` (CSS strikethrough). Checkbox: `.day-checkbox` inside the row.
- The mode toggle checkbox `#mode-switch` is visually hidden — click `.toggle-switch .toggle-slider` instead in Playwright.

## Gotchas

- **htmx loads from unpkg CDN** (`templates/layout.html`); the sandboxed Playwright browser cannot reach it. Download once with curl (which trusts the proxy CA) and fulfill via route interception:
  ```js
  await ctx.route('**/unpkg.com/**', r => r.fulfill({ contentType: 'application/javascript', body: htmxSrc }));
  ```
- Playwright is installed globally at `/opt/node22/lib/node_modules`; for ESM scripts symlink it: `ln -s /opt/node22/lib/node_modules node_modules` in your script dir.
- Back/forward re-sync: `pageshow` handler in `templates/index.html` refetches `/month` on back-forward navigation; simulate a stale history restore with `window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))`.
