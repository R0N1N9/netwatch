# netrunner

> A cyberpunk network console. Futuristic, experimental, AI-native security tooling —
> inspired by NetWatch and the netrunners of Cyberpunk 2077.

`netrunner` is the **R&D sandbox** counterpart to a grounded, enterprise suite
(*Ronin Suite*). Where that suite is modern-day production security, `netrunner`
is where the bleeding-edge and the slightly-crazy gets prototyped: AI-driven
detection, agentic response, and hardware/RF/3D fusion.

It is organized as one binary with multiple **decks**:

| Deck | Stance | Status |
|------|--------|--------|
| **NetWatch** | Defensive — watch the segment, guard the wall | ✅ MVP working |
| **Netrunner** | Offensive — authorized recon & lab testing | ⏳ planned |
| **RUView** | Spatial — fuse ESP32-S3 sensors into a live 3D map | 🔭 future |

> **Scope & ethics.** `netrunner` is for **defense, your own networks, lab
> environments, CTFs, and engagements you are authorized to perform**. The
> offensive deck is built as an authorized-use toolkit with scope/target
> confirmation baked in. Don't point it at things you don't own or have written
> permission to test.

---

## The NetWatch deck (built)

Defensive situational awareness for your LAN, rendered as a neon command deck.

- **SWEEP** — active ARP discovery of every host on your subnet, with MAC-vendor
  (OUI) fingerprinting, reverse-DNS, and `you` / `gateway` role tagging.
- **ICE** (Intrusion Countermeasures) — the detection engine. Today it diffs
  successive sweeps to flag **new hosts joining** and **gateway-MAC changes**
  (a classic ARP-spoof / man-in-the-middle signal). Passive packet-level
  detectors (live ARP-spoof, port-scan, rogue DHCP) plug in next.
- **The Deck** — a full-screen TUI: live device map, ICE alert feed, and stats,
  updating on a configurable auto-sweep.

### Commands

```bash
netrunner watch          # launch the live NetWatch deck (TUI)
netrunner watch --auto 30s
netrunner sweep          # one-shot discovery, printed to the terminal
netrunner sweep -d 5s --no-dns
```

### Keys (deck)

`s` sweep · `↑/↓` select · `t` trust/untrust host · `q` quit

---

## Build & run (Linux)

Requires Go ≥ 1.24. The capture layer is **pure-Go AF_PACKET — no libpcap, no
cgo** — so it builds to a single static binary with no system dependencies.

```bash
make build            # -> ./bin/netrunner (static)
# or:
CGO_ENABLED=0 go build -o bin/netrunner .
```

Raw sockets need privilege. Instead of running as root, grant the binary just
the capabilities it needs:

```bash
sudo setcap cap_net_raw,cap_net_admin+eip ./bin/netrunner
./bin/netrunner watch
```

`cap_net_raw` covers capture/injection; `cap_net_admin` is for the future
**BLACKWALL** quarantine (nftables). Drop it if you don't want blocking.

### Other laptops

The logic is portable; only the capture backend is OS-specific. macOS (BPF) and
Windows (Npcap) backends slot in behind the same `capture.Handle` API — the
non-Linux build currently compiles with a stub so the rest of the tool runs.

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
  theme/      cyberpunk lipgloss palette + banner
  tui/        the NetWatch deck (Bubble Tea)
  cli/        cobra command tree (netrunner ...)
```

Design principle: the capture + model layer is deck-agnostic. NetWatch,
the offensive deck, and RUView all consume the same engine.

---

## Roadmap

**NetWatch — deepen the defense**
- [ ] Passive live capture mode: continuous flow table (top talkers, per-host bandwidth).
- [ ] Live ICE detectors: ARP-spoof (gratuitous ARP watch), port-scan against us, rogue DHCP, SSDP/mDNS snooping.
- [ ] Persistent asset DB (SQLite, pure-Go `modernc.org/sqlite`): trusted vs unknown, first/last seen, nicknames.
- [ ] **BLACKWALL**: nftables quarantine of a host behind explicit confirmation.
- [ ] GeoIP + reputation enrichment of remote destinations (offline DB + optional feed).
- [ ] Black-box: auto-capture a pcap when an alert fires.
- [ ] Desktop/notification + logfile alerting.

**AI-native layer (the netrunner twist)**
- [ ] Copilot: natural-language queries over network state ("what's new/weird right now?").
- [ ] LLM threat triage: summarize + rank alerts, suggest response.
- [ ] Agentic response: reason about a confirmed threat and propose/execute BLACKWALL (human-in-the-loop).
- [ ] Pluggable model backend (local or API), no hard dependency.

**Netrunner deck — offensive (authorized)**
- [ ] Ping/port sweep + service fingerprint of targets you own.
- [ ] IoT lab probing (your own devices).
- [ ] Traffic masking / VPN (WireGuard) integration.
- [ ] Target allowlist + scope confirmation guardrails.

**RUView — spatial**
- [ ] ESP32-S3 sensor mesh ingestion (Wi-Fi/BLE/UWB).
- [ ] Live 3D map of hosts + physical/RF space.

---

## Status

MVP of the NetWatch deck is working: SWEEP, sweep-diff ICE, and the live TUI.
Everything builds to one static, cgo-free binary.
