# Type, shape and motion

The scales every part of the panel is built from. They are defined once in
`web/src/index.css` and reached through Tailwind utilities. New code does not set a
font size, a corner radius or a duration of its own. The few places that do today are
listed in each section.

## 1. Type scale

Six roles. Hierarchy comes from the role. Pick the role for what the text does, and
only then its tone.

| Role | Token | Size / leading | Tracking | Weight | Utility | Use it for |
|---|---|---|---|---|---|---|
| display | `--t-display` | 26 / 30 | -0.02em | 600 | `text-display` | The page title: `PageHeader`, the login heading, the server page heading. One per page. |
| title | `--t-title` | 17 / 24 | -0.01em | 600 | `text-title` | What a panel, dialog, sheet or settings section calls itself; the panel name in the sidebar. |
| body | `--t-body` | 14 / 21 | 0 | inherit | `text-body` | Everything a person reads: prose, field values, menu items, table cells, buttons. `body` sets it by default. |
| label | `--t-label` | 13 / 19 | 0 | inherit | `text-label` | What a field is called, a description under a title, a caption, a small button. |
| micro | `--t-micro` | 12 / 18 | +0.04em | 500 | `.micro` / `text-micro` | Chrome: table heads, group headings, badges, counters. |
| mono | `--t-mono` | 12.5 / 18 | -0.012em (from `.mono`) | inherit | `mono text-mono` | Anything a machine produced: hosts, ids, versions, secrets, sizes, timestamps. |

`--font-sans` is Inter Variable and `--font-mono` is JetBrains Mono Variable, both
from the `@fontsource-variable` packages imported in `web/src/main.tsx`. `.mono` also
turns on tabular figures, and so do `th`, `td` and number inputs.

Rules:

- One `display` heading per page. If a second heading wants to be big, it is a
  `title`.
- The number in a stat tile sits outside the six roles. `StatTile.tsx` and
  `DashboardMetrics.tsx` set it in mono at 19 / 28, weight 600
  (`text-[19px] leading-7 font-semibold`).
- Table heads are `micro`, in the `--mute` tone. `index.css` gives
  `[data-slot='table-head']` weight 600 and the `--bg` ground. Badges and counters are
  `text-micro`.
- Numbers, hostnames, ids, commits and byte counts are `mono`, right aligned in a
  column.
- Uppercase with tracking belongs to the `micro` role. The one other place is the
  sidebar group label (`.wave-group-label` in `Sidebar.tsx`): 11 / 16, weight 600,
  0.12em, uppercase, written with arbitrary utilities.

### micro: two utilities

- `.micro` is the complete role: size, leading, +0.04em, uppercase, weight 500. Use it
  for chrome whose text is a word you wrote, such as a table head.
- `text-micro` is the size and leading only. Use it when the content is a machine
  value that must not be uppercased or re-tracked, such as a badge holding a hostname,
  where `.mono` already owns the tracking.

`tracking-micro` exposes the +0.04em on its own, for the few places a class cannot be
applied directly. cmdk renders its own group heading element, so `command.tsx`
reaches it through a child selector.

### There is no second scale

Tailwind's t-shirt sizes (`text-xs`, `text-sm`, `text-base`, `text-xl`, `text-2xl`)
are not used in `src/**`, and body text is `text-body`. `text-sm` resolves to
Tailwind's stock 14px, which is not a role in this system. The one t-shirt size in
the code today is `text-lg`, twice in `components/web/TelemtUpdateCard.tsx` for the
installed and recommended versions.

Arbitrary sizes (`text-[Npx]`) are not part of the system either. Three exist today:
`text-[19px]` in the two stat tiles and `text-[11px]` in the sidebar group label. Do
not add more.

### Tracking

`--tracking-micro` (+0.04em) belongs to the micro role. `.mono` sets -0.012em, and the
display and title roles carry their own. `--tracking-code` (0.35em, the
`tracking-code` utility) is the cell gap of a one-time-code field, where the eye
counts characters instead of reading a word. Three inputs use it: the login code
field and the two TOTP code fields in `TotpSection.tsx` (enrolment and disable).
Their placeholders use it too, so the placeholder characters stand where the digits
will.

Other hand-written letter spacing is a bug. Two cases exist today, both in
`Sidebar.tsx`: `tracking-[0.12em]` on the group label and `tracking-tight` on the
panel name.

