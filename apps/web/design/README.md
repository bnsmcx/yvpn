# Design explorations

Source for the visual directions explored for the web app. Each `*.dc.html` is one
artboard — a self-contained static mockup — and `canvas.json` lays them out as rows,
one row per direction.

These are mockups, not shipping code: nothing here is loaded by the app. They live in
the repo so the exploration survives, since the canvas they were published to is the
only other copy.

| Direction | Artboards |
|---|---|
| **Phosphor** — terminal/CRT, green | `Main`, `PhosphorSignIn`, `PhosphorCreate`, plus `PhosphorLight` (the light-mode inversion) |
| **TVA** — the same bones in amber, beige console housing | `TVASignIn`, `TVAConsole`, `TVAScreen` |
| **Substrate** — modern infra dashboard | `SubstrateSignIn`, `SubstrateDash`, `SubstrateCreate` |
| **Meridian** — nodes plotted by real longitude and latitude | `MeridianSignIn`, `MeridianDash`, `MeridianCreate` |

`TVASignIn` reflects the current sign-in flow: one credentials field holding both
tokens, with the two-token helper collapsed beneath it, and no proxy URL. The other
sign-in boards predate that change and still show the older three-field form.

## Constraints these were drawn under

Whichever direction ships gets reimplemented as hand-written CSS in `../index.html`,
so every board sticks to what that file can do: system font stacks only, icons as
inline SVG or CSS shapes, no external resources of any kind, and no emoji.

## Viewing and editing

Each file is static markup — open one directly in a browser to see it, ignoring the
`./support.js` line in `<head>` (it is a hook for the canvas runtime, not a real
script). The assembled canvas bundle is a 2.5 MB generated artifact and is
deliberately not committed.
