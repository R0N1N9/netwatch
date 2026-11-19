# Architecture

NetWatch is one Go binary. It captures and analyses on the machine it runs on,
serves an embedded web console on localhost, and pushes state to that console
over Server-Sent Events. There is no cgo, no libpcap and no front-end build
step.

```
                 ┌────────────────────────── netwatch gui ──────────────────────────┐
 LAN  ──ARP──▶   │ sweep ─▶ ice (alerts)            passive monitor ─▶ flow table   │
 frames ───────▶ │                                   └▶ OS / JA3 / DNS / beacon     │
 ESP32 nodes ──▶ │ sonar engine ─▶ presence, breathing, radios ─▶ braindance buffer │
 /proc, /sys ──▶ │ audit · sysmon · hostwatch (USB, surface)        drill           │
                 │                       server ── SSE + JSON API ──▶ web console   │
                 └──────────────────────────────────────────────────────────────────┘
```

## Packages (`internal/`)

| Package | Responsibility |
|---|---|
| `cli` | Command tree: `gui`, `watch`, `sweep`, `sonar-sim`. `gui` also opens the console: a Chromium-family browser's application window with its own profile if one is installed, otherwise the default browser. |
| `server` | HTTP server, SSE stream, every API endpoint, background loops, demo data. `server.go` holds the core; `room.go` holds the room, tripwires, sleep log, USB, surface and drill wiring; `modules.go` feeds and exposes the eight module decks; `senses.go` holds rehearsal, recap, operator watch, acknowledgement and scanner data; `reflex.go` holds scenes, gestures, the light, talking decoys and room clips; `companion.go` is the phone listener; `dance.go` serves clips ready to draw and takes and replays phone views; `forge.go` flashes boards and starts the node listener at run time; `about.go` tells the UPDATE card where this copy lives. |
| `webui` | The embedded console (`assets/`: `index.html`, `app.css`, `skin2077.css`, `app.js`, `senses.js`, `reflex.js`, `optic.js`, `forge.js`, `layout.js`, `xray.js`, `dance.js`, `companion.html`, `companion.css`, vendored three.js and fonts). |
| `model` | Shared types: `Device`, `Alert`, `Severity`. |
| `netif` | Finds the interface, subnet and gateway. |
| `capture` | AF_PACKET raw socket in pure Go; a stub on other systems. |
| `sweep` | ARP discovery of hosts. |
| `oui` | MAC prefix to vendor. |
| `ice` | Alerts from differences between sweeps (new host, gateway MAC change). |
| `passive` | Live capture, flow table, and the analyzers: OS fingerprint, JA3, DNS scoring, beaconing. |
| `anomaly` | Per-device running baselines and anomaly score. |
| `geo` | Places an address on the globe (deterministic, not real geography). |
| `blackwall` | Quarantine via one nftables table, rebuilt atomically. |
| `copilot` | The Copilot interface, built-in rules, and the Ollama backend. |
| `offensive` | The scope-locked connect scan. |
| `audit` | Read-only host inspection: listeners, start-up entries, connections, process lineage, EICAR string. |
| `sonar` | CSI presence engine, breathing, activity, ranging, radio location, room geometry, sleep log, simulator. |
| `braindance` | Rolling buffer of the presence field and the clip library. A clip carries the node layout and the walls as drawn when it was cut. |
| `optic` | Phone views: a capped, in-memory trail per phone of where it stood and faced, and the geometry that works out which known things were inside its field of view. Only numbers are accepted from the phone; labels come from the console's own map. |
| `hostwatch` | USB watch, typing-rate rule, daily surface snapshots and diff, screen lock. |
| `sysmon` | CPU and memory of this machine. |
| `drill` | The detector fire drill. |
| `decoy` | Honeyports, canary files (inotify) and canary names; calls back on every touch. |
| `cortex` | Turns the evidence around one alert into weighted reasoning steps, for/against arguments, a verdict and one proposed action. Pure; it never acts. |
| `spectrum` | Inventory of Wi-Fi and Bluetooth radios and access points from node reports; lingering radios, tracker tags, look-alike access points, disconnect floods. Does not import `sonar` (`sonar` imports it for the frame fields). |
| `oracle` | A host's reach over observed conversations, and what-if rules evaluated against them. |
| `ward` | Per-device learned profile (destinations, ports, hours, volume, TLS fingerprint) and deviations from it. |
| `pulse` | Typing-rhythm profile from key hold and gap timings (median and spread), window scoring, mismatch callback. |
| `quantum` | TLS and SSH inventory of private addresses, including whether a hybrid post-quantum key exchange is accepted. |
| `ledger` | Append-only hash chain of alerts, actions and notes; verification and the incident report text. |
| `rehearsal` | Scripted incident scenarios, a session that injects steps on a clock, credits responses and scores them. |
| `recap` | Presence tracker (away / back) and the summary of a span of absence. |
| `fatigue` | Operator sharpness from session length, hour, typing drift and unopened alerts. |
| `scenes` | Trigger → actions rules with matching, arming and cooldowns. It decides which scenes fire; `server` carries out the actions. |
| `light` | Picks the threat light's colour for a state and sends it as a UDP datagram to a private address. |
| `forge` | Finds ESP32 boards on USB (`/sys`), holds the embedded firmware images (`forge/firmware/*.bin`, built by CI), renders the settings block, runs esptool and reads its output into a stage and percentage. |
| `persona` | The talking decoy: a scripted login and shell that records a visit and summarises it. Input is matched against a table, never executed. |
| `tui`, `theme` | The terminal view and shared colours. |

