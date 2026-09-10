# Type, shape and motion

The three scales every part of the panel is built from. They are defined once in
`web/src/index.css` and reached through Tailwind utilities. Nothing in `src/**`
sets a font size, a corner radius or a duration of its own.

## 1. Type scale

Six roles. Colour is not hierarchy: before a role is chosen by tone, it is
chosen by job.

| Role | Token | Size / leading | Tracking | Weight | Utility | Use it for |
|---|---|---|---|---|---|---|
| display | `--t-display` | 26 / 30 | -0.02em | 600 | `text-display` | The page title, and the number in a KPI tile. One heading per page. |
| title | `--t-title` | 17 / 24 | -0.01em | 600 | `text-title` | What a panel, dialog or sheet calls itself. |
| body | `--t-body` | 14 / 21 | 0 | inherit | `text-body` | Everything a person reads: prose, field values, menu items, table cells, buttons. |
| label | `--t-label` | 12.5 / 16 | 0 | inherit | `text-label` | What a field is called, a description under a title, a caption, a small button. |
| micro | `--t-micro` | 11 / 14 | +0.04em | 500 | `.micro` / `text-micro` | Chrome: table heads, group headings, chips, counters. |
| mono | `--t-mono` | 12.5 / 18 | (from `.mono`) | inherit | `mono text-mono` | Anything a machine produced: hosts, ids, versions, secrets, sizes, timestamps. |

Rules:

- One `display` *heading* per page. If a second heading wants to be big, it is
  a `title`. The KPI row is the one place the display size repeats: four stat
  tiles all set their number at `display`, because the row is read as one
  object and a number that shrank would read as the less important one.
- Table heads are `micro`, in the `--mute` tone. Chips and counters are
  `text-micro`.
- Numbers, hostnames, ids, commits and byte counts are `mono`, right aligned in
  a column.
- Uppercase with tracking exists only in the `micro` role. Anywhere else it is
  a bug.

### micro: two utilities, on purpose

- `.micro` is the complete role: size, leading, +0.04em, uppercase, weight 500.
  Use it for chrome whose text is a word you wrote, such as a table head.
- `text-micro` is the size and leading only. Use it when the content is a
  machine value that must not be uppercased or re-tracked, such as a badge
  holding a hostname, where `.mono` already owns the tracking.

`tracking-micro` exposes the +0.04em on its own, for the few places a class
cannot be applied directly (cmdk renders its own group heading element, so
`command.tsx` reaches it through a child selector).

### There is no second scale

The older t-shirt sizes (`text-xs`, `text-sm`, `text-base`, `text-xl`,
`text-2xl`) were retuned in `@theme` while the pages were migrating. The
migration is finished - `src/**` contains none of them - so the retuning is
gone with them and `body` is `text-body`. `text-sm` now resolves to Tailwind's
stock 14px, which is not a role in this system. Do not reach for it.

### Tracking

`--tracking-micro` (+0.04em) belongs to the micro role and is the only
typographic tracking in the panel. `--tracking-code` (0.35em, the
`tracking-code` utility) is not a typographic value: it is the cell gap of a
one-time-code field, where the eye counts six characters instead of reading a
word. It is used by exactly two inputs - the login OTP and the TOTP
confirmation - and by their placeholders, so the dots stand where the digits
will. Anywhere else, hand-written letter spacing is a bug.

## 2. Shape scale (the shape lock)

Three radii and a zero. There is no fourth value.

| Token | Value | Utility | Use it for |
|---|---|---|---|
| `--r-control` | 8px | `rounded-control` | Anything you click or type into: buttons, inputs, textareas, select triggers, select and menu items, command items, tabs, chips, badges, tooltips, segmented controls. |
| `--r-surface` | 12px | `rounded-surface` | Anything that contains those: panels, cards, stat tiles, dialogs, popovers, dropdown surfaces, sheets, toasts, empty states, chart tooltips. |
| `--r-pill` | 9999px | `rounded-pill` | Only status dots, avatars, switch tracks and thumbs, counter pills. |
| (none) | 0 | | Table rows, table heads, hairlines, dividers. A rounded row would fight the grid the eye follows down a column. |