## 2. Shape scale

Three radius tokens and a zero.

| Token | Value | Utility | Use it for |
|---|---|---|---|
| `--r-control` | 8px | `rounded-control` | Anything you click or type into: buttons, inputs, textareas, select triggers, select and menu items, command items, tabs, badges, tooltips, segmented controls, sidebar links. Also small icon squares: the panel header plate and the tinted icon squares. |
| `--r-surface` | 8px | `rounded-surface` | Anything that contains those: panels, stat tiles, dialogs, popovers, dropdown and select popups, sheets, toasts, empty states, the dashboard verdict line. |
| `--r-pill` | 9999px | `rounded-pill` | Status dots, switch tracks and thumbs, the round plates on stat tiles, chip dots, status pills, the ends of the carrier share bar. |
| (none) | 0 | | Table rows, table heads, hairlines, dividers. A rounded row would fight the grid the eye follows down a column. |

Control and surface are both 8px. They stay two tokens, and a component picks one by
its role.

The `@theme` block also sets Tailwind's own radius scale for shadcn classes:
`--radius-sm` 4px, `--radius-md` 6px, `--radius-lg` 8px, `--radius-xl` 8px,
`--radius-2xl` 10px. Of these, `src/**` uses only `rounded-sm`.

A one-off radius is not allowed in new code. The ones in the code today:

- The checkbox keeps `rounded-sm` (4px). The box is 16px, so the 8px control radius
  would draw a circle and the checkbox would read as a radio.
- The skeleton keeps `rounded-sm` (4px). It stands in for a line of text or a cell,
  and a control radius on an `h-3` line draws a pill.
- `.wave-sidebar` has a 14px radius in `index.css`, and the marker before a sidebar
  group label has 1px.
- The legend swatch in `components/web/CarrierDistribution.tsx` uses `rounded-[3px]`.

A drawer takes the surface radius only on its inner edge
(`data-[side=right]:rounded-l-surface` and so on). The edges flush with the viewport
stay square. A dialog footer rounds only its bottom corners (`rounded-b-surface`).

Floating surfaces (dialogs, sheets, popovers, menus, select popups, toasts, tooltips)
share `--shadow-popover`: `0 12px 32px rgba(0, 0, 0, 0.45)` in the dark theme and
`0 12px 32px rgba(27, 35, 48, 0.14)` in the light one.

## 3. Motion

CSS only, no animation library.

| Token | Value | How to reach it | Use it for |
|---|---|---|---|
| `--ease-out` | `cubic-bezier(0.2, 0, 0, 1)` | any `transition-*`, or `ease-out` | Every transition and animation in the panel except the pulse ring. Fast out of the gate, long settle. |
| `--dur-fast` | 120ms | any `transition-*`, or `duration-fast` | A state the pointer is holding: hover, press, focus. Tooltips use it too. |
| `--dur-base` | 180ms | `duration-base` | Something arriving or leaving: an entrance, a dialog, sheet, popover, menu, select or toast, and the sidebar collapsing to its rail. |

The token is named `--ease-out`, which is also Tailwind's own easing key, so the
`ease-out` utility and every hand-written transition share one curve.

Tailwind has no theme namespace for durations, so `.duration-fast` and
`.duration-base` are written out once in `index.css`. Both read the tokens, and both
set `--tw-duration` as well as the two longhands, because tw-animate-css builds
`animate-in` and `animate-out` as an `animation` shorthand that reads that variable.
Reach for the utility. A literal duration is not used, with one exception today:
`duration-200` on the screenshot zoom in `pages/sites/WebsiteGallery.tsx`.

A bare `transition-colors` is already 120ms on the panel curve: `index.css` sets
`--default-transition-duration` and `--default-transition-timing-function` to the two
tokens. So `duration-fast` is only worth writing next to a hand-written property list
where the intent is worth saying out loud.

When a transition needs to carry the press, put `scale` in its property list instead
of `transform`, because Tailwind v4's `scale-*` utilities set the standalone `scale`
property. The shorthand `transition-transform` already covers `transform`,
`translate`, `scale` and `rotate`.