`firmware/esp32s3-csi` is the ESP-IDF project for the sensor nodes;
`firmware/esp32-light` is the one for the LED threat light. `sonar/gesture.go`
holds the wave-gesture detector.

`.github/workflows/firmware.yml` builds both firmware projects with ESP-IDF
whenever `firmware/**` changes, merges each into one image, and commits the
images to `internal/forge/firmware/`, where `go:embed` picks them up. After
changing firmware, wait for that workflow and `git pull` before `make build`.
The settings partition (`nwcfg`, offset `0x190000` in `partitions.csv`) must
match `forge.ConfigOffset`; a test checks the embedded image.

## HTTP API

All endpoints are served on the console's address (localhost by default).
Reads are `GET`; actions are `POST` with a JSON body.

| Endpoint | Purpose |
|---|---|
| `/api/stream` | SSE stream of the full state snapshot (hosts, alerts, meta). |
| `/api/sweep` | Sweep now. |
| `/api/trust` | Mark a host trusted or not. |
| `/api/blackwall` | Quarantine or release a host. |
| `/api/gate` | Set or clear the presence key. |
| `/api/scan` | Scope-locked connect scan. |
| `/api/copilot` | Ask the Copilot. |
| `/api/globe` | Public peers for the globe. |
| `/api/lineage` | Connections with their owning process. |
| `/api/sys` | CPU and memory. |
| `/api/history`, `/api/history/at` | Time-scrubber index and snapshots. |
| `/api/sonar` | Nodes, layout, walls, tripwires, located hosts, head count. |
| `/api/sonar/ingest` | Accept sensor frames over HTTP (UDP is the normal path). |
| `/api/sonar/arm`, `/layout`, `/map`, `/sleep` | Arm, save node positions, save walls and tripwires, read the respiration log. |
| `/api/braindance`, `/save`, `/rec`, `/clip`, `/delete` | List, save, record, load and delete clips. |
| `/api/braindance/scene?id=` | A clip ready to draw: samples, layout, walls (the clip's own, or today's for older clips) and the movement estimate for each sample. |
| `/api/optic` · `/api/optic/trail?phone=&since=` | Phone views: `GET` lists them, `POST` `forget` drops one (or `*`); the trail is one phone's frames plus the room as it is now. |
| `/api/about` | Build stamp, the binary's path, the checkout it was built in and the data directory, for the UPDATE card. |
| `/api/usb`, `/api/surface`, `/api/drill`, `/api/lock` | USB list, surface diff, fire drill, lock the screen. |
| `/api/kc/*` | The same audits as text lines for the Kill Chain and ATT&CK ops consoles. |
| `/api/decoy` | `GET` planted decoys and trips; `POST` `port-open`, `port-close`, `file-plant`, `file-remove`, `name-add`, `name-remove`, `test`. |
| `/api/cortex` | `GET` the triaged queue; `POST` one alert (`ts`, `source`, `kind`, optional `narrate`) for its full case. |
| `/api/spectrum` | `GET` radios, access points and the disconnect count; `POST` approve access points (`ssids`), clear approvals, or mark a radio as mine. |
| `/api/oracle`, `/api/oracle/whatif` | A host's reach (`?host=`); evaluate a rule. |
| `/api/pulse` | `GET` state; `POST` key timings (`strokes`), `reset`, or `lock`. |
| `/api/ward` | `GET` profiles and deviations; `POST` `accept` or `relearn`. |
| `/api/quantum` | `GET` findings; `POST` starts an inventory. |
| `/api/ledger`, `/note`, `/report` | The chain (`?recheck=1` re-reads the file), add a note, the incident report (`?mins=`). |
| `/api/rehearsal` | `GET` scenarios, state and past scores; `POST` `start` or `stop`. |
| `/api/recap` | `GET` the last recap; `POST` builds one for the last `mins` minutes. |
| `/api/operator` | `GET` the fatigue reading; `POST` `on`, or `ping` on operator input. |
| `/api/ack` | Acknowledge one alert, the newest, or all. |
| `/api/scanner` | Every host with its verdict, reach, trips and deviations, for scanner mode. |
| `/api/reflex` | `GET` scenes, recent firings, light and companion state; `POST` `put`, `remove`, `fire`, `gesture`, `lightAddr`, `lightTest`. |
| `/api/mobile`, `/api/mobile/qr.png` | The phone link's state; `POST` `on` or `newCode`; the link as a QR code (console only). |
| `/api/forge` | `GET` boards on USB, whether esptool and the images are there, the current job; `POST` flashes a board (`flash`, `kind`, `ssid`, `pass`, `name`, `settingsOnly`), cancels the one in progress (`cancel`), starts listening (`listen`), or sets where esptool is (`toolPath`). |
| `/api/oracle?all=1` · `/api/spectrum?hunt=<id>` | Every conversation (for NETDIVE); per-node readings of one radio (for HUNT). |

