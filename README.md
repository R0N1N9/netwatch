# netrunner

> A cyberpunk network console. Futuristic, experimental, AI-native security tooling —
> inspired by NetWatch and the netrunners of Cyberpunk 2077.

`netrunner` is the **R&D sandbox** counterpart to a grounded, enterprise suite
(*Ronin Suite*). Where that suite is modern-day production security, `netrunner`
is where the bleeding-edge gets prototyped: AI-driven detection, agentic
response, and hardware/RF/3D fusion — behind a sleek Cyberpunk-2077-style HUD.

One binary, multiple **decks**:

| Deck | Stance | Status |
|------|--------|--------|
| **NetWatch** | Defensive — watch the segment, guard the wall | ✅ working (GUI + TUI) |
| **Netrunner** | Offensive — authorized recon & lab testing | 🟡 recon seed (scope-locked) |
| **RUView** | Spatial — fuse ESP32-S3 sensors into a live 3D map | 🔭 scaffold |

> **Scope & ethics.** `netrunner` is for **defense, your own networks, lab
> environments, CTFs, and engagements you are authorized to perform**. The
> offensive deck is scope-locked to private/lab address space and refuses public
> targets. Don't point it at things you don't own or have permission to test.

![NetWatch deck](docs/netwatch.png)
![Kill Chain deck](docs/killchain.png)

---

## Quick start (Linux)

Requires Go ≥ 1.24. The capture layer is **pure-Go AF_PACKET — no libpcap, no
cgo** — so it builds to a single static binary; the GUI is embedded inside it.

```bash
make build
```

```bash
# Preview the console with sample data — no privileges needed:
./bin/netrunner gui --demo
```

```bash
# Live watch (needs raw sockets + nftables). Grant caps instead of running as root:
sudo setcap cap_net_raw,cap_net_admin+eip ./bin/netrunner
./bin/netrunner gui
```

The GUI serves on `http://127.0.0.1:7979` and auto-opens your browser.
`cap_net_raw` covers capture/injection; `cap_net_admin` powers BLACKWALL (nftables).

### Commands

```
netrunner gui     # Cyberpunk web console (NetWatch deck)  — primary UI
netrunner watch   # lightweight terminal deck (TUI fallback)
netrunner sweep   # one-shot device discovery, printed to the terminal
```

`gui` flags: `--demo`, `--addr 127.0.0.1:7979`, `--auto 20s` (sweep interval,
`0` disables), `--no-open`.

---

## The NetWatch deck (built)

Defensive situational awareness for your LAN, rendered as a neon command deck.

- **SEGMENT MAP / SWEEP** — active ARP discovery of every host, with MAC-vendor
  (OUI) fingerprinting, reverse-DNS, and `you` / `gateway` / trusted tagging.
- **ICE** (Intrusion Countermeasures) — detection engine. Flags **new hosts
  joining** and **gateway-MAC changes** (classic ARP-spoof / MITM signal).
  Passive packet-level detectors (live ARP-spoof, port-scan, rogue DHCP) next.
- **COPILOT** — an AI daemon you can ask *"what's weird right now?"*. Today it
  reasons over live state locally; the `copilot.Copilot` interface lets an LLM
  backend (local model or API) drop in behind the same call.
- **BLACKWALL** — one-click **quarantine** of a selected host via nftables
  (behind an explicit confirm). Release just as easily. Gateway and self are
  protected from accidental lockout.
- **VITALS** — live counts: hosts, new, threats, trusted.

## The Netrunner deck (recon seed)

Authorized, lab-scoped recon. **PROBE** runs a concurrent TCP-connect scan of a
target across common + IoT ports — and **refuses any non-private address**.

## The Kill Chain deck (purple team)

The whole Lockheed-Martin kill chain as an interactive, MITRE ATT&CK-mapped
console — RECON → WEAPONIZATION → DELIVERY → EXPLOITATION → INSTALLATION → C2 →
ACTIONS ON OBJECTIVES. Each stage pairs **RED** (authorized/lab offense) with
**BLUE** (live defense), and an OPS CONSOLE streams results.

