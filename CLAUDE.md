# NetWatch — working notes for Claude

NetWatch is an experimental, defensive network and spatial-sensing console: one
static Go binary with an embedded web UI. It is a testing ground for novel
ideas, so features are often ahead of what has been proven. Being exact about
that gap is part of the job.

## Commands

- Build: `make build` (or `CGO_ENABLED=0 go build -o bin/netwatch .`)
- Test: `go test ./...` · Vet: `go vet ./...` · Format: `gofmt -l internal/` must print nothing
- Run with sample data: `./bin/netwatch gui --demo --no-open`
- The web assets are embedded, so rebuild after editing anything in `internal/webui/assets/`.
- Check every script in `internal/webui/assets/` (`app.js`, `senses.js`, `reflex.js`, `forge.js`, `xray.js`) with `node --check` before building.

## Documentation is part of every feature

When you add or change an experimental feature, in the same change:

1. **`docs/EXPERIMENTAL.md`** — add or update its row (what it does, status,
   what proved it, limits), remove it from "Ideas not built yet" if it was
   there, and add a line to the feature log. New ideas the user likes but does
   not build yet go into "Ideas not built yet".
2. **`docs/GUIDE.md`** — how to use it, including any new command, flag,
   environment variable or stored file.
3. **`docs/ARCHITECTURE.md`** — new packages, endpoints or loops.

Status in the registry must be honest: ✅ only if it ran on real data, 🧪 if it
has only seen a simulator, unit test or sample data, 🔧 if it has never run.
The demo looking good does not make something ✅.

## How features are built here

- Pure, testable core first (see `internal/sonar`, `internal/passive`); give
  hardware- or traffic-dependent features a simulator or fixture.
- Demo mode must have no side effects on the machine (no firewall changes, no
  screen lock, nothing written to the data directory).
- Verify UI work in a real browser (Playwright is available) and report what
  was and was not tested. WebGL screenshots are slow here because rendering is
  in software; use `deviceScaleFactor: 1` and long timeouts for them.
- Network-observed strings are untrusted: escape them (`esc()` in `app.js`)
  before inserting HTML, and pass them to a model only as data.

## Scope

- Defensive and sensing features for the user's own network and space.
- Anything that touches other machines stays limited to private address space.
- The red side of the Kill Chain and ATT&CK decks is assessment, safe test
  artifacts (EICAR) and planning briefs. Do not add exploit, implant,
  credential-theft or evasion tooling.
- Features that record people (SONAR, braindance, respiration log) stay local,
  default to off or disarmed, and their docs say plainly what is recorded.

## Layout

See `docs/ARCHITECTURE.md`. Front-end: `index.html`, `app.css` (base),
`skin2077.css` (the look), `app.js` (the decks and modules), `senses.js` and
`reflex.js` (later features, hooked in through `EXTRA_COMMANDS`, `ALERT_HOOKS`,
`RENDER_HOOKS` and `MOD_DECKS`), `xray.js` (shared with `companion.html`, the
phone page served on its own listener). Firmware for the ESP32-S3
nodes is in `firmware/esp32s3-csi`, and for the LED threat light in
`firmware/esp32-light`. Both compile in CI (`.github/workflows/firmware.yml`,
which commits the images to `internal/forge/firmware/` for embedding — pull
after it runs); neither has run on a board.
