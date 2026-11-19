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
and reporting, all of it has only seen the built-in simulator.

| Feature | What it does | Status | Proven by | Needs / limits |
|---|---|---|---|---|
| **Wi-Fi CSI presence sensing** | Detects a body moving through the path between the access point and a node, from disturbance in the per-subcarrier signal. | 🧪 | Unit tests on simulated frames; frames sent over UDP by `sonar-sim` and read back | ESP32-S3 nodes. Senses which *link* is disturbed, not coordinates. |
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

## Ideas not built yet

Candidates, roughly grouped. Move a line into a table above when it lands.

**Room and radio**
- Rogue-transmitter hunting — radios present in the room that never join the network
- RF fingerprinting — tell devices apart by transmitter imperfections, even with a cloned MAC
- Node-to-node ranging — real positions instead of bearings you set by hand
- "Who brought that device" — attach the braindance clip to a new-host alert
- Full-scene braindance — record per-device network state alongside the presence field
- Floor plan image import
- Gesture control — wave through a link to switch decks or dismiss an alert
- Ambient threat light — an ESP32 with an LED strip showing network state
- Bluetooth presence key and tracker sweep

**Network**
- Real GeoIP database for the globe
- JA4 fingerprints
- Encrypted-traffic classification from packet sizes and timing
- Reach map — which hosts each device actually talks to, and what a quarantine would cut off
- Habit model — flag a device active at an hour it normally sleeps
- Firmware drift watch — alert when an IoT device's fingerprint changes
- Post-quantum readiness scan of local TLS/SSH endpoints
- Persistent asset database

**Deception**
- Canary tokens — decoy files, credentials and DNS names that alert when touched
- Model-driven honeypot — a decoy service that answers plausibly and summarises the session
- Self-shuffling decoy hosts

**AI**
- Natural-language detection rules, compiled for review before they go live
- Graph learning over hosts and flows
- Counterfactual replay — rerun recorded traffic against a changed rule
- Incident report writer over a scrubber span
- Daily digest and routine learning
- Agentic response with a human approving each action

**Host**
- Moving-target mode — rotate this machine's MAC and hostname
- Tamper-evident log of alerts and actions
- Network sonification

---

## Feature log

| Date | Added |
|---|---|
| 2026-11-17 | ARP sweep and ICE, web console, BLACKWALL, Copilot (rules), scope-locked probe, HUD, Kill Chain deck |
| 2026-11-19 | Passive intelligence (OS, JA3, DNS, beaconing, anomaly), GRID globe, AR overlay, scanner skin and neural-link visuals, ATT&CK deck |
| 2026-11-19 | Local-model Copilot, time scrubber, CSI presence engine, SONAR deck (2D then 3D), commands, voice, spatial audio, live RAM/CPU bars |
| 2026-11-19 | Braindance, breathing detection, Wi-Fi ranging, process lineage, presence key |
| 2026-11-19 | Hosts on the map, floor plan, tripwires, activity, people count, respiration log, USB watch, surface diff, fire drill, proximity lock |

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