The red side is deliberately **assessment + safe test artifacts** (scope-locked
scanning, the benign EICAR string for AV/EDR validation, planning briefs) — *not*
weaponized exploit/C2/malware generators. The blue side does the real work, with
read-only host introspection:

- **Listening-socket audit** — your C2/backdoor surface (`/proc/net/tcp`, or `ss`).
- **Persistence audit** — systemd units, cron, shell rc, autostart, rc.local (T1543/T1547/T1053).
- **Established-peer hunt** — live outbound/inbound TCP peers, **public egress flagged** (C2/exfil, T1041/T1048).
- **EICAR validation** — confirm your scanner actually fires.

## Kiroshi HUD

The optic layer: a boot/uplink sequence, **scramble-decode** text reveals, a live
**telemetry ticker**, a **target-lock reticle** that snaps onto the selected host
with a threat-score readout, plus scanlines, CRT flicker and glitch.

## RUView (scaffold)

Placeholder for the ESP32-S3 sensor mesh → live 3D spatial map (WebGL/three.js
in the same shell).

---

## Architecture

```
main.go                      entrypoint
internal/
  model/      core types (Device, Alert, Severity)
  netif/      interface / subnet / gateway discovery (parses /proc/net/route)
  capture/    AF_PACKET raw socket — pure Go (capture_linux.go), stub elsewhere
  oui/        MAC-prefix -> vendor (built-in table + optional IEEE CSV load)
  sweep/      SWEEP: ARP request injection + reply collection
  ice/        ICE engine: sweep-diff detectors (ARP-spoof, new-device)
  blackwall/  nftables quarantine (atomic rebuild of one dedicated table)
  copilot/    AI daemon — heuristic now, LLM-pluggable via the Copilot interface
  offensive/  Netrunner recon — scope-locked TCP-connect scan
  audit/      blue-team host introspection (listeners, persistence, peers, EICAR)
  server/     HTTP server: serves the GUI + SSE live stream + action endpoints
  webui/      embedded Cyberpunk web console (assets/ baked in with go:embed)
  tui/        terminal deck (Bubble Tea) — fallback
  theme/      cyberpunk palette + banner
  cli/        cobra command tree (netrunner gui | watch | sweep)
```

The capture + model + ICE engine is deck-agnostic: the GUI, the TUI, and the
offensive deck all consume the same core. The front-end (plain HTML/CSS/JS, no
build step) can also be dropped into a native Wails window later for a packaged
desktop app.

---

## Roadmap

**NetWatch — deepen the defense**
- [x] Cyberpunk GUI shell + live SSE stream.
- [x] BLACKWALL nftables quarantine.
- [x] Copilot (local heuristic) + pluggable LLM interface.
- [ ] Passive live capture: continuous flow table (top talkers, per-host bandwidth).
- [ ] Live ICE detectors: real-time ARP-spoof, port-scan-against-us, rogue DHCP, SSDP/mDNS snooping.
- [ ] Persistent asset DB (pure-Go SQLite): trusted/unknown, first/last seen, nicknames.
- [ ] GeoIP + reputation enrichment; auto-pcap on alert; desktop notifications.

**AI-native layer**
- [x] Copilot Q&A over live state.
- [ ] LLM backend (local model / API) behind the Copilot interface.
- [ ] Agentic response: reason about a confirmed threat → propose/execute BLACKWALL (human-in-the-loop).

**Netrunner deck — offensive (authorized)**
- [x] Scope-locked TCP-connect port/service scan.
- [ ] CIDR ranges, service banners, OS hints.
- [ ] Traffic masking / VPN (WireGuard) integration.

**RUView — spatial**
- [ ] ESP32-S3 sensor mesh ingestion (Wi-Fi/BLE/UWB) → live 3D map.

---

## Status

The NetWatch deck is working end-to-end: SWEEP, sweep-diff ICE, the live
Cyberpunk GUI (SSE), BLACKWALL quarantine, the Copilot daemon, and a
scope-locked recon probe. Everything builds to one static, cgo-free binary.
