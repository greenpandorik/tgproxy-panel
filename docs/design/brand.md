# TGProxy Panel brand

Product name: **TGProxy Panel** (repository `greenpandorik/tgproxy-panel`). The
name is the default value of `branding_profiles.panel_name` (migration 00008),
`branding.DefaultPanelName` in Go and `DEFAULT_PANEL_NAME` in
`web/src/components/brand/brand.ts`; an operator can rename their install in
Settings → Branding and every surface follows.

## Mark

A rotated square outline with an axis-aligned square inside: the wrapper around
the payload. That is what the product does - it hides MTProto inside WEB or
Fake-TLS traffic - and it is two shapes, so it still reads at 16 px. The outline
takes the text colour of wherever it sits (`currentColor`); the core takes the
brand primary, so the mark agrees with the active-nav marker next to it and
follows an operator's own primary colour.

Geometry, on a 24-unit grid:

```
outline  M12 1.75 L22.25 12 L12 22.25 L1.75 12 Z   stroke 2.4, round joins
core     rect 9,9 6×6, radius 1                    fill --brand-primary
```

Files:

| File | What | Used by |
|---|---|---|
| `web/src/components/brand/Logo.tsx` | Inline React mark, sizes 16 / 24 / 40 | Sidebar header (16), login wordmark (24) |
| `web/public/favicon.svg` | Mark on a `#09090b` tile, 32 px | Browser tab, until an operator uploads a favicon |
| `web/public/logo.svg`, `docs/brand/logo.svg` | Mark + wordmark (Inter 600, outlined) | Standalone use; wordmark follows `prefers-color-scheme` |
| `docs/brand/logo-dark.png`, `logo-light.png` | The same at 3×, transparent ground | README header (`<picture>`) |
| `internal/subscription/*.tmpl.html` | Mark inlined as SVG | Subscription and error pages (no external requests) |

The wordmark is the panel name set in Inter 600 at 20 px with −0.012 em
tracking, converted to outlines from the bundled `@fontsource-variable/inter`
file (`scratchpad/brand/wordmark.py` regenerates it). Do not put the mark in a
box, add gradients or a shadow, or use the Telegram paper plane next to it.

## Colours

The panel stays almost colourless - three near-black surfaces, hairlines,
three text weights (see `docs/superpowers/specs/2026-09-05-ui-redesign.md`).
Two hues carry the brand:

| Role | Hex | Hue | on `--bg` `#09090b` | on `--bg-2` `#0f0f11` | on `--bg-3` `#151517` | on light `#fafafa` |
|---|---|---|---|---|---|---|
| Primary (magenta) | `#e23c92` | 329° | **5.02 : 1** | 4.83 : 1 | 4.60 : 1 | 3.80 : 1 |
| Accent (teal) | `#12a198` | 176° | **6.23 : 1** | 6.00 : 1 | 5.71 : 1 | 3.06 : 1 |

Ratios are WCAG 2.x relative-luminance contrast. The primary is used as link
text on the dark ground, so it has to clear 4.5 : 1 (AA text) there and does on
all three dark surfaces; on the light theme it clears 3 : 1 (UI) and is a little
under AA for body text - the same trade the previous blue made (3.8 : 1 on white),
accepted because links in the panel are short and underlined on hover. The
accent only ever colours chart strokes, where 3 : 1 is the floor, and clears it
in both themes. `internal/branding/defaults_test.go` recomputes the dark-ground
ratios and fails the build if a default drifts below the floor.

Where they go:

- **Primary** - the active nav item's 2 px marker, the focus ring, links,
  checkbox/switch fills, the first chart series, the login page glow (10 %
  alpha) and the core of the mark. Never a button fill: the primary button is
  `--fg` on `--bg`.
- **Accent** - the second chart series and the copy-button hover on the
  subscription page. Nothing else, so an operator who leaves it alone never
  sees a second hue outside a chart.

Why these two: the previous pair (`#3b82f6` / `#22c55e`) was the status palette
wearing a brand badge, and Telegram's own blue (`#2aabee`) is off the table.
Magenta at 329° is 128° from Telegram blue, 173° from `ok`, 69° from `warn`
and 31° from `err` - the closest neighbour, but a magenta tab beside a red dot
is not confusable, and the two never share a role. Teal at 176° sits between
`ok` (34°) and `info` (41°) in hue and is only used for chart strokes, where
the legend names the series.

Status colours are unchanged and still reserved for state:

| Role | Hex | on `#09090b` |
|---|---|---|
| ok | `#22c55e` | 8.73 : 1 |
| warn | `#f59e0b` | 9.26 : 1 |
| err | `#ef4444` | 5.29 : 1 |
| info | `#3b82f6` | 5.41 : 1 |

Chart series 3-8 are unchanged except series 5, which was a pink (`#d55181`)
and is now a blue (`#4f8ef7` dark / `#2f6fd6` light): with a magenta series 1
it was the one hue a chart could not tell apart.

Where the defaults live (keep them in step):

- `internal/store/migrations/00008_rename_default.sql` - column defaults
- `internal/branding/defaults.go` - Go fallbacks for the subscription page, the
  TOTP issuer and the Telegram test message
- `web/src/index.css` - `--brand-primary` / `--brand-accent` fallbacks before
  `/api/v1/branding` answers
- `web/src/components/brand/brand.ts` - the name and colours the SPA shows
  while that request is in flight