The phone companion is a **separate listener** (`--companion`, or switched on
from the MOBILE deck at run time and remembered; HTTPS, self-signed): `/` (the page), `/companion.css`, `/fonts/*` (the console's typeface),
`/xray.js`, `/dance.js`, `/stream?code=` (alerts only), `/sonar?code=`,
`/finder?code=` (located hosts, the node that hears each loudest, and the
asking phone's own position if the nodes hear it; also at `/api/finder` on the
console), `/dance?code=` and `/dance/clip?code=&id=` (the clip library and one
clip ready to draw). Nothing under `/api/` is served there. The single thing
a phone can send is `POST /view?code=`: one frame of its own view (position,
heading, tilt, open tab, FIND target), at most 600 bytes, at most ten a
second, into `optic`. A target that is not a host the console has placed is
dropped, and what was "in view" is computed by the console.

Every `POST` handled by `decode()` is refused with 403 if the browser marks it
cross-site or its `Origin` is not the console's own address.

## Background loops

| Loop | Interval | Does |
|---|---|---|
| Sweep | `--auto` | ARP sweep, ICE diff, broadcast. |
| Passive monitor | per packet, plus 12 s | Analyzers; anomaly and beacon scans. |
| Record | 200 ms | Samples the presence field into the braindance buffer; tripwire checks and automatic clips while armed. |
| Minute | 60 s | Appends to the respiration log. |
| Daily | start-up + 24 h | Surface diff and fire drill, each reported as an alert. |
| USB watch | 2 s | New-device alerts. |
| WARD | 30 s | Feeds each known device's active conversations into its profile; saves `modules.json` every fifth minute. Not started in demo mode. |
| Honeyport accept, canary-file watch | event-driven | One goroutine per decoy; each touch becomes an alert (at most one a minute per source and decoy). |
| Demo radio scene | 1 s | Demo mode only: feeds SPECTRUM a simulated set of radios. |
| Senses | 500 ms | Injects due rehearsal steps and ends the drill; feeds presence to the away tracker; every 10 s re-assesses the operator. |
| Reflex | 150 ms | Feeds the gesture detector (only while a scene uses gestures); once a second computes the light's colour, sends it if an address is set, and fires away/return scenes. |
| Talking decoy | event-driven | One goroutine per visit, at most four at once per port. |
| System monitor | 1 s | CPU sample. |

## Adding a feature

1. Put the logic in its own package or file with a pure, testable core, and
   write the test first where you can. Hardware- or traffic-dependent features
   need a simulator or fixture so they can be exercised without the real thing.
2. Wire it into `server` (an endpoint, and a loop if it runs on its own). Give
   demo mode believable sample data, and make sure demo mode has no side
   effects on the machine.
3. Add it to the console in `assets/`, and to the command grammar in `app.js`
   if it makes sense to say or type.
4. Add a row to [EXPERIMENTAL.md](EXPERIMENTAL.md) with an honest status, and a
   section or line in [GUIDE.md](GUIDE.md).
5. Verify in a browser before calling it done, and say what was and was not
   tested.

## Conventions

- Anything observed on the network (host names, vendors, server names) is
  untrusted. Escape it before inserting it into the page, and pass it to a
  model only as data.
- Features that act on other machines are limited to private address space.
  The red side of the purple-team decks stays at assessment, safe test
  artifacts and planning briefs.
- Sensing features that record people default to off or disarmed, and store
  only on the local machine.
- Every alert goes through `emitAlert` (or calls `logAlert`), and every
  operator action calls `logAction`, so the LEDGER stays complete. A new
  action endpoint should do the same.
- Module state that must survive a restart goes in `modules.json` through
  `saveModules` / `loadModules`; demo mode never reads or writes it.
- Every new alert passes through `afterAlert` (scenes, the room clip). The
  browser side has matching hooks: `ALERT_HOOKS`, `RENDER_HOOKS`,
  `EXTRA_COMMANDS` and `MOD_DECKS`; `senses.js` and `reflex.js` add features
  through those rather than by editing `app.js`.
- A popped-out window is the same page with `?solo=1&deck=<deck>` and
  optionally `&panel=<n>` (the nth `.panel`/`.tpanel` of that deck, in page
  order); `layout.js` hides everything else. New panels get the expand and
  pop-out buttons automatically.
- The server can ask open consoles to speak or switch deck through the `cue`
  field of the snapshot; a scene is the only thing that sets it.
- Anything a phone or other device on the network may reach goes on the
  companion listener, behind the pairing code — never on the console's own
  address. It is read-only except for `/view`, which takes numbers only,
  stores them in memory and changes nothing else.
- `dance.js` draws the room for both the console's BRAINDANCE deck and the
  phone's BD tab; labels are drawn with `fillText`, never as HTML.
- A rehearsal must never reach a real host or the firewall: actions aimed at
  its host are intercepted in `rehearsalBlackwall`.
