# Experimental feature registry

NetWatch is a testing ground for unusual security and sensing ideas. This file
is the running list of every experimental feature: what it does, how far it has
actually been proven, and what it needs. **It grows with the project** — see
[Keeping this list](#keeping-this-list) at the bottom.

How to use each feature is in the [guide](GUIDE.md).

## Reading the status column

| Status | Meaning |
|---|---|
| ✅ **Proven** | Ran on real data in the development environment. |
| 🧪 **Synthetic** | The logic is tested, but only against simulated, crafted or sample input. It has not met real traffic, a real room or real hardware yet. |
| 🔧 **Unrun** | Written but never executed (for example firmware with no board to flash). |

A feature can be Proven in the interface and Synthetic underneath; the notes
say which part is which.

---

## 1. Sensing physical space (SONAR)

Everything in this section depends on ESP32-S3 nodes. Until boards are flashed
and reporting, all of it has only seen the built-in simulator. The node
firmware has now run on one real board (2026-11-19, the owner's report): flashed
from the console, it joined Wi-Fi and its link appeared in SONAR. Whether the
sensing built on top of that data behaves as it does in the simulator is
still unconfirmed, so the rows below stay 🧪 until it has been watched
working.

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **Wi-Fi CSI presence sensing** | Detects a body moving through the path between the access point and a node, from disturbance in the per-subcarrier signal. | 🧪 | Unit tests on simulated frames; frames sent over UDP by `sonar-sim` and read back; one real node now streams frames and its link shows in SONAR (owner's report), but motion detection on that real data has not been confirmed yet | ESP32-S3 nodes. Senses which *link* is disturbed, not coordinates. |
| **3D / 2D presence field** | Room view with nodes, the sensed zone around each link, sweep rings; 3D by default, 2D for arranging, automatic 2D fallback without WebGL. | ✅ view · 🧪 data | Browser run in both modes and with WebGL disabled | — |
| **Movement estimate and trail** | A marker and trail at a score-weighted blend of the disturbed links. | 🧪 | Unit test; browser | A rough guide. It jumps between link midpoints. |
| **Braindance** | Records the presence field (rolling ~50 min buffer, manual clips, save-last-minute, automatic clips while armed) and replays a clip in the room view with seek and speed, plus the network's host and alert counts from that moment. | 🧪 | Recorder and clip-library tests (incl. persistence and bad ids); clip saved and replayed in a browser | Clips are a record of when people moved — keep them private. Per-device network state is not recorded yet. |
| **Breathing detection** | On a link with no movement, finds a steady 9–30 cycles/min ripple shared across subcarriers and reports `RESP n/min`. | 🧪 | Unit test: simulated 15/min breather read as 15, empty link silent | Likely to need tuning on real signals. |
| **Wi-Fi ranging (FTM)** | Nodes measure their distance to the access point by signal time-of-flight; the map places each node at that range. | 🧪 | Unit test on simulated ranges | Access point must support 802.11mc; many consumer routers do not. Gives distance, not direction. |
| **Hosts on the room map** | Nodes report the signal strength of devices they overhear; NetWatch fits a position and matches it to a network host by MAC. | 🧪 | Unit test: within 0.6 m on consistent synthetic readings; three demo hosts placed | Real indoor error will be metres. Three or more nodes for a real fix. |
| **Floor plan** | Draw walls in 2D; shown as translucent panels in 3D. Stored with the room layout. | ✅ | Drawn by pointer and by code in a browser; persisted and reloaded from the server | Image import is not built. |
| **Virtual tripwires** | Named lines on the map; while armed, movement passing from one side to the other raises a critical alert. | 🧪 | Crossing unit test; alert raised in the demo scene | Uses the movement estimate, so it inherits its roughness. |
| **Activity recognition** | Labels each link empty / still / minor / moving / walking / fall?. | 🧪 | Unit tests on simulated sequences | Rules on signal turbulence, not a trained model. "fall?" is a burst followed by stillness — sitting down looks the same — so it only alerts while armed. |
| **People counting** | Rough head count from active links grouped by direction. | 🧪 | Unit test; demo | Very approximate; two people on one link count as one. |
| **Respiration log** | One reading a minute per link (breathing rate, movement), 24 h kept, charted per node. | 🧪 | Demo data charted; log persistence | Only as good as breathing detection. |
| **ESP32-S3 node firmware** | Streams CSI amplitudes, FTM range and overheard-device signal strength to NetWatch. | 🔧 | Nothing yet | Written against ESP-IDF v5 without a toolchain or board. Expect fixes on first build. |

## 2. Network intelligence

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **Passive OS fingerprinting** | Guesses a host's OS from its TCP SYN (TTL, window, MSS) without probing it. | 🧪 | Unit tests on known combinations | Coarse families only. |
| **JA3 TLS fingerprinting** | Hashes the TLS ClientHello and extracts the server name. | 🧪 | Unit test against a hand-built ClientHello | JA4 not implemented. |
| **Generated-domain and DNS-tunnel detection** | Scores query names by entropy and character mix; flags long encoded labels. | 🧪 | Unit tests | Heuristic; expect false positives on CDNs. |
| **Beaconing detection** | Flags regular, low-jitter callbacks to one destination. | 🧪 | Unit tests on interval series | Needs minutes of traffic to see a pattern. |
| **Behavioural anomaly model** | Per-device running baselines; scores a host acting unlike itself and names the feature responsible. | 🧪 | Unit tests | Statistical baseline, not an isolation forest. |
| **External-connection globe (GRID)** | 3D globe of this network's public peers; opaque (no server name) egress in red. | ✅ view · 🧪 data | Rendered in a browser | **Positions are not real geography** — placement is deterministic per address until a GeoIP database is added. |
| **Process lineage** | Names the program on this machine behind each connection. | ✅ | Run against the development machine's real connections | Other users' sockets need root. Linux only. |
| **Time scrubber** | Replays the console's recent state; ticks mark alerts. | ✅ | Browser run on demo history | In memory only; lost on restart. |
| **BLACKWALL quarantine** | Blocks a host at the firewall (nftables), with release. | ✅ | Rule created and removed on the development machine | Blocks traffic to *this* machine, not the whole LAN. Needs `cap_net_admin`. |
| **Presence key** | Trust and quarantine are refused while a chosen device is off the network. | 🧪 | Server test of allow/refuse paths; browser | Matches by MAC address, which can be forged. A convenience lock. |

## 3. AI and interaction

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **Local-model Copilot** | Answers questions about the network with a model served by Ollama on your machine; falls back to built-in rules. | 🧪 | Tests against a stand-in Ollama server | Never run against a real model here. |
| **Command line in the Copilot box** | Typed commands (select, trust, quarantine with confirm, rewind, record…); anything else is a question. | ✅ | Browser run of each command | — |
| **Voice control** | Speaks the same commands and reads replies back. | 🧪 | Command parser only | Speech recognition was never exercised (no microphone). In Chrome it sends audio to Google. |
| **Spatial audio alerts** | Each alert plays from a direction (HRTF). | 🧪 | Direction maths and playback path in a browser | Nobody has listened to it yet. Headphones needed. |
| **Scanner HUD and neural-link visuals** | 2077-style skin, target lock, point-cloud brain that fires while the Copilot works, EEG trace, glitch cuts. | ✅ | Browser | Cosmetic. |
| **AR optic overlay** | Host tags floating over the camera feed. | 🧪 | Rendered with a fake camera | WebXR untested on a device; tags are not anchored to real positions. |

## 4. The machine NetWatch runs on

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **Live RAM and CPU gauges** | Header bars show how much of the host's memory and processor is still free: full when idle, draining as it is used, red when under 15% is left. | ✅ | Compared with `free` | Linux `/proc`. |
| **USB watch** | Alerts on a newly attached USB device; calls out keyboards. | 🧪 | Tests on a fake device tree | No real plug-in event has been observed. |
| **Keystroke-injection check** | Flags a new keyboard typing faster than a person can. | 🧪 rate rule · 🔧 input reading | Unit test of the rate rule | Reading `/dev/input` has never run; needs permission to read it. |
| **Daily surface diff** | Compares listening ports, start-up entries, hosts and USB devices with the previous day. | ✅ sources · 🧪 diff | Sources read on the development machine; diff and storage unit-tested | Needs a previous day's snapshot to be useful. |
| **Detector fire drill** | Stages synthetic events against fresh copies of eleven detectors, at start-up and daily, and reports any that stay silent. | ✅ | Runs in every build (11/11) | Checks detection logic, not capture or sensors. |
| **Ultrasonic proximity lock** | Plays a 19 kHz tone, listens for the Doppler smear of nearby movement, and locks the screen after a minute of none. | 🧪 | Signal maths and lock logic with synthetic input | Never heard a real speaker and microphone. Many laptops barely reach 19 kHz; a perfectly still person looks like an empty chair; pets may hear it. |

## 5. Purple-team decks

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **Kill Chain deck** | The seven Lockheed-Martin stages, each with red (authorised/lab) and blue (defence) actions and an ops console. | ✅ | Browser; blue audits run on the development machine | Red side is assessment, safe test artifacts and planning briefs only. |
| **ATT&CK deck** | The same structure over the fourteen MITRE ATT&CK Enterprise tactics. | ✅ | Browser | As above. |
| **Scope-locked probe** | TCP connect scan that refuses any public address. | ✅ | Loopback scanned; public address refused | Single address only. |

---

## 6. Module decks

Eight decks added together: deception, an investigating agent, a radio
inventory, traffic foresight, typing rhythm, learned device behaviour, a
cryptography inventory and a tamper-evident log. Where a deck needs live
traffic it has the same blind spot as the rest of NetWatch: it sees what
reaches this machine's network interface, which on ordinary Wi-Fi is this
machine's own traffic plus broadcasts — not every conversation between other
devices.

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **DECOY · honeyports** | Opens a TCP port with no service behind it. Any connection raises a critical alert with the caller's address and the first bytes it sent. | ✅ | A live (non-demo) run on the development machine: a real listener, a real connection over loopback, the alert, and the port reopened after a restart | Only ever touched from this machine — no outside scanner has hit one yet. Listens on every interface; ports under 1024 need root. Reads at most 256 bytes and never answers. |
| **DECOY · canary files** | Plants a tempting file in your home folder and watches it; opening it raises the alarm. | ✅ | The same live run: a real file, a real open, the alert, and the file picked up again after a restart | Linux only. Home folder only. It knows the file was opened, not by which program — backup and indexing tools will trip it. Blind for the first 0.3 s after planting. The file holds made-up credentials that work nowhere. |
| **DECOY · canary names** | A host name nothing real uses; any DNS lookup of it is flagged with who asked. | 🧪 | Unit test feeding lookups | Needs live capture. Cannot see lookups made over encrypted DNS, or by devices whose traffic never reaches this machine. |
| **DECOY · drill** | Touches every port and file decoy from this machine and reports it as a drill, to prove the alarms work. | ✅ | Handler test: a drill touch is reported as informational, an unannounced touch as critical | Does not exercise canary names. |
| **CORTEX · case builder** | For an alert, gathers what is known (who the device is, its behaviour, other alerts, decoys touched, steps outside its profile, local programs talking to it), weighs each piece, argues for and against a threat, and gives a verdict with one proposed action. | 🧪 | Unit tests; sample alerts in a browser | The weights were chosen by hand and have not been checked against real incidents. The verdict is a sum of those weights; "confidence" is not a calibrated probability. |
| **CORTEX · approve to act** | The proposal does nothing until you press APPROVE; approving or dismissing is written to the LEDGER. It never proposes quarantining the gateway or this machine. | 🧪 | Browser: approving quarantined the sample host in demo mode, where the firewall is not touched | The quarantine itself is the existing BLACKWALL path. |
| **CORTEX · local-model summary** | With Ollama present, asks the local model to put a finished case into three sentences. The model gets the case as data and cannot change the verdict. | 🔧 | Never run — no local model in the development environment | — |
| **SPECTRUM · radio inventory** | Lists every Wi-Fi and Bluetooth radio the sensor nodes hear, with signal history, and flags a Wi-Fi radio that stays close for five minutes without ever joining your network. You can mark a radio as yours. | 🧪 | Unit tests; a simulated air scene in a browser | Needs ESP32-S3 nodes, which hear only their own Wi-Fi channel. Phones that randomise their address look like new radios. The "close" cut-off (−70 dBm) and the five minutes are guesses. |
| **SPECTRUM · tracker tags** | Flags a Bluetooth tag advertising in the Find My style that stays in range. | 🧪 | Crafted advertisements | **Nothing sends Bluetooth data yet** — the node firmware does not scan Bluetooth; only the ingest format exists. Such tags change address every few minutes to a day, which can reset the timer. |
| **SPECTRUM · look-alike access point** | After you approve the access points that are yours, any other one announcing your network name is flagged. | 🧪 | Unit tests; simulated air | You must approve your own access points first; a mesh node added later is flagged until approved. Same channel only. |
| **SPECTRUM · disconnect flood** | Counts deauthentication and disassociation frames and alerts on a burst. | 🧪 | Unit tests; simulated air | The threshold (20 in 10 s) is a guess. Networks using protected management frames ignore such floods anyway. |
| **Node firmware: access points and disconnect counts** | The ESP32-S3 node reports beacons it hears and the number of disconnect frames each second. | 🔧 | Compiles in CI; never run on a board | — |
| **ORACLE · reach map** | For a chosen host, draws everything it has been seen talking to inside and outside your network, and states how many of those conversations a quarantine on this machine would actually stop. | 🧪 | Unit tests; sample conversations in a browser | Live use needs capture, and is limited to traffic this machine can see (above). |
| **ORACLE · what-if rules** | Tests a rule (`port 23`, `unnamed`, `fanout > 3`, `bytes > 1000000`, `src …`, `dst …`) against the conversations observed so far and shows what it would have caught. | 🧪 | Unit tests; browser | Six rule forms. Works on conversation totals, not on a timeline. It creates no firewall rule. |
| **PULSE · typing rhythm** | Learns how long you hold keys and the gaps between them, then scores each 50 keystrokes against that; two bad windows in a row raise a critical alert. | 🧪 | Unit tests; a real browser pressing and releasing keys on a script — the same rhythm scored 87–95% over two runs, a different one 0% and raised the alert | **Never tried on a human typist.** The thresholds are guesses. Only measures while the console has keyboard focus, so it does not protect the rest of the desktop. |
| **PULSE · lock on mismatch** | Optionally locks the screen when the rhythm stops matching. | 🔧 | The lock branch has not been triggered from PULSE (demo mode never locks) | Uses the same lock commands as `lock`. |
| **WARD · learned profiles** | Learns, per device, which destinations, ports and hours are normal and its usual conversation size; afterwards flags a new destination, a new port, an hour it is normally silent, or three times its usual volume. Each can be accepted as normal. | 🧪 | Unit tests; a sample day in a browser | Needs capture, with the same visibility limit. Enforces only after 300 observations and 24 hours. Outside addresses with no name are grouped by /16 and names by their last two labels, which is crude (it treats all of `co.uk` as one). |
| **WARD · fingerprint change** | Flags a device whose TLS fingerprint changes — new firmware, or different software speaking from that address. | 🧪 | Unit test | A device running several TLS libraries will flip back and forth. |
| **QUANTUM · TLS inventory** | Handshakes with TLS services on your own network and records protocol version, cipher, key type and size, days to certificate expiry, and whether the server accepts a hybrid post-quantum key exchange (X25519MLKEM768). | 🧪 | Real handshakes against a Go TLS server on loopback, including the post-quantum check | Has not been pointed at a real router, camera or NAS. Private addresses only; ports 443, 8443, 993, 8883. |
| **QUANTUM · SSH inventory** | Reads an SSH server's banner and the algorithm list it sends in the clear; flags no post-quantum key exchange and SHA-1. | 🧪 | A scripted server speaking the real wire format | Not yet run against a real OpenSSH or dropbear. Port 22 only. |
| **LEDGER · hash chain** | Every alert, every operator action and any note you add is appended to a file where each entry carries the hash of the one before. VERIFY re-reads the file and checks every link. | ✅ | Live run: entries written to `ledger.jsonl`, reloaded after a restart, then the file was edited by hand and VERIFY reported the break at the right entry | Tamper-*evident*, not tamper-proof: someone able to rewrite the whole file can rebuild the chain. Copy the head hash somewhere else if that matters. In demo mode it is memory only. |
| **LEDGER · incident report** | Writes a plain report for the last hour, day or everything: counts, the most involved sources, a timeline and the integrity result. | 🧪 | Sample alerts in a browser; unit test | A fixed template with no model behind it; it can only restate the chain. Not linked to the time scrubber. |
| **Console · cross-site request guard** | Every state-changing request must come from the console's own page; a request another website makes your browser send is refused. | ✅ | Handler test with same-origin, cross-site and origin-less requests | Requests with no `Origin` header (curl, the sensor nodes) are allowed, as before. There is still no login: anyone who can reach the port can use the console. |

---

## 7. Experience and reflexes

Features you see, hear, feel or walk through, and the automation that ties the
room to the console.

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **NETDIVE** | The network as a city in 3D: a tower per host (height follows traffic, colour follows trust and threat), flows as moving lights, outside destinations floating above, and a wireframe wall around anything quarantined. The time scrubber moves the city through time. | 🧪 | A browser with software WebGL: 5 towers, 9 flows, the quarantined host walled; scrubbing back rebuilt it with 3 towers | Sample conversations only. The towers follow the scrubber but the flows are always the ones observed up to now. Needs WebGL; there is no 2D fallback. |
| **Incident rehearsal (DOJO)** | Plays a scripted, labelled incident into the live console and scores you on doing the right thing quickly. Two scenarios. | ✅ | A live (non-demo) run: alerts injected, the synthetic host quarantined "for pretend" and no firewall rule requested (a test with demo mode off checks that); in a browser a full run through the real controls scored 100 | The drill's host is made up and its alerts are marked `[REHEARSAL]`. Scoring windows are my guesses at "quick". The drill's alerts do go into the LEDGER, labelled. |
| **While you were out** | Marks you away after ten minutes with nobody sensed, and on your return replays what happened: counts, who joined or left, and a time-lapse through the scrubber's history. | 🧪 | Unit and handler tests; a recap of the sample half-hour replayed in a browser | **Away detection has never been triggered by a real absence.** It needs SONAR nodes, or operator watch switched on. The time-lapse only reaches as far back as the in-memory history. |
| **Live commentary** | Speaks each new alert as one sentence and glitches the display on a critical. | 🧪 | The sentences were generated and queued in a browser | Nobody has listened to it: the test browser has no speech engine. Uses the browser's built-in voice. |
| **Network sonification** | A continuous soundscape: a drone that opens up with traffic, a pulse that quickens, a dissonant tone that grows with unopened critical alerts, and a bell per alert. | 🧪 | The audio graph ran in a browser and the state-to-sound mapping was checked at quiet, normal and storm | **Never heard by a person**, so whether it is pleasant or useful is unknown. |
| **Scanner mode** | Hold `Q`: the console dims and a list of hosts fans out, each with its verdict, reach, decoys touched and steps outside its profile, plus actions (lock on, investigate, reach, trust, quarantine with a confirm). | ✅ | Browser, driven by key and mouse; a quarantine issued from it | It presents what the other modules know, so it is only as proven as they are. |
| **Operator fatigue meter** | Estimates how sharp you are from time at the console, the hour, drift in your typing rhythm and critical alerts left unopened. When you are "spent", CORTEX asks twice before a quarantine. | 🧪 | Unit and handler tests | The weights are guesses and have never been compared with how tired anyone actually was. Off by default; nothing is stored. |
| **Layout: compact mode, expand, pop-out windows** | The console adapts to short screens, any panel can fill the window, and any deck or panel can be opened in its own window for a second monitor. | ✅ | A browser at 1366×768: all SONAR nodes visible, the side panel scrolls, a panel expanded and restored, a panel and a deck popped out into live windows; the 1600×1000 layout unchanged | Each popped-out window is a whole console under the hood, so it polls the server separately. Positions and sizes of windows are not remembered. |
| **Alert acknowledgement** | Click an alert (or say `ack`) to mark it seen. Unopened alerts drive the threat light, the fatigue meter and the soundscape. | ✅ | Handler test; browser | Kept in memory only. |
| **HUNT mode** | Pick a radio in SPECTRUM and carry a sensor node towards it: a signal meter, warmer/colder, and Geiger-counter ticks that speed up as you close in. | 🧪 | A simulated approach and retreat in a browser (−58 → −35 → −76 dBm, with the trend following) | **Never done with a real node in a real room.** Needs a node you can carry on a battery. Signal strength bounces off walls, so expect it to mislead at times. |
| **X-RAY** | Draws what SONAR senses — the presence estimate, still-and-breathing links, nodes, located hosts — where it is, from a point and heading you set, over the camera feed. | 🧪 | The projection was checked against hand-worked angles; the AR deck showed and hid tags as the heading changed | On a laptop you set position and heading by hand. Positions are only as good as the node layout you arranged. The phone path (compass and camera) has never run on a phone. |
| **Phone companion** | A read-only page for a phone on your network: each kind of alert vibrates in its own pattern, plus the X-RAY view. Served over HTTPS on its own port, behind a pairing code. | 🧪 | A live listener: page 200, data 403 without the code, console API 404 through it, lock-out after repeated wrong codes; an emulated phone browser received an alert and "vibrated" (stubbed) | **Never opened on a real phone.** iPhones do not let web pages vibrate (it flashes and beeps instead). The certificate is self-signed, so the phone shows a warning once. |
| **Room scenes (REFLEX)** | "When this happens, do that": a trigger (tripwire, movement, alert, everyone leaves, someone returns, a gesture) and up to six actions (save a clip, speak, alert, note, flash the light, acknowledge, arm, disarm, switch deck, lock). | 🧪 | Handler tests and a browser: scenes built with the form, fired by a sample alert, a simulated tripwire and the TEST button | Every trigger so far came from the simulator, a test button or a sample alert. The lock action has never fired. A scene cannot fire more than once every five seconds. |
| **Gesture control** | Two or three short waves through a link, followed by stillness, count as a gesture that can trigger a scene. Walking through is ignored. | 🧪 | Scripted on/off sequences (two waves, three, one, walking, fidgeting) | **Never tried with real signals**, and the timings are guesses built around the engine's 1.6-second window. Only listens while a scene uses a gesture. Reports about three seconds after the last wave. |
| **Ambient threat light** | Sends the room's state as a colour, once a second, to an ESP32 with an LED strip: green quiet, amber warnings, red breathing for unopened criticals, flashing for a fresh one, blue while armed. | 🧪 | A live run: the datagram arrived at a local listener; a public address was refused | Private addresses only. One datagram a second, nothing comes back. |
| **Threat light firmware** | The ESP32 side: listens for the datagram and drives a WS2812 strip. | 🔧 | Compiles in CI (ESP-IDF v5.3.2, `led_strip` v2); not confirmed working on a board | Drives a strip on GPIO8 (30 LEDs) and mirrors the colour on the board's own RGB light, so a bare board can serve. |
| **Who brought that device** | When a new host joins, the last minute of the room is saved as a braindance clip and attached to the alert (▶ ROOM), and CORTEX is told whether anyone was moving. | 🧪 | Handler test; the sample alert in a browser opened its clip | Needs sensor nodes. It shows that someone moved, not who, and not that they carried the device. |
| **Talking decoy** | A port that imitates a small device's login: refuses the first attempt, lets the second "in", answers a handful of commands from a fixed table and records the conversation with a plain-language summary. | ✅ | A live run: a real session over loopback, transcript and summary recorded, the port reopened after a restart, and the typed password found nowhere on disk | **Nothing typed is ever run** — input is only matched against a table. Passwords are not kept, only their length. Only ever visited by my own scripted client, never by a real intruder or bot. 90 seconds and 16 commands per visit. |
| **Node forge: flash a board from the console** | Plug in an ESP32-S3, press ADD A NODE in SONAR, type your Wi-Fi details: NetWatch writes the firmware and the board's settings with esptool. Afterwards the board only needs power. | ✅ | One real ESP32-S3 flashed from the console by the owner (2026-11-19): the flash completed and the node then appeared in SONAR. Before that: the images passed the real esptool's checks and a stand-in tool confirmed the files and clean-up | One board, one laptop, one network so far. Linux only (it finds boards through `/sys`). The Wi-Fi password is stored on the board in plain form. |
| **Flashing feedback** | The button you press becomes the progress bar — filling green, solid green when the node is online, red where it stopped if the flash failed, amber if the board never reports. CANCEL stops a flash midway. The panel also shows the stage (connect, write, verify, restart, online), a progress bar, and a green ONLINE banner with a chime when the new node's first frames arrive — or says after a minute that it has not reported. | 🧪 | The progress parser is tested against esptool 4 and 5 output; the online and stalled cases ran with a scripted node over loopback; the panel ran in a browser in demo mode | The percentages come from reading esptool's text, which could change. Never seen with a real board. |
| **Node status light** | The board's RGB LED blinks blue while joining Wi-Fi, shows green once streaming, blinks red when it cannot join and purple with no settings. | 🔧 | Compiles in CI; never run | Assumes the LED is on GPIO48 or GPIO38 (Espressif DevKitC-1). Other boards may stay dark; a red-green-blue flash at power-up shows whether it is wired. On the owner's first board only a steady red light was seen, which is most likely its power light — so the status light is unconfirmed there. |
| **Firmware built in CI** | Both firmware projects are compiled by a GitHub Actions workflow and the images committed where the binary embeds them. | ✅ | Two CI runs: the first found two errors, the second built both projects | Compiling is not running: neither image has booted on a board. |
| **Board settings partition** | The firmware reads Wi-Fi details, console address and node name from a small flash partition written at flash time. | ✅ | The real board joined the owner's Wi-Fi and reported to NetWatch using settings written this way | The password is stored on the board in plain form. |
| **Console discovery** | A node asks the network where NetWatch is every 15 s and follows it if the address changes. | 🧪 | NetWatch's side (answering a node's hello, hearing its frames, listening without a flag) ran over loopback UDP with a scripted node | On the real board it has not been exercised: the address has not changed yet. |
| **Talking decoy · model improvisation** | With a local model, commands outside the table get an improvised reply. | 🔧 | Never run — no local model here | The visitor's text is passed to the model as data. |

---

## Ideas not built yet

Candidates, roughly grouped. Move a line into a table above when it lands.

**Room and radio**
- RF fingerprinting — tell devices apart by transmitter imperfections, even with a cloned MAC
- Node-to-node ranging — real positions instead of bearings you set by hand
- Full-scene braindance — record per-device network state alongside the presence field
- Floor plan image import
- Bluetooth scanning in the node firmware (SPECTRUM can ingest it; nothing sends it yet) and a Bluetooth presence key

**Network**
- Real GeoIP database for the globe
- JA4 fingerprints
- Encrypted-traffic classification from packet sizes and timing
- Persistent asset database

**Deception**
- Canary credentials — a made-up login that alerts when someone tries to use it (files and names are built)
- Self-shuffling decoy hosts

**AI**
- Natural-language detection rules, compiled for review before they go live
- Graph learning over hosts and flows
- Counterfactual replay over time — ORACLE tests a rule against conversation totals; replaying recorded traffic minute by minute is not built
- Model-written incident narrative, and a report tied to a scrubber span (the template report is built)
- Daily digest and routine learning

**Host**
- Moving-target mode — rotate this machine's MAC and hostname

---

## Feature log

| Date | Added |
|---|---|
| 2026-11-17 | ARP sweep and ICE, web console, BLACKWALL, Copilot (rules), scope-locked probe, HUD, Kill Chain deck |
| 2026-11-19 | Passive intelligence (OS, JA3, DNS, beaconing, anomaly), GRID globe, AR overlay, scanner skin and neural-link visuals, ATT&CK deck |
| 2026-11-19 | Local-model Copilot, time scrubber, CSI presence engine, SONAR deck (2D then 3D), commands, voice, spatial audio, live RAM/CPU bars |
| 2026-11-19 | Braindance, breathing detection, Wi-Fi ranging, process lineage, presence key |
| 2026-11-19 | Hosts on the map, floor plan, tripwires, activity, people count, respiration log, USB watch, surface diff, fire drill, proximity lock |
| 2026-11-19 | Module decks: DECOY, CORTEX, SPECTRUM, ORACLE, PULSE, WARD, QUANTUM, LEDGER; two-row tab bar; cross-site request guard; demo mode no longer calls the firewall on quarantine |
| 2026-11-19 | NETDIVE, incident rehearsal (DOJO), while-you-were-out, live commentary, sonification, scanner mode, operator fatigue meter, alert acknowledgement |
| 2026-11-19 | REFLEX scenes, gesture detection, ambient threat light (+ firmware), HUNT mode, X-RAY, phone companion, who-brought-that-device, talking decoy |
| 2026-11-19 | Node forge (flash boards from the console), firmware built in CI and embedded, settings partition, console discovery, automatic listening |
| 2026-11-19 | Flashing feedback (stages, progress, ONLINE confirmation) and the node's status light |
| 2026-11-19 | **First real hardware:** one ESP32-S3 flashed from the console, joined Wi-Fi and appeared in SONAR. Cancel button for flashing; clearer message when NetWatch is already running |
| 2026-11-19 | Small-screen layout: compact mode, scrolling columns, expand any panel, pop decks and panels out into their own windows |

---

## Keeping this list

Every experimental feature gets a row here in the same change that adds it.

1. Put it in the right section with all five columns filled in.
2. Be exact about status. If it has only seen a simulator or a unit test, it is
   🧪, however good it looks in the demo. If it has never run, it is 🔧.
3. Say what proved it, and name its limits plainly.
4. If it was in *Ideas not built yet*, remove it from there.
5. Add a line to the feature log.
6. When a 🧪 feature meets real data or hardware, update the row — including
   when it turns out not to work as hoped.
