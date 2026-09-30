# TGProxy Panel brand

The product is TGProxy Panel (repository `greenpandorik/tgproxy-panel`). That name
is the default of `branding_profiles.panel_name` (migration 00008), of
`branding.DefaultPanelName` in Go and of `DEFAULT_PANEL_NAME` in
`web/src/components/brand/brand.ts`. An operator renames an install in Settings →
Branding («Настройки» → «Оформление» in the Russian UI), in the "Panel name" field
(«Название панели») of a branding profile. The active profile is what every surface
shows: the sidebar, the login page, the browser tab title, the subscription page,
the TOTP issuer and the Telegram test message.

The Wave look of the interface borrows from the [Remnawave](https://github.com/remnawave/panel)
panel, as the README says: the floating sectioned sidebar, the metric cards with a
round tinted plate, and a blue-grey dark ground with a cyan primary. The code is the
project's own.

## Mark

The mark is a domino mask: an outline with two eye holes. It refers to what the
proxy does. Its Fake-TLS port and cover site make it pass for an ordinary website.

The outline takes the text colour of wherever it sits (`currentColor`). The eyes
take the brand primary, so they follow an operator's own primary colour.

Geometry, on a 24-unit grid (`MARK_PATH` and `MARK_EYES` in
`web/src/components/brand/Logo.tsx`):

```
outline  M2.6 9.7c0-1.9 1.4-2.9 3.2-2.9h12.4c1.8 0 3.2 1 3.2 2.9 0 4.2-2.4 7.6-5.5 7.6
         -1.9 0-3-1.3-3.9-2.8-.9 1.5-2 2.8-3.9 2.8-3.1 0-5.5-3.4-5.5-7.6z
         no fill, stroke currentColor, width 2, round joins
eyes     two ellipses at (8.1, 11.5) and (15.9, 11.5), rx 1.9, ry 1.4,
         fill --brand-primary
```

The outline spans x 2.6 to 21.4 and y 6.8 to 17.3. The notch over the nose rises to
(12, 14.5).

Files:

| File | What | Used by |
|---|---|---|
| `web/src/components/brand/Logo.tsx` | Inline React mark, sizes 16 / 24 / 40 / 72. The eyes default to `var(--brand-primary)`; the `accent` prop overrides them | Sidebar header (24), login wordmark (24), login brand column (72) |
| `web/public/favicon.svg`, `site/favicon.svg` | Mark on a 32 px `#171b21` tile with a 7 px corner; outline `#e9edf1`, eyes `#3fc0d6` | Browser tab until an operator uploads a favicon; the project page |
| `web/public/logo.svg`, `docs/brand/logo.svg` | Mark and wordmark, 174 × 32. Outline and wordmark `#1b2330`, eyes `#0b7285`; under `prefers-color-scheme: dark` they switch to `#e9edf1` and `#3fc0d6` | Standalone use |
| `docs/brand/logo-dark.png`, `docs/brand/logo-light.png` | The same at 3× (522 × 96), transparent ground | README header (`<picture>`) |
| `docs/brand/social-preview.png` | 1280 × 640 card: mark, name and tagline on the dark ground | Repository social preview |
| `internal/subscription/page.tmpl.html` | Mark inlined at 20 px, eyes in the page's resolved primary | Subscription page |
| `internal/subscription/error.tmpl.html` | Mark inlined at 22 px, eyes `#3fc0d6` in the dark theme and `#0b7285` in the light one | Subscription error page |

The SVG files and the templates make no external requests. An operator's uploaded
logo, and the separate dark-theme logo if there is one, replaces the mark in the
sidebar and on the login page.

The wordmark is "TGProxy Panel" set in Inter 600 at 20 px with -0.012 em tracking,
converted to outlines. Outside the favicon tile the mark stands on the page ground
without a box. It takes no gradient and no shadow, and the Telegram paper plane does
not appear next to it.

## Colours

The panel is mostly blue-grey neutrals. Two hues carry the brand, and each theme has
its own default pair.

### Surfaces and lines

| Token | Dark | Light | Utility | Used for |
|---|---|---|---|---|
| `--bg` | `#171b21` | `#f3f5f8` | `bg-background` | Page ground, table heads |
| `--bg-2` | `#1b2026` | `#ffffff` | `bg-surface`, `bg-card` | Panels, cards, dialogs, popovers |
| `--bg-3` | `#242a31` | `#e9edf2` | `bg-elevated` | Hover, selected rows, secondary buttons, disabled controls |
| `--field-bg` | `#13171c` | `#f8fafc` | | Inputs, textareas, native selects |
| `--sidebar` | `#14191f` | `#ffffff` | `bg-sidebar` | The floating sidebar |
| `--line` | `#2a3038` | `#e1e6ec` | `border-hairline` | Default border, rules inside panels |
| `--line-2` | `#3c434c` | `#cfd6de` | `border-hairline-strong` | Panel and tile borders, input borders, the dashed rule between sidebar groups |

Text tones (`--fg`, `--mute`, `--dim`) are in `type-and-shape.md`.

### Brand pair

| Theme | Role | Hex | Hue | on `--bg` | on `--bg-2` | on `--bg-3` |
|---|---|---|---|---|---|---|
| Dark | Primary | `#3fc0d6` | 189° | 8.00 : 1 | 7.59 : 1 | 6.70 : 1 |
| Dark | Accent | `#20c997` | 162° | 8.12 : 1 | 7.70 : 1 | 6.80 : 1 |
| Light | Primary | `#0b7285` | 189° | 5.11 : 1 | 5.59 : 1 | 4.75 : 1 |
| Light | Accent | `#099268` | 162° | 3.61 : 1 | 3.95 : 1 | 3.36 : 1 |

Ratios are WCAG 2.x relative-luminance contrast against the surfaces of the same
theme. Both primaries clear 4.5 : 1 on all three surfaces. The light accent clears
only the 3 : 1 floor for graphics. The one place the accent colours words is the copy
button on the subscription page, whose label turns accent on hover and after a copy.
It sits on the link row (`#e9edf2` in the light theme), where the light accent
measures 3.36 : 1.
`internal/branding/defaults_test.go` checks each primary for 4.5 : 1 and each accent
for 3 : 1 against its theme's `--bg` and fails below that.

A stored pair is resolved per theme by `ThemeColors` in Go and `themeColors` in
`web/src/theme/colors.ts`. When the pair is empty or one of the known default pairs,
the page gets the pair of the current theme. A custom pair is used as it is in both
themes, and an empty half falls back to the theme's value. "Restore default colors"
in the Branding tab puts `#3fc0d6` / `#20c997` back in the form. Once saved, the
profile follows the theme pairs again.

### Brand ink

`--brand-ink` is the brand hue at text weight. It is a mix of `--brand-primary`, so an
operator's own colour gets the same treatment.

| Theme | Definition | Default value | on `--bg` | on `--bg-2` | on `--bg-3` |
|---|---|---|---|---|---|
| Dark | `color-mix(in srgb, var(--brand-primary) 60%, white)` | `#8cd9e6` | 10.85 : 1 | 10.29 : 1 | 9.09 : 1 |
| Light | `color-mix(in srgb, var(--brand-primary) 70%, black)` | `#08505d` | 8.30 : 1 | 9.07 : 1 | 7.71 : 1 |

Text on brand fills, for the default pairs:

| What | Dark | Light |
|---|---|---|
| `--primary-foreground` on a solid primary fill | `#000000` on `#3fc0d6`, 9.72 : 1 | `#ffffff` on `#0b7285`, 5.59 : 1 |
| Default button label: ink on `bg-primary/15` over `--bg-2` | 7.75 : 1 | 7.33 : 1 |
| The same on hover (`bg-primary/25`) | 6.21 : 1 | 6.31 : 1 |
| Active sidebar item: ink on a 13 % primary tint over `--sidebar` | 8.76 : 1 | 7.56 : 1 |

`--primary-foreground` is `#000000` in the dark theme and `#ffffff` in the light one
in `index.css`. At runtime `ThemeProvider` replaces it with the result of
`brandForeground` (`web/src/theme/contrast.ts`): white when white reaches 4.5 : 1 on
the primary, black otherwise. `Foreground` in Go makes the same choice for the
subscription page.

### Where the brand colours go

The primary goes to:

- the eyes of the mark;
- the active sidebar item (`.panel-nav-link[aria-current='page']`): a 13 % tint, a
  75 % outline and the icon in the primary, and the active item of a section nav
  (`.section-nav`): a 75 % outline over a 10 % tint;
- the sidebar wash, a 7 % tint that fades out 38 % of the way down `.wave-sidebar`,
  and the 2 × 11 px marker before each sidebar group label (`.wave-group-label`);
- the default button, tinted at 15 % and 25 % on hover (`bg-primary/15 text-brand-ink`);
- the icon plate in a panel header (`bg-primary/12`), the plate of a neutral stat
  tile and the "active keys" plate on the dashboard (`.tgwp-tone-plate`);
- the focus ring (`--ring`), checkbox and switch fills, and native checkboxes
  (`accent-primary`);
- the server load bar below the warning level, the dot and outline of the update chip,
  the marker on the active branding profile, the upload drop zone in the Branding tab
  while a file is dragged over it, and the 40 % outline on a hovered dashboard metric;
- the glow on the login page (10 %) and text selection (25 %);
- chart series 1;
- the "open" button on the subscription page, as a solid fill.

The ink goes to links (the `link` button and badge variants), the default button
label, the active sidebar label, the update chip label, the keyboard focus outline
(2 px, offset 3 px, on links, buttons and summaries), the text caret in inputs, and
brand-coloured words on the keys, monitoring and server pages and in the help sheet.

The accent goes to chart series 2, the "traffic" plate on the dashboard, and the copy
button's hover and copied states on the subscription page.

The default primary and accent are 26° apart in the dark theme and 28° in the light
one. `seriesPalette` in `web/src/lib/chart.ts` draws the accent as series 2 only when
it is at least 25° from the primary (`MIN_HUE_SEPARATION_DEG`), so the default pair
passes with little room. The dark accent sits 14° from `--status-ok` (`#4fd18b`,
148°), and chart series 3 is the dark `--status-info` blue (`#4dabf7`, 18° from the
primary). Neither is used to show state.

### Status colours

Status colours are reserved for state. Each theme has its own values.

| Role | Dark | on `--bg` / `--bg-2` / `--bg-3` | Light | on `--bg` / `--bg-2` / `--bg-3` |
|---|---|---|---|---|
| ok | `#4fd18b` | 8.91 / 8.45 / 7.46 | `#237233` | 5.45 / 5.96 / 5.07 |
| warn | `#f5c451` | 10.61 / 10.07 / 8.89 | `#875b00` | 5.45 / 5.95 / 5.06 |
| err | `#ff6b6b` | 6.23 / 5.91 / 5.22 | `#bc2727` | 5.56 / 6.07 / 5.17 |
| info | `#4dabf7` | 6.98 / 6.62 / 5.85 | `#1a65ae` | 5.47 / 5.97 / 5.08 |

All four clear 4.5 : 1 on every surface in both themes. The light values were picked
so that a word in the status colour also clears 4.5 : 1 on its own 13 % tint over
`--bg` and `--bg-2`, which is how the diagnostics pill uses them.

`--status-online`, `--status-offline` and `--status-degraded` point at ok, err and
warn. `--status-pending` is `--mute`.

The solid destructive button uses `--destructive-solid`, `#da1313` (`#b81010` on
hover), in both themes. White text on it measures 5.14 : 1 (6.73 : 1 on hover). White
on the dark `--status-err` would measure 2.78 : 1.

### Chart series

Charts sit on `--bg-2`.

| Series | Dark | on `--bg-2` | Light | on `--bg-2` |
|---|---|---|---|---|
| 1 (`--series-1`) | brand primary | 7.59 : 1 | brand primary | 5.59 : 1 |
| 2 (`--series-2`) | brand accent | 7.70 : 1 | brand accent | 3.95 : 1 |
| 3 | `#4dabf7` | 6.62 : 1 | `#1971c2` | 5.02 : 1 |
| 4 | `#fcc419` | 10.19 : 1 | `#d26c00` | 3.57 : 1 |
| 5 | `#9775fa` | 4.87 : 1 | `#6741d9` | 6.30 : 1 |
| 6 | `#f06595` | 5.46 : 1 | `#c2255c` | 5.66 : 1 |
| 7 | `#199e70` | 4.81 : 1 | `#12855e` | 4.62 : 1 |
| 8 | `#e66767` | 5.07 : 1 | `#d23a3a` | 4.77 : 1 |
| other (`--series-other`) | `--dim` | 3.87 : 1 | `--dim` | 4.63 : 1 |

Most charts take series 1 and 2 from the resolved brand pair, then 3 to 6
(`seriesPalette`). The DC latency chart cycles all eight (`dcSeriesColor` in
`web/src/pages/nodes/dcDisplay.ts`). The "other" bucket and offline nodes are drawn in
`--dim`. The light series 4 sits just under the 3 : 1 floor for graphics.

## Where the defaults live

Keep these in step:

- `internal/store/migrations/`: column defaults. 00008 sets the name to
  `TGProxy Panel`. 00017 and 00019 move the colour defaults; the column default is now
  `#3fc0d6` / `#20c997`. Each colour migration updates only rows still on the previous
  default pair and keeps custom colours. Untouched rows went `#3b82f6` / `#22c55e` →
  `#e23c92` / `#12a198` → `#c4ed79` / `#c0a8ed` → `#3fc0d6` / `#20c997`. The light
  pair is never stored.
- `internal/branding/defaults.go`: `DefaultPanelName`, `DefaultPrimaryColor`,
  `DefaultAccentColor`, `LightPrimaryColor`, `LightAccentColor`, `DarkGround`,
  `LightGround`, the `defaultPairs` list, `ThemeColors` for the subscription page and
  `Foreground` for the text on its "open" button. The name is also the fallback for
  the Telegram test message, and the last fallback for the TOTP issuer after the
  public URL's host.
- `internal/branding/defaults_test.go`: contrast checks for both pairs, the
  `ThemeColors` cases and `Foreground`.
- `web/src/theme/colors.ts`: `THEME_COLORS`, `DEFAULT_PAIRS`, `isDefaultPair` and
  `themeColors`. `DEFAULT_PAIRS` and the Go `defaultPairs` hold the same pairs; a new
  default pair goes into both lists.
- `web/src/components/brand/brand.ts`: `DEFAULT_PANEL_NAME` and the dark pair the
  Branding form restores.
- `web/src/index.css`: every token, with the light values under
  `:root[data-theme='light']`. Its brand values apply until the SPA runs. Then
  `ThemeProvider` sets `--brand-primary`, `--brand-accent` and `--primary-foreground` on `<html>` from
  the resolved pair.
- `internal/subscription/page.tmpl.html` and `error.tmpl.html`: their own copies of
  the surfaces, lines and text colours for both themes. The error page's mark colours
  are fixed.
- `DEFAULT_BRAND_PRIMARY` / `DEFAULT_BRAND_ACCENT` in `DashboardPage.tsx`,
  `MonitoringPage.tsx`, `NodeStatsTab.tsx` and `KeyStatsSection.tsx`: the light pair
  as a chart fallback.
- The files in `web/public/`, `site/favicon.svg` and `docs/brand/`: colours baked in.