A one-off pixel radius (`rounded-[6px]`) is not allowed. Two components sit off
the scale deliberately, and both say why in a comment:

- **Checkbox** keeps `rounded-sm` (4px). The box is 16px, so a control radius
  would draw a circle and the checkbox would read as a radio.
- **Skeleton** keeps `rounded-sm` (4px). It is neither a control nor a surface;
  it is the silhouette of the text or cell it stands in for, and a control
  radius on an `h-3` line draws a pill.

A drawer takes the surface radius only on its inner edge
(`data-[side=right]:rounded-l-surface` and so on) - the edges flush with the
viewport stay square.

## 3. Motion

CSS only, no animation library.

| Token | Value | How to reach it | Use it for |
|---|---|---|---|
| `--ease-out` | `cubic-bezier(0.2, 0, 0, 1)` | any `transition-*`, or `ease-out` | Every transition and animation in the panel. Fast out of the gate, long settle. |
| `--dur-fast` | 120ms | any `transition-*`, or `duration-fast` | A state the pointer is holding: hover, press, focus. |
| `--dur-base` | 180ms | `duration-base` | Something arriving or leaving: an entrance, a panel opening, an overlay. |

The token is deliberately named `--ease-out`, which is also Tailwind's own
easing key, so the `ease-out` utility and every hand-written transition share
one curve.

Tailwind has no theme namespace for durations, so `.duration-fast` and
`.duration-base` are written out once in `index.css` instead. Both read the
tokens, and both set `--tw-duration` as well as the two longhands, because
tw-animate-css builds `animate-in` and `animate-out` as an `animation`
shorthand that reads that variable. Reach for the utility, never for a literal
`duration-150` and never for `duration-[var(--dur-base)]`.

A bare `transition-colors` is already 120ms on the panel curve: `index.css`
retunes `--default-transition-duration` and `--default-transition-timing-function`
to the two tokens. So `duration-fast` is only worth writing next to a
hand-written property list where the intent is worth saying out loud.

When a transition needs to carry the press, put `scale` in its property list,
not `transform` - Tailwind v4's `scale-*` utilities set the standalone `scale`
property. The shorthand `transition-transform` already covers `transform`,
`translate`, `scale` and `rotate`.

### Press

Every button and every clickable row or menu item carries
`active:scale-[0.985]`. It is a transform, so it costs no layout, and it gives
the pointer something to feel on a page whose hover states are only colour.

The one clickable control without it is the tab: its marker is a 2px rule
sitting exactly on the tab list's hairline, and shrinking the tab would lift
that marker off the line for the length of the press. Table rows do not carry
it either, because a `tr` is not reliably transformable and the row is only
sometimes clickable; a clickable row gets its feedback from the `--bg-3` hover.

### Entrance

Fade plus a 2px rise, staggered 40ms, capped at six elements. Use it for
content that has just arrived: the panels of a page, a row of stat tiles, the
first page of a table. Do not use it for content that was already on screen.

`src/components/ui/motion.ts` is the only way to apply it:

```tsx
import { enter } from '@/components/ui/motion';

{tiles.map((tile, i) => (
  <StatCard key={tile.id} {...enter(i)} {...tile} />
))}
```

`enter(i)` returns the `tgwp-enter` class and the `--enter-index` the animation
reads for its delay. The index is clamped at 5, so element seven onwards lands
with element six and a long list still finishes inside 200ms. When the element
already has a `className`, compose it instead:

```tsx
<div className={cn(ENTER_CLASS, 'grid gap-4')} style={enterDelay(2)} />
```

### Reduced motion

`@media (prefers-reduced-motion: reduce)` in `index.css` switches the panel's
motion off in three layers, and the guarantee is that **nothing in the panel
moves under it** - not just the things somebody remembered to list.

1. `--dur-fast` and `--dur-base` go to `0ms`, so every transition built on
   them, including every bare `transition-*`, becomes an instant state change
   without any component knowing about it.