The sidebar collapses to an icon rail from a toggle in the header, on screens from
`lg` up. Its column animates its width between 276px and 84px on `duration-base
ease-out`, and the choice is kept as the `sidebar-collapsed` preference.

The pulse ring on a live status dot (`.tgwp-pulse-ring`) runs its own loop: 1.8s on
`cubic-bezier(0.2, 0.7, 0.4, 1)`, growing from scale 1 to 2.4 while its opacity falls
from 0.5 to 0.

### Press

Buttons, menu, select and command items, segmented controls, sidebar links, status
chips, clickable stat tiles and the rows of the branding profile list carry
`active:scale-[0.985]`. It is a transform, so it costs no layout, and it gives the
pointer something to feel on a page whose hover states are only colour. New clickable
controls get it too. The four metric cards in `DashboardMetrics.tsx` do not have it
today.

The tab goes without it. Its marker is a 2px rule sitting on the tab list's
hairline, and shrinking the tab would lift that marker off the line for the length of
the press. Table rows do not carry it either, because a `tr` is not reliably
transformable and the row is only sometimes clickable. A clickable row gets its
feedback from the `--bg-3` hover.

### Entrance

Fade plus a 2px rise, staggered 40ms, capped at six elements. Use it for content that
has just arrived: the panels of a page, a row of stat tiles, the first page of a
table. Do not use it for content that was already on screen.

`src/components/ui/motion.ts` is the only way to apply it:

```tsx
import { enter } from '@/components/ui/motion';

{tiles.map(({ id, ...props }, i) => (
  <StatTile key={id} {...props} {...enter(i)} />
))}
```

`enter(i)` returns the `tgwp-enter` class and the `--enter-index` the animation reads
for its delay. The index is clamped at 5, so element seven onwards lands with element
six and the last delay is 200ms. When the element already has a `className`, compose
it instead:

```tsx
<div className={cn(ENTER_CLASS, 'grid gap-4')} style={enterDelay(2)} />
```

### Reduced motion

`@media (prefers-reduced-motion: reduce)` in `index.css` switches the panel's motion
off in three layers. Nothing in the panel moves under it, including things nobody
thought to list.

1. `--dur-fast` and `--dur-base` go to `0ms`, so every transition built on them,
   including every bare `transition-*`, becomes an instant state change without any
   component knowing about it.
2. A blanket `*, *::before, *::after` rule forces `animation-duration` and
   `transition-duration` to `1ms`, `animation-iteration-count` to `1`, both delays to
   `0` and `scroll-behavior` to `auto`, all `!important`. This catches what the tokens
   cannot reach: the `animate-in` / `animate-out` fade, zoom and slide on dialogs,
   sheets, popovers, selects, menus and tooltips, the skeleton's `animate-pulse`, and
   every `animate-spin` on a loading icon. It is a wildcard so that code written after
   this file is covered by default. It uses 1ms because a zero-length animation never
   fires `animationend`, and the iteration count is what stops a loop.
3. Two motifs would still read as motion at 1ms: the pulse ring on a live status dot,
   which is a growing circle, and the entrance, which starts at `opacity: 0`. Both are
   switched off outright.

Nothing new needs to add itself to that block.

## 4. Tone

Colour decides what can be read, and two of the panel's tones cannot stand in for
each other. Ratios below are on `--bg` / `--bg-2` / `--bg-3` of the same theme.

| Token | Dark | Light | What it is for |
|---|---|---|---|
| `--fg` | `#e9edf1` (14.69 / 13.93 / 12.30) | `#1b2330` (14.46 / 15.79 / 13.43) | Primary text: the value, the answer, the title. |
| `--mute` | `#9ba4ae` (6.84 / 6.49 / 5.73) | `#5b6673` (5.35 / 5.84 / 4.97) | Everything else a person reads: a description, an empty state, a field label, a table head, helper text, a placeholder, and every machine value a person actually reads: a timestamp, a count, an id, a hostname, a byte figure, a version, an IP, a status word, a pagination summary. |
| `--dim` | `#737c86` (4.08 / 3.87 / 3.42) | `#6b7682` (4.24 / 4.63 / 3.94) | Not a text tone. Marks that carry no information on their own: the separator glyph between two items, the dash standing in for an absent value, the gutter line numbers in the code editor, an icon that only repeats the label beside it. |

