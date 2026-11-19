# NetWatch

> An experimental network and spatial-sensing console with a Cyberpunk 2077
> scanner HUD. A testing ground for security ideas that are still unusual.

NetWatch is the R&D counterpart to a grounded, enterprise suite (*Ronin
Suite*). It watches your local network, senses movement in your space from
Wi-Fi signals, and lets you replay both.

![Watchtower](docs/screens/watchtower.jpg)

> **Scope.** NetWatch is for your own networks and spaces, lab environments,
> and work you are authorised to do. Anything that touches other machines is
> limited to private address space.

## Documentation

| | |
|---|---|
| **[Guide](docs/GUIDE.md)** | Install, run, every deck, commands, hardware, files and privacy. |
| **[Experimental feature registry](docs/EXPERIMENTAL.md)** | The growing list of every experimental feature, how far each has been proven, and ideas not built yet. |
| **[Architecture](docs/ARCHITECTURE.md)** | Packages, HTTP API, background loops, how to add a feature. |

## Quick start (Linux)

Requires Go 1.24+. Builds to one static binary with the console embedded.

```bash
make build
```

```bash
./bin/netwatch gui --demo
```

That runs with sample data and touches nothing. To watch your real network:

```bash
sudo setcap cap_net_raw,cap_net_admin+eip ./bin/netwatch
```

```bash
./bin/netwatch gui
```

The console opens in a window of its own if Chrome, Edge, Chromium or Brave is
installed, otherwise in your browser.

**Update and restart** — one command stops the running copy, pulls, rebuilds,
restores the capture rights and starts it again:

```bash
./update.sh
```

The same commands, ready to copy, are behind **⟳ UPDATE** in the console's top
bar. Details: [guide](docs/GUIDE.md#update-and-restart).

## Decks

| Deck | What it is |
|---|---|
| **Watchtower** | Hosts on your network, intrusion alerts, a Copilot you can question or command, and BLACKWALL to quarantine a host. |
| **Breach** | A connect scan that refuses any public address. |
| **Kill Chain** / **ATT&CK** | Purple-team consoles over the Lockheed-Martin stages and the MITRE tactics, with audits you can run on this machine. |
| **GRID** | A globe of the public addresses your network talks to, with the local program behind each. |
| **NETDIVE** | The network as a 3D city you fly through: a tower per host, flows as moving lights, a wall around anything quarantined. |
| **AR** | Host tags over your camera feed. |
| **MOBILE** | A QR code that links your phone to a page of its own: alerts by vibration, a compass that points to devices, X-RAY through its camera, and clip playback. It can look but not change anything. |
| **BRAINDANCE** | Replays on a tilted model of the room: what SONAR sensed (room clips) and what a paired phone was facing, if its owner shared that. |
| **SONAR** | Presence sensing from ESP32-S3 Wi-Fi nodes: a 3D room view, tripwires, located hosts, breathing, and braindance recordings you can replay. |

The second row of tabs holds the modules:

| Module | What it is |
|---|---|
| **DECOY** | Honeyports, canary files and canary host names: things with no legitimate use, so any touch is an alarm. |
| **CORTEX** | Investigates an alert: gathers the evidence, argues for and against a threat, and proposes one action that waits for your approval. |
| **SPECTRUM** | Every Wi-Fi and Bluetooth radio the sensor nodes hear; flags strangers that linger, tracker tags, look-alike access points and disconnect floods. |
| **ORACLE** | What each host really talks to, what a quarantine would stop, and a what-if box for testing a rule against observed traffic. |
| **PULSE** | Learns your typing rhythm (timing only, never the keys) and alerts when someone else is at the keyboard. |
| **WARD** | Learns each device's normal destinations, ports and hours, then flags steps outside them. |
| **QUANTUM** | Inventories the TLS and SSH on your own network and shows what is exposed to record-now, decrypt-later. |
| **LEDGER** | A hash-chained record of every alert and action, with a verify button and an incident report. |
| **DOJO** | Incident rehearsals that play a labelled, synthetic incident into the console and score your response; and an operator fatigue meter. |
| **REFLEX** | Scenes ("when this happens, do that"), wave gestures, spoken commentary, a soundscape of the network, an LED threat light and a phone companion. |

And across every deck: hold `Q` for **scanner mode**, and when you come back after being away the console replays **what happened while you were out**.

![CORTEX](docs/screens/cortex.jpg)

![SONAR](docs/screens/sonar-3d.jpg)

## How proven is it?

Much of NetWatch runs ahead of what has been tested on real hardware. The
[registry](docs/EXPERIMENTAL.md) marks every feature as proven on real data,
tested only on simulated data, or written but never run. In short:

- The network console, firewall quarantine, host audits, process lineage and
  the interface have run on real data. So have the honeyports, canary files
  and the LEDGER's hash chain, on this machine only.
- Everything on SONAR has only seen the built-in simulator, and the ESP32-S3
  firmware compiles (it is built on GitHub and embedded in the binary, so the
  console can flash a board for you) and has run on one real board: it joined
  Wi-Fi and showed up in SONAR. What SONAR makes of that real signal has not
  been confirmed yet.
- The passive traffic analyzers, the local-model Copilot, voice, spatial audio,
  USB watch and the proximity lock are tested with synthetic input only.
- The incident rehearsal and the talking decoy have run live on this machine.
  NETDIVE, HUNT, X-RAY, scenes, gestures, the phone companion, commentary and
  the soundscape have only run on simulated input — nobody has yet heard the
  audio, waved at a real sensor or opened the companion on a real phone.
- CORTEX, SPECTRUM, ORACLE, PULSE, WARD and QUANTUM have only seen sample or
  scripted input: PULSE has never been tried on a human typist, and nothing
  yet sends SPECTRUM any Bluetooth data.

## Privacy

Nothing leaves your machine unless you turn on voice in Chrome (speech goes to
Google) or point the Copilot at an Ollama host that is not yours. SONAR
recordings and the respiration log describe when people moved and breathed in
your space; they are stored locally, and the [guide](docs/GUIDE.md#files-and-privacy)
says where. PULSE keeps four numbers describing your typing rhythm and never
records which keys you press. The phone companion is off unless you start it;
a phone shares where it is pointing only while its owner switches that on, as
numbers kept in memory for ten minutes, never pictures or sound;
and a talking decoy records what a visitor types but never their password.