2. A blanket `*, *::before, *::after` rule forces `animation-duration` and
   `transition-duration` to `1ms`, `animation-iteration-count` to `1` and both
   delays to `0`, all `!important`. This is what catches what the tokens
   cannot reach: the `animate-in` / `animate-out` fade, zoom and slide on
   dialogs, sheets, popovers, selects, menus and tooltips, the skeleton's
   `animate-pulse`, and every `animate-spin` on a loading icon. It is a
   wildcard rather than a list so that code written after this file is covered
   by default. 1ms rather than 0s because a zero-length animation never fires
   `animationend`, and the iteration count is what actually stops a loop.
3. The two motifs that would still read as motion even at 1ms - the pulse ring
   on a live status dot, which is a growing circle, and the entrance, which
   starts at `opacity: 0` - are switched off outright.

Nothing new needs to add itself to that block. If something does need to opt
out of layer 2, it has to say why in a comment there.

## 4. Tone

Colour is not hierarchy, but it is legibility, and two of the panel's tones are
not interchangeable.

| Token | Dark | Light | What it is for |
|---|---|---|---|
| `--fg` | `#f4f4f5` | `#111113` | Primary text: the value, the answer, the title. |
| `--mute` | `#8b8d93` (5.77:1 on `--bg-2`) | `#6b6d75` (5.16:1) | Everything else a person reads: a description, an empty state, a field label, a table head, helper text, a placeholder, an error line - and every machine value a person actually reads: a timestamp, a count, an id, a hostname, a byte figure, a version, an IP, a status word, a pagination summary. |
| `--dim` | `#55575f` (2.76 / 2.53 / 2.36 on `--bg` / `--bg-2` / `--bg-3`) | `#9a9ca3` (2.54 / 2.74 / 2.43) | Not a text tone. Marks that carry no information on their own: the separator glyph between two items, the dash standing in for an absent value, the gutter line numbers in the code editor, an icon that only repeats the label beside it. |

`--dim` misses the 4.5:1 floor on every surface in both themes, so it can never
carry meaning. It is the tone of a mark you are meant to skip, not of small
text. That a machine wrote the string is not a reason a person cannot read it:
a timestamp, a byte count, a job duration, an audit meta line and a role name
are all `--mute`, however incidental they look in a mock.

The check is a rendered sweep, not a reading of the class name. Measure the
computed colour of every text element against its nearest opaque background,
in both themes; anything under 4.5:1 has to be one of the four marks above.

### Status and brand across themes

The status hues and the brand hue are the same five colours in both themes, but
not the same five *values*: a colour tuned to glow off a near-black ground
disappears on a near-white one. `index.css` therefore redefines all four
`--status-*` under `[data-theme='light']`, measured so that all twelve
combinations (four hues on `--bg`, `--bg-2` and `--bg-3`) clear 4.5:1, which
also puts the 7px status dots well clear of the 3:1 non-text floor.

Two tokens exist purely so a fill and a word can be different values of one
colour:

- **`--destructive-solid`** is the fill under the one solid destructive button.
  `--status-err` is a text red and white on it is 3.76:1; white on
  `--destructive-solid` is 5.14:1 in both themes.
- **`--brand-ink`** is the brand hue at text weight, used for links, the `link`
  button and badge variants, and the brand chip. `--brand-primary` stays the
  identity - the active nav marker, the focus ring, the checkbox fill, the
  first chart series - and those are graphics that owe 3:1. Prose owes 4.5:1,
  which the default magenta does not make on a light surface, so the light
  theme darkens it with `color-mix(in oklab, var(--brand-primary) 82%, black)`.
  It is a mix and not a constant so that an operator's own brand colour, which
  arrives at runtime on `--brand-primary`, is darkened with it.

## 5. What did not change

The near-black surfaces, the two hairline strengths, the three text weights and
the brand hue itself. `--brand-primary` and `--brand-accent` are the values
`docs/design/brand.md` documents, and the dark status hues are untouched; what
the fix wave added is the light-theme half that was missing, never a change to
the dark palette. Focus rings are untouched. So are routes, i18n keys, form
field names and component props.
