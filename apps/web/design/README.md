# Design

Source for the look the web app ships with. Each `*.dc.html` is one artboard — a
self-contained static mockup — and `canvas.json` arranges them on a canvas.

Nothing here is loaded by the app: `../index.html` is hand-written and is the real
thing. These boards are where the design was worked out, kept so the reasoning
behind it survives.

| Board | What it is |
|---|---|
| `SignIn.dc.html` | Sign-in: one credentials field holding both tokens, with the two-token helper collapsed beneath it |
| `Main.dc.html` | The dashboard — the design the app is built from |
| `ScreenOnly.dc.html` | The variant that did not ship: same palette, no console housing |

There is no create-dialog board. That flow was designed directly in the app, so
`../index.html` is its source of truth.

## The direction

A beige console housing with the working area set into it as an amber-phosphor
screen, after the retro-futurist workstations the look is named for.

| | | |
|---|---|---|
| `#FFB000` | amber phosphor | brand, buttons, selection, live counts |
| `#17100A` | warm near-black | screen ground — black with brown in it, never neutral |
| `#EFE2C6` | cream | primary text |
| `#A08B64` / `#97845F` | tan | secondary data, labels |
| `#1B1309` / `#C2AE83` | housing, unlit / lit | the console itself — near-black under dark, beige under light |
| `#9DA84E` | olive | healthy exit node |
| `#D9822B` | ochre | joining / route not approved |
| `#B23A26` | brick rust | offline, negative balance |

Healthy nodes read **olive rather than amber** on purpose. A strict monochrome
amber screen would be more faithful, but it collapses the four node states into one
hue, and amber is already carrying every button and the selected row. Olive and
rust are period-correct indicator-lamp colours, so the states stay legible without
leaving the world.

The two themes light the same console rather than recolouring one thing: under dark
the housing recedes to near-black with its stamped labels inverted to tan, so there
is no pale frame around a lit screen; under light the cabinet is beige and the
screen becomes a pale readout, with amber darkened to `#8A5200` to hold contrast on
cream. The boards here show the dark state. See `../README.md` for how the
**DISPLAY** switch drives it.

## What was chosen over

Three other directions were explored and deleted once this one shipped — Phosphor
(the same console idea in green CRT phosphor, plus a light-mode inversion),
Substrate (a restrained Linear/Vercel-style dashboard) and Meridian (nodes plotted
at their real longitude and latitude). They are in the git history if they are ever
wanted again.

## Constraints these were drawn under

Whatever ships gets reimplemented as hand-written CSS in `../index.html`, so every
board sticks to what that file can do: system font stacks only, icons as inline SVG
or CSS shapes, no external resources of any kind, and no emoji.

## Viewing

Each file is static markup — open one directly in a browser, ignoring the
`./support.js` line in `<head>` (a hook for the canvas runtime, not a real script).