`--dim` stays under 4.5:1 on every surface except the light `--bg-2` (`#ffffff`),
where it reaches 4.63. Treat it as a mark tone everywhere. It is for marks you are
meant to skip, and small text does not qualify. A string a machine wrote still has to
be readable: a timestamp, a byte count, a job duration, an audit meta line and a role
name are all `--mute`.

Check this on the rendered page; class names do not tell you. Measure the computed
colour of every text element against its nearest opaque background, in both themes.
Anything under 4.5:1 has to be one of the four marks above.

### Status and brand across themes

The status hues and the brand hue have different values in each theme. The light
theme redefines the surfaces, lines, text tones, the brand pair,
`--primary-foreground`, `--brand-ink`, the four `--status-*` colours, chart series 3
to 8 and the popover shadow. `docs/design/brand.md` lists the values and their
ratios.

All four status colours clear 4.5:1 on every surface in both themes, so they can carry
words as well as the 7px status dots and the status icons.

Three tokens exist so that a fill and a word can be different values of one colour:

- `--destructive-solid` (`#da1313`, `#b81010` on hover, the same in both themes) is
  the fill under the one solid destructive button. White on it is 5.14:1 (6.73:1 on
  hover). White on the dark `--status-err` would be 2.78:1.
- `--brand-ink` is the brand hue at text weight. It is used for links, the `link`
  button and badge variants, the default button label, the active sidebar label, the
  update chip, the keyboard focus outline and the text caret. `--brand-primary` stays
  the identity colour for graphics that owe 3:1: the eyes of the mark, the active nav
  icon, the focus ring, checkbox and switch fills, tone plates and the first chart
  series. The dark theme lightens it with `color-mix(in srgb, var(--brand-primary)
  60%, white)` and the light theme darkens it with `color-mix(in srgb,
  var(--brand-primary) 70%, black)`. It is a mix so that an operator's own brand
  colour, which arrives at runtime on `--brand-primary`, is treated the same way.
- `--primary-foreground` is the text on a solid primary fill. `index.css` sets
  `#000000` in the dark theme and `#ffffff` in the light one, and `ThemeProvider`
  replaces it with the result of `brandForeground` for the active primary.

### Tinted tones

Two utilities in `index.css` paint a tone from `--tone`, which a component sets inline
from `TONE_VAR` (`components/common/statTone.ts`) or a brand variable:

- `.tgwp-tone-plate`: 13 % of the tone mixed into `--bg-2`, a 1px inset ring at 16 %,
  content in the tone. It is the round 44px icon plate on stat tiles and dashboard
  metrics.
- `.tgwp-tone-tint`: 10 % of the tone as the background, a border at 20 %, content in
  the tone. It is used for the empty-state icon, the dashboard verdict line, small
  status squares in the branding profile list and the server dialog, and the status
  pills in `WebDiagnosticsCard.tsx`.

On a plate or a tint the tone colours icons and borders. The one place it colours
words is the diagnostics status pill. It sits on a card, and every status colour clears
4.5:1 on its own tint over `--bg` and `--bg-2` in both themes.

## 5. Fixed values

Every token in `index.css` is fixed per theme except the brand pair and
`--primary-foreground`: the surfaces, the field and sidebar grounds, the two hairline
strengths, the three text tones, the four status colours, the destructive fill, chart
series 3 to 8 and the popover shadow. `--brand-primary` and `--brand-accent` take the
resolved pair described in `docs/design/brand.md`. `--brand-ink` and every brand tint
follow them through `color-mix`. `--destructive-solid` has one value for both themes.
An operator's custom CSS from the Branding tab is added after the
stylesheet and can override any of this.

Sizing that depends on density and pointer:

| Token or rule | Comfortable | Compact | Applies to |
|---|---|---|---|
| `--control-height` | 38px | 32px | The default button height, and the minimum height of inputs and select triggers |
| `--cell-padding-y` | 14px | 8px | Vertical padding of table cells |

Density is a personal preference (`data-density` on `<html>`). Under
`pointer: coarse`, buttons, inputs, select triggers and sidebar links get a 44px
minimum height, and native selects (`.ops-select`) are at least 2.75rem tall
everywhere.

The sidebar is 276px wide, or 84px as a rail, with 40px links. Below `lg` it opens as
a 256px drawer from the left.
