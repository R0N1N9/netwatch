"use strict";

// ---------- state ----------
let STATE = { meta: {}, devices: [], alerts: [], stats: {} };
let selectedIP = null;
let live = false;
let tickerRefresh = null, tickerPrimed = false;
let liveState = null, replay = false;

const $ = (s) => document.querySelector(s);
const esc = (v) => String(v == null ? "" : v).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const el = (tag, cls, txt) => { const e = document.createElement(tag); if (cls) e.className = cls; if (txt != null) e.textContent = txt; return e; };

// ---------- clock ----------
setInterval(() => { $("#clock").textContent = new Date().toLocaleTimeString("en-GB"); }, 1000);

// ---------- deck tabs ----------
document.querySelectorAll(".tab").forEach((t) => {
  t.addEventListener("click", () => selectDeck(t.dataset.deck));
});
function selectDeck(name) {
  if (name !== currentDeck()) { document.body.classList.add("glitch"); setTimeout(() => document.body.classList.remove("glitch"), 270); }
  document.querySelectorAll(".tab").forEach((t) => t.classList.toggle("active", t.dataset.deck === name));
  document.querySelectorAll(".deck").forEach((d) => (d.hidden = d.dataset.deck !== name));
  if (name === "netwatch" && selectedIP) lockReticle(selectedIP);
  else $("#reticle").hidden = true;
  if (name === "grid") setTimeout(() => { initGlobe(); if (globe && globe.resize) globe.resize(); loadGlobe(); }, 40);
  if (name === "ar") renderARTags();
  if (name === "sonar") setTimeout(() => { if (!sonarMode) setSonarMode(sonarPref()); sonarResize(); }, 40);
}
function currentDeck() { return document.querySelector(".tab.active").dataset.deck; }

// ---------- API ----------
async function post(path, body) {
  try {
    const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
    if (!r.ok) throw new Error("HTTP " + r.status);
    return await r.json();
  } catch (e) {
    status("offline: " + e.message);
    return null;
  }
}
function status(msg) { $("#statusLine").textContent = msg; }
async function get(path) {
  try { const r = await fetch(path); if (!r.ok) throw new Error("HTTP " + r.status); return await r.json(); } catch (e) { return null; }
}

// ---------- live stream (SSE) ----------
function connect() {
  const es = new EventSource("/api/stream");
  es.onmessage = (ev) => {
    let snap; try { snap = JSON.parse(ev.data); } catch (_) { return; }
    live = true; liveState = snap;
    onNewAlerts(snap);
    if (!replay) { STATE = snap; render(); }
  };
  es.onerror = () => { if (!live) { loadMock(); } };
}

// ---------- render ----------
function render() {
  const m = STATE.meta || {}, s = STATE.stats || {};
  $("#segMeta").textContent = `${m.iface || "--"} · ${m.subnet || "--"}`;
  $("#statusText").textContent = replay ? "REPLAY" : m.sweeping ? "SCANNING" : (m.watching ? "WATCH ACTIVE" : "IDLE");
  if (m.copilot) $("#cpBackend").textContent = m.copilot;
  const dot = $("#statusDot"); dot.className = "dot" + (threats() > 0 ? " crit" : m.sweeping ? " warn" : "");

  $("#hostCount").textContent = STATE.devices.length;
  $("#newCount").textContent = s.new || 0;
  $("#vHosts").textContent = STATE.devices.length;
  $("#vNew").textContent = s.new || 0;
  $("#vThreats").textContent = threats();
  $("#vTrust").textContent = STATE.devices.filter((d) => d.trusted).length;
  $("#vThreatCard").classList.toggle("hot", threats() > 0);

  renderDevices();
  renderAlerts();
  renderHud();
  if (selectedIP) fillTarget(STATE.devices.find((x) => x.ip === selectedIP));
  if (currentDeck() === "ar") renderARTags();
  if (!tickerPrimed && tickerRefresh && STATE.devices.length) { tickerPrimed = true; tickerRefresh(); }
}

function threats() { return (STATE.alerts || []).filter((a) => a.severity === "CRITICAL").length; }

function threatOf(d) { return Math.max(riskScore(d), Math.round(d.anomaly || 0)); }
function statusOf(d) {
  if (d.quarantined) return ["LOCKED", "lock"];
  if (d.self) return ["SELF", ""];
  if (d.gateway) return ["GATEWAY", ""];
  if (d.trusted) return ["TRUSTED", ""];
  return ["UNVERIFIED", "bad"];
}
function renderDevices() {
  const list = $("#deviceList"); list.innerHTML = "";
  if (!STATE.devices.length) { list.appendChild(mk("empty", "scanning segment…")); return; }
  STATE.devices.forEach((d) => {
    const t = threatOf(d), st = statusOf(d);
    const row = el("div", "dev qh");
    if (d.ip === selectedIP) row.classList.add("sel");
    if (d.quarantined) row.classList.add("quar");
    if (t >= 50) row.classList.add("hot");
    const main = el("div", "qh-main");
    main.appendChild(el("div", "qh-name", (d.hostname || d.vendor || "unknown host").toUpperCase()));
    const sub = el("div", "qh-sub");
    sub.appendChild(el("span", "badge " + st[1], st[0]));
    if (d.beacon) sub.appendChild(el("span", "badge bad", "BEACON"));
    sub.appendChild(el("span", "ip", d.ip));
    main.appendChild(sub);
    const cost = el("div", "qh-cost" + (t >= 50 ? " hot" : ""));
    if (t > 0) cost.appendChild(el("span", "arrow", "↑"));
    cost.appendChild(el("b", null, String(t)));
    cost.appendChild(el("i"));
    const icon = el("div", "qh-icon", d.quarantined ? "⊘" : t >= 50 ? "⚠" : d.self ? "◉" : d.gateway ? "⌂" : d.trusted ? "◈" : "▣");
    row.appendChild(main); row.appendChild(cost); row.appendChild(icon);
    row.addEventListener("click", () => selectDevice(d.ip));
    list.appendChild(row);
  });
  if (selectedIP && currentDeck() === "netwatch") lockReticle(selectedIP);
}

function renderAlerts() {
  const list = $("#alertList"); list.innerHTML = "";
  const alerts = STATE.alerts || [];
  $("#alertMeta").textContent = alerts.length + " events";
  if (!alerts.length) { list.appendChild(mk("empty", "✓ no anomalies · the wall holds")); return; }
  alerts.slice(0, 60).forEach((a) => {
    const sev = (a.severity || "INFO").toLowerCase();
    const cls = sev === "critical" ? "crit" : sev === "warn" ? "warn" : "info";
    const row = el("div", "alert sev-" + cls + (cls === "crit" ? " crit" : ""));
    row.appendChild(el("span", "sev", a.severity || "INFO"));
    const msg = el("div", "msg"); msg.textContent = a.message;
    const t = el("time"); t.textContent = (a.kind || "") + " · " + fmtTime(a.ts);
    msg.appendChild(t); row.appendChild(msg);
    list.appendChild(row);
  });
}

function mk(cls, txt) { return el("div", cls, txt); }
function fmtTime(ts) { if (!ts) return ""; try { return new Date(ts).toLocaleTimeString("en-GB"); } catch { return ts; } }

// ---------- selection + actions ----------
function selectDevice(ip) {
  selectedIP = ip;
  const d = STATE.devices.find((x) => x.ip === ip);
  $("#btnTrust").disabled = false;
  $("#btnTrust").textContent = d && d.trusted ? "◈ UNTRUST" : "◈ TRUST";
  $("#bwTarget").innerHTML = d ? "target: <b>" + esc(d.ip) + "</b> " + (d.hostname ? "(" + esc(d.hostname) + ")" : "") : "no host selected";
  fillTarget(d);
  $("#btnQuarantine").disabled = !d || d.self || d.gateway || d.quarantined;
  $("#btnRelease").disabled = !d || !d.quarantined;
  renderDevices();
}

$("#btnSweep").addEventListener("click", async () => { status("sweep requested"); await post("/api/sweep"); });
$("#btnTrust").addEventListener("click", async () => {
  if (!selectedIP) return;
  const d = STATE.devices.find((x) => x.ip === selectedIP);
  await post("/api/trust", { ip: selectedIP, trusted: !(d && d.trusted) });
});
$("#btnQuarantine").addEventListener("click", async () => {
  if (!selectedIP) return;
  if (!confirm(`BLACKWALL: quarantine ${selectedIP}? This will block it at the firewall.`)) return;
  status("engaging blackwall on " + selectedIP);
  await post("/api/blackwall", { ip: selectedIP, action: "quarantine" });
});
$("#btnRelease").addEventListener("click", async () => {
  if (!selectedIP) return;
  await post("/api/blackwall", { ip: selectedIP, action: "release" });
});

// ---------- copilot ----------
$("#copilotSend").addEventListener("click", () => sendCopilot(false));
$("#copilotInput").addEventListener("keydown", (e) => { if (e.key === "Enter") sendCopilot(false); });
async function sendCopilot(viaVoice) {
  const inp = $("#copilotInput"); const q = inp.value.trim(); if (!q) return;
  addCp("user", viaVoice === true ? "OPERATOR · VOICE" : "OPERATOR", q); inp.value = "";
  const cmd = await runCommand(q);
  if (cmd) { addCp("ai", "DAEMON", cmd); if (viaVoice === true) speak(cmd); return; }
  const thinking = addCp("ai", "DAEMON", "▌ parsing…");
  neuralBusy = true;
  const res = await post("/api/copilot", { query: q });
  setTimeout(() => { neuralBusy = false; }, 900);
  const ans = res && res.answer ? res.answer : "uplink down — backend not reachable.";
  thinking.querySelector(".body").textContent = ans;
  if (viaVoice === true) speak(ans);
}
function addCp(who, label, text) {
  const log = $("#copilotLog");
  const m = el("div", "cp-msg " + who);
  m.appendChild(el("span", "who", label));
  const b = el("div", "body", text); b.className = "body"; m.appendChild(b);
  log.appendChild(m); log.scrollTop = log.scrollHeight; return m;
}

// ---------- offensive ----------
$("#btnScan").addEventListener("click", async () => {
  const target = $("#scanTarget").value.trim(); if (!target) return;
  const log = $("#scanLog"); log.innerHTML = "";
  log.appendChild(el("div", "cp-msg ai", "⌁ probing " + target + " …"));
  const res = await post("/api/scan", { target });
  log.innerHTML = "";
  if (!res) { log.appendChild(el("div", "empty", "backend offline")); return; }
  (res.lines || ["no response"]).forEach((l) => log.appendChild(el("div", "cp-msg ai", l)));
});

// ---------- keyboard ----------
document.addEventListener("keydown", (e) => {
  if (e.target.tagName === "INPUT") return;
  if (e.key === "s") $("#btnSweep").click();
  if (e.key === "t") $("#btnTrust").click();
  if (e.key === "v") $("#micBtn").click();
  if (e.key === "Tab") { e.preventDefault(); cycleDeck(); }
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    const ips = STATE.devices.map((d) => d.ip); if (!ips.length) return;
    let i = ips.indexOf(selectedIP); i = e.key === "ArrowDown" ? Math.min(ips.length - 1, i + 1) : Math.max(0, i - 1);
    selectDevice(ips[i]); e.preventDefault();
  }
});
function cycleDeck() {
  const order = ["netwatch", "breach", "killchain", "attack", "grid", "ar", "sonar"];
  const cur = document.querySelector(".tab.active").dataset.deck;
  selectDeck(order[(order.indexOf(cur) + 1) % order.length]);
}

// ---------- mock (file:// preview / backend down) ----------
let mockLoaded = false;
function loadMock() {
  if (live || mockLoaded) return;
  mockLoaded = true;
  status("● DEMO DATA — backend not connected");
  STATE = {
    meta: { iface: "wlan0", subnet: "192.168.1.0/24", gateway: "192.168.1.1", watching: true, sweeping: false },
    stats: { new: 2 },
    devices: [
      { ip: "192.168.1.1", vendor: "Ubiquiti", hostname: "gateway.lan", gateway: true },
      { ip: "192.168.1.50", vendor: "VirtualBox", hostname: "ronin-rig", self: true, trusted: true },
      { ip: "192.168.1.20", vendor: "Raspberry Pi", hostname: "pi-hole.lan", trusted: true },
      { ip: "192.168.1.31", vendor: "Amazon", hostname: "echo-dot" },
      { ip: "192.168.1.77", vendor: "Samsung", hostname: "galaxy-s24" },
      { ip: "192.168.1.99", vendor: "(randomized MAC)", hostname: "" },
      { ip: "192.168.1.112", vendor: "Espressif", hostname: "esp32-node-3" },
    ],
    alerts: [
      { ts: Date.now(), severity: "CRITICAL", kind: "arp-spoof", source: "192.168.1.1", message: "gateway MAC changed f0:27:2d:aa:bb:cc → 02:11:22:33:44:55 (possible MITM)" },
      { ts: Date.now() - 42000, severity: "WARN", kind: "new-device", source: "192.168.1.99", message: "new host joined the segment: 192.168.1.99 · randomized MAC (privacy address or evasion)" },
      { ts: Date.now() - 118000, severity: "WARN", kind: "new-device", source: "192.168.1.77", message: "new host joined the segment: 192.168.1.77 · Samsung" },
      { ts: Date.now() - 301000, severity: "INFO", kind: "new-device", source: "192.168.1.112", message: "new host joined the segment: esp32-node-3 · Espressif" },
    ],
  };
  liveState = STATE;
  render();
  addCp("ai", "DAEMON", "Uplink synced. 7 hosts on the segment. 1 CRITICAL: gateway MAC changed — classic ARP-spoof signature. Recommend BLACKWALL on 192.168.1.99 and verify the router.");
}

// ================= KIROSHI HUD LAYER =================

// ---- scramble / decode text ----
const GLYPHS = "ABCDEF0123456789#@$%&*<>/\\|=+·▚▞░▒";
function scramble(elm, text, dur = 650) {
  const start = performance.now();
  function frame(now) {
    const p = Math.min(1, (now - start) / dur);
    const reveal = Math.floor(p * text.length);
    let out = "";
    for (let i = 0; i < text.length; i++) {
      out += (i < reveal || text[i] === " ") ? text[i] : GLYPHS[(Math.random() * GLYPHS.length) | 0];
    }
    elm.textContent = out;
    if (p < 1) requestAnimationFrame(frame); else elm.textContent = text;
  }
  requestAnimationFrame(frame);
}

// ---- boot sequence ----
function runBoot() {
  const boot = $("#boot");
  if (!boot) return;
  $("#bootLogo").textContent = "NETWATCH // NEURAL LINK HANDSHAKE";
  const t0 = performance.now();
  const pct = setInterval(() => {
    const v = Math.min(100, Math.round((performance.now() - t0) / 23));
    $("#bootPct").textContent = v + "%";
    if (v >= 100) clearInterval(pct);
  }, 60);
  const seq = [
    ["> establishing neural link", "ok"],
    ["> mounting sensory cortex", "ok"],
    ["> decrypting netwatch daemon", "ok"],
    ["> arming ICE countermeasures", "ok"],
    ["> loading kill-chain matrix [7 stages]", "ok"],
    ["> blackwall handshake", "ok"],
    ["> calibrating optic overlay", "warn"],
    ["> UPLINK SYNCED — welcome, runner", "ok"],
  ];
  const box = $("#bootLines");
  seq.forEach((l, i) => {
    const d = document.createElement("div");
    d.className = "ln";
    d.style.animationDelay = (i * 0.26) + "s";
    d.innerHTML = l[0].padEnd(44, " ").replace(/ /g, " ") + ' <span class="' + l[1] + '">[' + l[1].toUpperCase() + "]</span>";
    box.appendChild(d);
  });
  setTimeout(() => {
    boot.classList.add("done");
    document.querySelectorAll(".panel-head > span:first-child").forEach((s) => scramble(s, s.textContent, 700));
    setTimeout(() => { boot.style.display = "none"; booting = false; }, 700);
  }, 2500);
}

// ---- telemetry ticker ----
function rhex(n) { let s = ""; for (let i = 0; i < n; i++) s += "0123456789abcdef"[(Math.random() * 16) | 0]; return s; }
function initTicker() {
  const track = $("#ticker");
  if (!track) return;
  function gen() {
    const d = STATE.devices || [], a = STATE.alerts || [];
    const thr = a.filter((x) => x.severity === "CRITICAL").length;
    const parts = [
      'PKT/s <span class="hl">' + ((STATE.meta && STATE.meta.pps) || ((Math.random() * 9000 + 800) | 0)) + "</span>",
      'HOSTS <span class="hl">' + d.length + "</span>",
      'ICE <span class="hlg">ARMED</span>',
      "ENTROPY <span class=\"hly\">" + (Math.random() * 3 + 5).toFixed(2) + "</span>",
      "THREATS <span class=\"" + (thr ? "hlr" : "hlg") + "\">" + thr + "</span>",
      "FLOW 0x" + rhex(6) + " → 0x" + rhex(6),
      "LAT <span class=\"hl\">" + (Math.random() * 30 + 2).toFixed(1) + "ms</span>",
      "BLACKWALL <span class=\"hlr\">READY</span>",
      "ARP/s <span class=\"hl\">" + ((Math.random() * 40) | 0) + "</span>",
      "MAC " + rhex(2) + ":" + rhex(2) + ":" + rhex(2) + ":" + rhex(2) + ":" + rhex(2) + ":" + rhex(2),
      "SIG <span class=\"hlg\">NOMINAL</span>",
    ];
    return parts.join('&nbsp;&nbsp;<span class="lo">░</span>&nbsp;&nbsp;');
  }
  function refresh() { const s = gen(); track.innerHTML = s + '&nbsp;&nbsp;<span class="lo">░</span>&nbsp;&nbsp;' + s; }
  tickerRefresh = refresh;
  refresh();
  setInterval(refresh, 9000);
}

// ---- target-lock reticle ----
function riskScore(d) {
  let s = 0;
  const crit = (STATE.alerts || []).some((a) => a.source === d.ip && a.severity === "CRITICAL");
  if (crit) s += 60;
  if (d.quarantined) s += 50;
  if (!d.trusted && !d.self && !d.gateway) s += 20;
  if ((d.vendor || "").includes("randomized")) s += 25;
  if (!d.hostname) s += 10;
  if (d.trusted) s -= 40;
  return Math.max(0, Math.min(100, s));
}
function lockReticle(ip) {
  const ret = $("#reticle");
  const d = (STATE.devices || []).find((x) => x.ip === ip);
  const rows = [...document.querySelectorAll("#deviceList .dev")];
  const row = rows.find((r) => r.querySelector(".ip") && r.querySelector(".ip").textContent === ip);
  if (!d || !row || currentDeck() !== "netwatch") { ret.hidden = true; return; }
  const r = row.getBoundingClientRect();
  ret.style.left = (r.left - 6) + "px";
  ret.style.top = (r.top - 4) + "px";
  ret.style.width = (r.width + 12) + "px";
  ret.style.height = (r.height + 8) + "px";
  const risk = threatOf(d);
  $("#rtag").innerHTML = "◈ " + esc((d.hostname || d.ip).toUpperCase()) + "<small>THREAT " + risk + "/100</small>";
  ret.hidden = false;
}

// ================= KILL CHAIN (purple team) =================
const KILLCHAIN = [
  { name: "RECON", attck: "T1595 · T1046", actions: [
    { side: "red", ttl: "Host sweep (ARP)", att: "T1046", type: "api", endpoint: "/api/sweep", btn: "RUN" },
    { side: "red", ttl: "Port / service scan", att: "T1595.001", type: "api", endpoint: "/api/scan", needsTarget: true, btn: "PROBE" },
    { side: "blue", ttl: "Listening-socket audit", att: "DS0029", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
    { side: "blue", ttl: "Scan-detection (ICE)", att: "T1046", type: "info", info: ["ICE watches for ARP floods + fan-out connection patterns.", "New-device + port-scan detectors flag recon against you in the NETWATCH deck."] },
  ]},
  { name: "WEAPON", attck: "T1587 · T1588", actions: [
    { side: "red", ttl: "EICAR test artifact", att: "benign", type: "api", endpoint: "/api/kc/eicar", btn: "BUILD" },
    { side: "blue", ttl: "AV / EDR validation", att: "DS0013", type: "info", info: ["Drop the EICAR string to a file — a healthy scanner must quarantine it.", "If nothing fires, your endpoint protection has a blind spot."] },
  ]},
  { name: "DELIVERY", attck: "T1566 · T1091", actions: [
    { side: "red", ttl: "Delivery vector brief", att: "T1566", type: "info", info: ["Common vectors: spear-phish attachment/link, watering-hole, removable media, supply-chain.", "PLANNING ONLY — NetWatch does not build phishing or delivery infrastructure."] },
    { side: "blue", ttl: "Ingress monitoring brief", att: "DS0022", type: "info", info: ["Watch: new executables in Downloads, macro-enabled docs, USB mass-storage mounts.", "Pair with the persistence + connection audits below to catch what slips through."] },
  ]},
  { name: "EXPLOIT", attck: "T1203 · T1190", actions: [
    { side: "red", ttl: "Service version probe", att: "T1190", type: "api", endpoint: "/api/scan", needsTarget: true, btn: "PROBE" },
    { side: "blue", ttl: "Attack-surface audit", att: "DS0009", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
  ]},
  { name: "INSTALL", attck: "T1543 · T1547 · T1053", actions: [
    { side: "red", ttl: "Persistence vector brief", att: "T1547", type: "info", info: ["Vectors: systemd units, cron/@reboot, shell rc, autostart .desktop, rc.local.", "Use the BLUE audit to see which of these exist on this host right now."] },
    { side: "blue", ttl: "Persistence audit (host)", att: "T1543", type: "api", endpoint: "/api/kc/persistence", btn: "AUDIT" },
  ]},
  { name: "C2", attck: "T1071 · T1571 · T1572", actions: [
    { side: "red", ttl: "Beacon-profile brief", att: "T1071", type: "info", info: ["Beacons = periodic low-jitter callbacks on app-layer protocols / odd ports / DNS tunnels.", "PLANNING ONLY — no C2 implant is generated. Hunt for it with the BLUE tools."] },
    { side: "blue", ttl: "Established peers", att: "T1071", type: "api", endpoint: "/api/kc/connections", btn: "HUNT" },
    { side: "blue", ttl: "Non-standard listeners", att: "T1571", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
  ]},
  { name: "ACTIONS", attck: "T1041 · T1048 · T1486", actions: [
    { side: "red", ttl: "Exfil scenario brief", att: "T1041", type: "info", info: ["Exfil over C2 channel, over alternate protocol (DNS/ICMP), or to cloud storage.", "PLANNING ONLY. Detect it below by watching large/odd outbound flows."] },
    { side: "blue", ttl: "Exfil watch (public peers)", att: "T1048", type: "api", endpoint: "/api/kc/connections", btn: "WATCH" },
  ]},
];
const ATTACK = [
  { name: "RECON", attck: "TA0043", actions: [
    { side: "red", ttl: "Active scanning", att: "T1595", type: "api", endpoint: "/api/scan", needsTarget: true, btn: "PROBE" },
    { side: "blue", ttl: "Scan detection (ICE)", att: "DS0029", type: "info", info: ["ICE flags ARP floods and port-scan fan-out against you.", "Watch the WATCHTOWER intrusion log."] },
  ]},
  { name: "RES DEV", attck: "TA0042", actions: [
    { side: "red", ttl: "Capability test (EICAR)", att: "T1588", type: "api", endpoint: "/api/kc/eicar", btn: "BUILD" },
    { side: "blue", ttl: "AV / EDR validation", att: "DS0013", type: "info", info: ["Drop the benign EICAR string — a healthy scanner must quarantine it."] },
  ]},
  { name: "ACCESS", attck: "TA0001", actions: [
    { side: "red", ttl: "Public-facing service probe", att: "T1190", type: "api", endpoint: "/api/scan", needsTarget: true, btn: "PROBE" },
    { side: "blue", ttl: "Exposed-surface audit", att: "DS0009", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
  ]},
  { name: "EXEC", attck: "TA0002", actions: [
    { side: "red", ttl: "Execution test (EICAR)", att: "T1204", type: "api", endpoint: "/api/kc/eicar", btn: "BUILD" },
    { side: "blue", ttl: "Process-exec monitoring", att: "DS0009", type: "info", info: ["Watch new process trees, interpreters spawning shells, LOLBins.", "Pair with the persistence audit to catch what survives."] },
  ]},
  { name: "PERSIST", attck: "TA0003", actions: [
    { side: "red", ttl: "Autostart vector brief", att: "T1547", type: "info", info: ["Vectors: systemd, cron/@reboot, shell rc, autostart, rc.local.", "Use the BLUE audit to list what exists on this host now."] },
    { side: "blue", ttl: "Persistence audit (host)", att: "T1543", type: "api", endpoint: "/api/kc/persistence", btn: "AUDIT" },
  ]},
  { name: "PRIVESC", attck: "TA0004", actions: [
    { side: "red", ttl: "Priv-esc surface brief", att: "T1548", type: "info", info: ["Surfaces: SUID/SGID binaries, sudo misconfig, writable units, stale kernels.", "PLANNING ONLY — no exploit is launched."] },
    { side: "blue", ttl: "Service / owner audit", att: "DS0009", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
  ]},
  { name: "EVASION", attck: "TA0005", actions: [
    { side: "red", ttl: "Evasion technique brief", att: "T1070", type: "info", info: ["Timestomping, log clearing, masquerading, disabling tooling.", "PLANNING ONLY."] },
    { side: "blue", ttl: "Log-integrity brief", att: "DS0017", type: "info", info: ["Ship logs off-host, watch for gaps, alert on auditd/journald tampering."] },
  ]},
  { name: "CREDS", attck: "TA0006", actions: [
    { side: "red", ttl: "Credential-vector brief", att: "T1110", type: "info", info: ["Brute force, OS credential stores, in-memory secrets.", "PLANNING ONLY — NetWatch does not dump or crack credentials."] },
    { side: "blue", ttl: "Auth-failure monitoring", att: "DS0002", type: "info", info: ["Alert on auth spikes, impossible travel, first use of a credential."] },
  ]},
  { name: "DISCOVERY", attck: "TA0007", actions: [
    { side: "red", ttl: "Host sweep (ARP)", att: "T1046", type: "api", endpoint: "/api/sweep", btn: "RUN" },
    { side: "red", ttl: "Service discovery", att: "T1046", type: "api", endpoint: "/api/scan", needsTarget: true, btn: "PROBE" },
    { side: "blue", ttl: "Established peers", att: "T1018", type: "api", endpoint: "/api/kc/connections", btn: "HUNT" },
  ]},
  { name: "LATERAL", attck: "TA0008", actions: [
    { side: "red", ttl: "Lateral-movement brief", att: "T1021", type: "info", info: ["Remote services (SSH/SMB/RDP), pass-the-hash, remote exec.", "PLANNING ONLY."] },
    { side: "blue", ttl: "Peer / listener audit", att: "T1021", type: "api", endpoint: "/api/kc/connections", btn: "HUNT" },
  ]},
  { name: "COLLECT", attck: "TA0009", actions: [
    { side: "red", ttl: "Collection-target brief", att: "T1005", type: "info", info: ["Local files, clipboard, screen capture, staging directories.", "PLANNING ONLY."] },
    { side: "blue", ttl: "Data-staging brief", att: "DS0022", type: "info", info: ["Watch for large archives in temp/staging and unusual read bursts."] },
  ]},
  { name: "C2", attck: "TA0011", actions: [
    { side: "red", ttl: "Beacon-profile brief", att: "T1071", type: "info", info: ["Periodic low-jitter callbacks, non-standard ports, DNS tunnels.", "PLANNING ONLY — no implant is generated."] },
    { side: "blue", ttl: "Established peers", att: "T1071", type: "api", endpoint: "/api/kc/connections", btn: "HUNT" },
    { side: "blue", ttl: "Non-standard listeners", att: "T1571", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
  ]},
  { name: "EXFIL", attck: "TA0010", actions: [
    { side: "red", ttl: "Exfil-channel brief", att: "T1041", type: "info", info: ["Over the C2 channel, an alternate protocol (DNS/ICMP), or cloud storage.", "PLANNING ONLY."] },
    { side: "blue", ttl: "Egress watch (public peers)", att: "T1048", type: "api", endpoint: "/api/kc/connections", btn: "WATCH" },
  ]},
  { name: "IMPACT", attck: "TA0040", actions: [
    { side: "red", ttl: "Impact-scenario brief", att: "T1486", type: "info", info: ["Encryption for impact, inhibit recovery, service/DoS, defacement.", "PLANNING ONLY — NetWatch performs no destructive action."] },
    { side: "blue", ttl: "Resilience brief", att: "DS0022", type: "info", info: ["Offline/immutable backups, egress rate alerts, recovery drills."] },
  ]},
];

// shared chain renderer (KILL CHAIN + ATT&CK). ids: {flow,title,attck,body,ops,target,taxon}
function makeChain(model, ids) {
  let stage = 0;
  const cov = (st) => Math.round(st.actions.filter((a) => a.type === "api").length / st.actions.length * 100);
  function renderFlow() {
    const flow = $(ids.flow); flow.innerHTML = "";
    model.forEach((st, i) => {
      const n = el("div", "kc-node" + (i === stage ? " active" : ""));
      n.innerHTML = '<div class="num">' + (i + 1 < 10 ? "0" : "") + (i + 1) + '</div><div class="nm">' + st.name + '</div><div class="cov"><i style="width:' + cov(st) + '%"></i></div>';
      n.addEventListener("click", () => { stage = i; renderFlow(); renderStage(); });
      flow.appendChild(n);
    });
  }
  function renderStage() {
    const st = model[stage];
    const title = (stage + 1) + " · " + st.name;
    $(ids.title).textContent = title;
    $(ids.attck).textContent = (ids.taxon || "ATT&CK ") + st.attck;
    const body = $(ids.body); body.innerHTML = "";
    [["red", "◢ RED · OFFENSE (authorized/lab)"], ["blue", "◤ BLUE · DEFENSE"]].forEach(([side, label]) => {
      const acts = st.actions.filter((a) => a.side === side);
      if (!acts.length) return;
      const sec = el("div", "kc-sec " + side);
      sec.appendChild(el("h4", null, label));
      acts.forEach((a) => {
        const row = el("div", "kc-tech");
        row.appendChild(el("span", "ttl", a.ttl));
        row.appendChild(el("span", "att", a.att));
        const btn = el("button", "go" + (a.type === "info" ? " info" : ""), a.btn || "INFO");
        btn.addEventListener("click", () => run(a));
        row.appendChild(btn);
        sec.appendChild(row);
      });
      body.appendChild(sec);
    });
    scramble($(ids.title), title, 450);
  }
  async function run(a) {
    if (a.type === "info") { log(a.side, a.ttl, a.info); return; }
    if (a.endpoint === "/api/sweep") { await post("/api/sweep"); log(a.side, a.ttl, ["sweep dispatched → results populate the WATCHTOWER deck"]); return; }
    let body = {};
    if (a.needsTarget) {
      const t = $(ids.target).value.trim();
      if (!t) { log(a.side, a.ttl, ["⚠ enter a lab target (an IP you own / are authorized to test) in the field below"]); return; }
      body = { target: t };
    }
    const bodyEl = log(a.side, a.ttl, ["▌ executing…"]);
    const res = await post(a.endpoint, body);
    bodyEl.textContent = (res && res.lines ? res.lines : ["no response / backend offline"]).join("\n");
  }
  function log(side, head, lines) {
    const box = $(ids.ops);
    const op = el("div", "op" + (side === "red" ? " red" : ""));
    op.appendChild(el("span", "ohead", "▸ " + head + "  ·  " + new Date().toLocaleTimeString("en-GB")));
    const bodyEl = el("div", "obody");
    bodyEl.textContent = (lines || []).join("\n");
    op.appendChild(bodyEl);
    box.insertBefore(op, box.firstChild);
    return bodyEl;
  }
  return { init() { renderFlow(); renderStage(); } };
}
const killchainDeck = makeChain(KILLCHAIN, { flow: "#kcFlow", title: "#kcStageTitle", attck: "#kcAttck", body: "#kcStageBody", ops: "#kcOpsLog", target: "#kcTarget" });
const attackDeck = makeChain(ATTACK, { flow: "#attFlow", title: "#attStageTitle", attck: "#attAttck", body: "#attStageBody", ops: "#attOpsLog", target: "#attTarget", taxon: "TACTIC " });

// keep reticle glued to the selected row while the device list scrolls
document.addEventListener("DOMContentLoaded", () => {
  const dl = $("#deviceList");
  if (dl) dl.addEventListener("scroll", () => { if (selectedIP) lockReticle(selectedIP); });
});
window.addEventListener("resize", () => { if (selectedIP) lockReticle(selectedIP); });

// ================= GRID: external-connection globe (three.js) =================
let globe = null;
function latLon(lat, lon, r) {
  const phi = (90 - lat) * Math.PI / 180, theta = (lon + 180) * Math.PI / 180;
  return new THREE.Vector3(-r * Math.sin(phi) * Math.cos(theta), r * Math.cos(phi), r * Math.sin(phi) * Math.sin(theta));
}
function initGlobe() {
  if (globe || typeof THREE === "undefined") return;
  try { buildGlobe(); } catch (e) {
    const host = $("#globeCanvas");
    if (host) host.innerHTML = '<div class="empty" style="position:absolute;inset:0;display:flex;align-items:center;justify-content:center">WebGL unavailable — egress list still live →</div>';
  }
}
function buildGlobe() {
  const host = $("#globeCanvas");
  const w = host.clientWidth || 800, h = host.clientHeight || 600;
  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(45, w / h, 0.1, 100);
  camera.position.set(0, 0.4, 3.2);
  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.setSize(w, h);
  host.appendChild(renderer.domElement);

  const group = new THREE.Group(); scene.add(group);
  const geo = new THREE.SphereGeometry(1, 48, 48);
  group.add(new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ color: 0x08141f })));
  group.add(new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ color: 0x0a7384, wireframe: true, transparent: true, opacity: 0.32 })));
  const selfPos = latLon(37, -98, 1.02);
  group.add(mkDot(selfPos, 0xff2bd6, 0.035));
  const dyn = new THREE.Group(); group.add(dyn);

  globe = { scene, camera, renderer, group, dyn, selfPos, host };
  globe.resize = () => { const w = host.clientWidth, h = host.clientHeight; if (!w || !h) return; camera.aspect = w / h; camera.updateProjectionMatrix(); renderer.setSize(w, h); };
  window.addEventListener("resize", globe.resize);
  (function animate() {
    requestAnimationFrame(animate);
    if (currentDeck() === "grid") { group.rotation.y += 0.0016; renderer.render(scene, camera); }
  })();
  setInterval(() => { if (currentDeck() === "grid") loadGlobe(); }, 15000);
}
function mkDot(pos, color, r) {
  const m = new THREE.Mesh(new THREE.SphereGeometry(r || 0.02, 10, 10), new THREE.MeshBasicMaterial({ color }));
  m.position.copy(pos); return m;
}
async function loadGlobe() {
  if (!globe) return;
  const res = await post("/api/globe");
  const pts = (res && res.points) || [];
  while (globe.dyn.children.length) globe.dyn.remove(globe.dyn.children[0]);
  pts.forEach((p) => {
    const sus = !p.sni || p.port === 8443 || p.port === 9001;
    const col = sus ? 0xff2e4d : 0x00e5ff;
    const pos = latLon(p.lat, p.lon, 1.02);
    globe.dyn.add(mkDot(pos, col, sus ? 0.028 : 0.02));
    const mid = globe.selfPos.clone().add(pos).multiplyScalar(0.5).normalize().multiplyScalar(1.5);
    const curve = new THREE.QuadraticBezierCurve3(globe.selfPos, mid, pos);
    const g = new THREE.BufferGeometry().setFromPoints(curve.getPoints(32));
    globe.dyn.add(new THREE.Line(g, new THREE.LineBasicMaterial({ color: col, transparent: true, opacity: 0.55 })));
  });
  $("#globeMeta").textContent = pts.length + " egress nodes";
  renderGlobeList(pts);
}
function renderGlobeList(pts) {
  const list = $("#globeList"); list.innerHTML = "";
  if (!pts.length) { list.appendChild(el("div", "empty", "no external connections")); return; }
  pts.slice().sort((a, b) => b.bytes - a.bytes).forEach((p) => {
    const sus = !p.sni || p.port === 8443;
    const n = el("div", "gnode" + (sus ? " sus" : ""));
    n.innerHTML = '<span class="d">' + esc(p.dst) + ":" + esc(p.port) + '</span><span class="r">' + esc(p.region) + " · " + fmtBytes(p.bytes) + "</span>" +
      '<span class="s">' + (p.sni ? esc(p.sni) : "⚠ no SNI — opaque egress") + "</span>";
    list.appendChild(n);
  });
}
function fmtBytes(b) { if (b >= 1e6) return (b / 1e6).toFixed(1) + "MB"; if (b >= 1e3) return (b / 1e3).toFixed(0) + "KB"; return b + "B"; }

// ================= AR: Kiroshi optic overlay =================
let arStream = null;
document.addEventListener("DOMContentLoaded", () => {
  const b = $("#arStart"); if (b) b.addEventListener("click", startAR);
});
async function startAR() {
  const v = $("#arVideo");
  try {
    arStream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false });
    v.srcObject = arStream;
    $("#arNote").textContent = "optic online — tagging segment in real space";
  } catch (e) {
    $("#arNote").textContent = "camera unavailable (" + e.name + ") — synthetic overlay active";
  }
  renderARTags();
}
function riskLite(d) { let s = 0; if (!d.trusted && !d.gateway && !d.self) s += 20; if ((d.vendor || "").includes("randomized")) s += 25; if (!d.hostname) s += 10; return s; }
function renderARTags() {
  const ov = $("#arOverlay"); if (!ov) return;
  ov.innerHTML = "";
  const devs = (STATE.devices || []).filter((d) => !d.self).slice(0, 8);
  devs.forEach((d, i) => {
    const sus = d.anomaly >= 50 || d.beacon || d.quarantined;
    const t = el("div", "ar-tag" + (sus ? " sus" : ""));
    const risk = Math.round(Math.max(d.anomaly || 0, riskLite(d)));
    let html = "<b>◢ " + esc(d.hostname || d.ip) + "</b><br>" +
      '<span class="lo">IP</span> ' + esc(d.ip) + "<br>" +
      '<span class="lo">OS</span> ' + esc(d.os || "unknown") + "<br>" +
      '<span class="lo">THREAT</span> ' + risk + "/100";
    if (d.beacon) html += '<br><span class="lo">BEACON</span> ' + esc(d.beacon);
    t.innerHTML = html;
    t.style.left = (10 + (i % 4) * 22 + (i * 7 % 9)) + "%";
    t.style.top = (16 + Math.floor(i / 4) * 32 + (i * 13 % 10)) + "%";
    ov.appendChild(t);
  });
}


// ================= SCANNER HUD: integrity bar, target panel, neural link =================
let neuralBusy = false, booting = true;

function renderHud() {
  const devs = STATE.devices || [], m = STATE.meta || {};
  const risky = devs.filter((d) => threatOf(d) >= 50).length;
  $("#intVal").textContent = (devs.length - risky) + "/" + devs.length;
  $("#intStat").classList.toggle("hot", risky > 0);
  const thr = threats();
  $("#ppsVal").textContent = m.pps || 0;
  $("#thrVal").textContent = thr;
  $("#thrStat").classList.toggle("hot", thr > 0);
  const link = $("#linkState");
  link.textContent = thr > 0 ? "INTRUSION DETECTED" : "SYNCED";
  link.classList.toggle("bad", thr > 0);
}

// ---- resource bars: live RAM and CPU of the machine running NetWatch ----
function fillBar(id, segs, pct) {
  const bar = $(id);
  if (bar.children.length !== segs) { bar.innerHTML = ""; for (let i = 0; i < segs; i++) { const seg = el("i", "off"); seg.style.animationDelay = (i * 0.03) + "s"; bar.appendChild(seg); } }
  const on = Math.max(pct > 0 ? 1 : 0, Math.round(pct / 100 * segs)), hot = pct >= 85;
  [...bar.children].forEach((seg, i) => { seg.className = i < on ? (hot ? "hot" : "") : "off"; });
}
async function sysPoll() {
  const r = await get("/api/sys");
  if (!r || !r.ok) { $("#ramText").textContent = "n/a"; $("#cpuText").textContent = "n/a"; return; }
  const gb = (b) => (b / 1073741824).toFixed(1);
  $("#ramText").textContent = gb(r.memUsed) + "/" + gb(r.memTotal) + " GB";
  $("#cpuText").textContent = Math.round(r.cpu) + "%";
  $("#cpuCores").textContent = r.cores;
  $("#cpuLoad").textContent = r.load1.toFixed(2);
  fillBar("#ramBar", 16, r.memPct);
  fillBar("#cpuBar", 12, r.cpu);
}

function tri(sym, label, cls) {
  return '<div class="tri ' + cls + '"><svg viewBox="0 0 40 36"><path d="M20 3 37 33H3Z" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linejoin="round"/>' +
    '<text x="20" y="28" text-anchor="middle" font-size="14" font-weight="700" fill="currentColor" font-family="sans-serif">' + sym + "</text></svg>" + label + "</div>";
}
function fillTarget(d) {
  if (!d) return;
  $("#tpName").textContent = (d.hostname || d.ip).toUpperCase();
  $("#tpAff").textContent = (d.vendor || "unknown").toUpperCase();
  let kv = "<span>IP</span>" + esc(d.ip) + "<br><span>MAC</span>" + esc(d.mac || "--") + "<br><span>OS</span>" + esc(d.os || "profiling…");
  if (d.ja3) kv += "<br><span>JA3</span>" + esc(d.ja3.slice(0, 20)) + "…";
  if (d.sni) kv += "<br><span>SNI</span>" + esc(d.sni);
  if (d.beacon) kv += '<br><span>C2?</span><b style="color:var(--red)">' + esc(d.beacon) + "</b>";
  $("#tpKV").innerHTML = kv;
  const iceHit = (STATE.alerts || []).some((a) => a.source === d.ip && a.severity === "CRITICAL");
  const randomMac = (d.vendor || "").includes("randomized");
  let good = "", bad = "";
  if (d.self) good += tri("◉", "SELF", "good");
  if (d.gateway) good += tri("⌂", "GATEWAY", "good");
  if (d.trusted) good += tri("✓", "TRUSTED", "good");
  if (d.hostname) good += tri("N", "NAMED", "good");
  if (d.vendor && !randomMac) good += tri("V", "VENDOR ID", "good");
  if (!d.trusted && !d.self && !d.gateway) bad += tri("?", "UNVERIFIED", "bad");
  if (randomMac) bad += tri("≠", "RANDOM MAC", "bad");
  if (d.anomaly >= 25) bad += tri("~", "ANOMALY", "bad");
  if (d.beacon) bad += tri("⚡", "BEACON", "bad");
  if (iceHit) bad += tri("!", "ICE ALERT", "bad");
  if (d.quarantined) bad += tri("⊘", "LOCKED", "bad");
  const t = threatOf(d);
  $("#tpGood").innerHTML = good; $("#tpBad").innerHTML = bad;
  $("#tpGoodPct").textContent = (100 - t) + " %";
  $("#tpBadPct").textContent = t + " %";
}
document.querySelectorAll(".tp-tab").forEach((t) => t.addEventListener("click", () => {
  document.querySelectorAll(".tp-tab").forEach((x) => x.classList.toggle("active", x === t));
  document.querySelectorAll(".tp-body").forEach((b) => (b.hidden = b.dataset.tp !== t.dataset.tp));
}));

// ---- neural-link brain: a point-cloud cortex with synapse fire, static and glitch ----
function makeBrain(canvas) {
  const ctx = canvas.getContext("2d"), W = canvas.width, H = canvas.height;
  const sx = W / 2.62, sy = H / 2.1, cx = W * 0.52, cy = H * 0.44;
  const inE = (x, y, ex, ey, rx, ry) => ((x - ex) / rx) ** 2 + ((y - ey) / ry) ** 2 < 1;
  const region = (x, y) => {
    if (inE(x, y, 0, -0.1, 1.05, 0.68) && y < 0.34) return 1;          // cerebrum
    if (inE(x, y, 0.2, 0.3, 0.62, 0.3)) return 1;                       // temporal lobe
    if (inE(x, y, -0.64, 0.5, 0.36, 0.23)) return 2;                    // cerebellum
    if (y > 0.36 && y < 0.98) { const t = (y - 0.36) / 0.62; if (x > -0.34 - 0.16 * t && x < -0.1 - 0.2 * t) return 3; } // stem
    return 0;
  };
  const pts = [];
  for (let g = 0; pts.length < 430 && g < 30000; g++) {
    const x = Math.random() * 2.5 - 1.25, y = Math.random() * 1.95 - 0.85, r = region(x, y);
    if (r) pts.push({ x, y, r, ph: Math.random() * 6.28, hot: Math.random() < 0.07 });
  }
  for (let a = Math.PI * 0.94; a < Math.PI * 2.14; a += 0.03) pts.push({ x: Math.cos(a) * 1.05, y: -0.1 + Math.sin(a) * 0.68, r: 1, ph: a, rim: true });
  const edges = [];
  for (let i = 0; i < pts.length && edges.length < 1100; i++) for (let j = i + 1; j < pts.length; j++) {
    const dx = pts[i].x - pts[j].x, dy = pts[i].y - pts[j].y;
    if (dx * dx + dy * dy < 0.016) edges.push([i, j]);
  }
  const folds = [];
  for (let k = 0; k < 6; k++) {
    const f = [];
    for (let x = -0.95; x <= 0.95; x += 0.05) { const y = -0.55 + k * 0.15 + 0.07 * Math.sin(x * 6 + k * 1.7); if (region(x, y) === 1) f.push([x, y]); }
    folds.push(f);
  }
  const pos = new Float32Array(pts.length * 2);
  return function tick(t, busy, build) {
    ctx.clearRect(0, 0, W, H);
    const band = Math.random() < (busy ? 0.5 : 0.22) ? Math.random() * H : -1, bandH = 8 + Math.random() * 26, bandDx = (Math.random() - 0.5) * (busy ? 60 : 34);
    const shown = build == null ? pts.length : Math.floor(pts.length * build);
    for (let i = 0; i < pts.length; i++) {
      const p = pts[i];
      let X = cx + p.x * sx + Math.sin(t * 2 + p.ph) * 1.4, Y = cy + p.y * sy + Math.cos(t * 1.7 + p.ph) * 1.4;
      if (band >= 0 && Y > band && Y < band + bandH) X += bandDx;
      pos[i * 2] = X; pos[i * 2 + 1] = Y;
    }
    // synapses
    ctx.lineWidth = 1; ctx.strokeStyle = "rgba(94,246,255,0.14)"; ctx.beginPath();
    const fired = [];
    for (const e of edges) {
      if (e[0] >= shown || e[1] >= shown) continue;
      if (Math.random() < (busy ? 0.09 : 0.025)) { fired.push(e); continue; }
      ctx.moveTo(pos[e[0] * 2], pos[e[0] * 2 + 1]); ctx.lineTo(pos[e[1] * 2], pos[e[1] * 2 + 1]);
    }
    ctx.stroke();
    ctx.lineWidth = 2; ctx.strokeStyle = busy ? "rgba(255,184,74,0.95)" : "rgba(214,255,255,0.9)"; ctx.beginPath();
    for (const e of fired) { ctx.moveTo(pos[e[0] * 2], pos[e[0] * 2 + 1]); ctx.lineTo(pos[e[1] * 2], pos[e[1] * 2 + 1]); }
    ctx.stroke();
    // cortical folds
    ctx.lineWidth = 1.5; ctx.strokeStyle = "rgba(94,246,255,0.28)";
    for (const f of folds) { ctx.beginPath(); f.forEach((q, i) => { const X = cx + q[0] * sx, Y = cy + q[1] * sy + Math.sin(t * 3 + q[0] * 9) * 1.5; i ? ctx.lineTo(X, Y) : ctx.moveTo(X, Y); }); ctx.stroke(); }
    // neurons
    for (let i = 0; i < shown; i++) {
      const p = pts[i]; let a = 0.3 + 0.7 * Math.abs(Math.sin(t * 3 + p.ph));
      if (Math.random() < 0.04) a = 1;
      ctx.fillStyle = p.hot ? "rgba(255,84,73," + a + ")" : p.r === 2 ? "rgba(29,255,161," + a + ")" : p.rim ? "rgba(189,249,255," + (0.5 + a * 0.5) + ")" : "rgba(94,246,255," + a + ")";
      const sz = p.hot ? 4 : p.rim ? 3 : 2.5;
      ctx.fillRect(pos[i * 2] - sz / 2, pos[i * 2 + 1] - sz / 2, sz, sz);
    }
    // static
    const n = busy ? 420 : 170;
    for (let i = 0; i < n; i++) { ctx.fillStyle = "rgba(200,255,255," + (Math.random() * 0.35) + ")"; ctx.fillRect(Math.random() * W, Math.random() * H, 2, 2); }
    if (Math.random() < 0.3) { const y = Math.random() * H; ctx.fillStyle = "rgba(255,84,73,0.25)"; ctx.fillRect(0, y, W, 2 + Math.random() * 3); }
    // scan bar + chromatic ghost
    ctx.fillStyle = "rgba(94,246,255,0.22)"; ctx.fillRect(0, (t * 90) % H, W, 3);
    if (Math.random() < (busy ? 0.6 : 0.2)) { ctx.globalAlpha = 0.28; ctx.globalCompositeOperation = "lighter"; ctx.drawImage(canvas, (Math.random() - 0.5) * 16, 0); ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over"; }
  };
}

// ---- neural link trace (header) ----
function makeEEG(canvas) {
  const ctx = canvas.getContext("2d"), W = canvas.width, H = canvas.height, buf = new Array(W / 2).fill(0);
  let spike = 0;
  return function tick(t, hot) {
    if (Math.random() < 0.035) spike = 6;
    let v = Math.sin(t * 9) * 2.4 + Math.sin(t * 23) * 1.4 + (Math.random() - 0.5) * (hot ? 7 : 2.4);
    if (spike > 0) { v += [0, -5, 11, -9, 4, -2][6 - spike] || 0; spike--; }
    buf.push(v); buf.shift();
    ctx.clearRect(0, 0, W, H);
    ctx.strokeStyle = hot ? "#ff5449" : "#5ef6ff"; ctx.lineWidth = 1.5; ctx.shadowColor = ctx.strokeStyle; ctx.shadowBlur = 6;
    ctx.beginPath(); buf.forEach((y, i) => { const Y = H / 2 - y; i ? ctx.lineTo(i * 2, Y) : ctx.moveTo(0, Y); }); ctx.stroke();
  };
}

function initNeural() {
  const g = document.createElement("canvas"); g.width = g.height = 128;
  const gx = g.getContext("2d"), img = gx.createImageData(128, 128);
  for (let i = 0; i < img.data.length; i += 4) { const v = (Math.random() * 255) | 0; img.data[i] = img.data[i + 1] = img.data[i + 2] = v; img.data[i + 3] = 255; }
  gx.putImageData(img, 0, 0);
  $("#grain").style.backgroundImage = "url(" + g.toDataURL() + ")";

  const brain = makeBrain($("#brain")), bootBrain = makeBrain($("#bootBrain")), eeg = makeEEG($("#eeg"));
  const t0 = performance.now();
  let frame = 0;
  setInterval(() => {
    const t = (performance.now() - t0) / 1000; frame++;
    if (booting) { bootBrain(t, true, Math.min(1, t / 2.2)); return; }
    eeg(t, threats() > 0);
    if ($("#brain").offsetParent) {
      brain(t, neuralBusy);
      if (frame % 8 === 0) {
        $("#neural").classList.toggle("busy", neuralBusy);
        $("#nlState").textContent = neuralBusy ? "PARSING QUERY" : "SYNAPSE SYNC";
        $("#nlPct").textContent = (neuralBusy ? 55 + Math.random() * 30 : 97 + Math.random() * 2.6).toFixed(1) + "%";
      }
    }
  }, 55);
}

// ================= TIME SCRUBBER: replay the console's recent past =================
let hist = [];
async function loadHistory() {
  const r = await get("/api/history");
  if (!r || !r.entries) return;
  hist = r.entries;
  const range = $("#scrubRange");
  range.max = Math.max(0, hist.length - 1);
  if (!replay) { range.value = range.max; $("#scrubTime").textContent = hist.length ? "LIVE · " + hist.length + " frames" : "LIVE"; }
  drawScrubMarks();
}
function drawScrubMarks() {
  const c = $("#scrubMarks"), w = c.clientWidth || 800;
  c.width = w * 2; c.height = 36;
  const x = c.getContext("2d");
  x.clearRect(0, 0, c.width, c.height);
  x.fillStyle = "rgba(94,246,255,0.18)"; x.fillRect(0, 17, c.width, 2);
  if (hist.length < 2) return;
  const maxHosts = Math.max(...hist.map((h) => h.hosts), 1);
  x.strokeStyle = "rgba(94,246,255,0.45)"; x.lineWidth = 2; x.beginPath();
  hist.forEach((h, i) => { const X = i / (hist.length - 1) * c.width, Y = 34 - (h.hosts / maxHosts) * 14; i ? x.lineTo(X, Y) : x.moveTo(X, Y); });
  x.stroke();
  for (let i = 1; i < hist.length; i++) {
    const X = i / (hist.length - 1) * c.width;
    if (hist[i].crit > hist[i - 1].crit) { x.fillStyle = "#ff5449"; x.fillRect(X - 2, 0, 5, 36); }
    else if (hist[i].alerts > hist[i - 1].alerts) { x.fillStyle = "#ffb84a"; x.fillRect(X - 1, 6, 3, 24); }
  }
}
async function scrubTo(i) {
  if (!hist.length || i >= hist.length - 1) return goLive();
  const snap = await get("/api/history/at?i=" + i);
  if (!snap) return;
  replay = true;
  document.body.classList.add("replay");
  $("#scrubLive").classList.remove("active");
  $("#scrubRange").value = i;
  $("#scrubTime").textContent = "REPLAY · " + new Date(hist[i].ts).toLocaleTimeString("en-GB");
  STATE = snap; render();
}
function goLive() {
  replay = false;
  document.body.classList.remove("replay");
  $("#scrubLive").classList.add("active");
  $("#scrubRange").value = $("#scrubRange").max;
  $("#scrubTime").textContent = "LIVE · " + hist.length + " frames";
  if (liveState) { STATE = liveState; render(); }
}
$("#scrubRange").addEventListener("input", (e) => scrubTo(+e.target.value));
$("#scrubLive").addEventListener("click", goLive);
window.addEventListener("resize", drawScrubMarks);

// ================= SONAR: Wi-Fi CSI presence field =================
let sonarState = { nodes: [], armed: false }, sonarPos = {}, sonarDrag = null;
try { sonarPos = JSON.parse(localStorage.getItem("nw.sonarPos") || "{}"); } catch (_) { sonarPos = {}; }
function sonarXY(name, idx, total) {
  if (!sonarPos[name]) {
    if (name === "@ap") sonarPos[name] = { x: 0, y: -0.05 };
    else { const a = Math.PI * (0.15 + 0.7 * (total > 1 ? idx / (total - 1) : 0.5)); sonarPos[name] = { x: Math.cos(a) * 0.72 * (idx % 2 ? 1 : 0.85), y: 0.1 + Math.sin(a) * 0.62 }; if (idx === 0) sonarPos[name] = { x: -0.7, y: -0.5 }; }
  }
  return sonarPos[name];
}
function sonarResize() { const c = $("#sonarCanvas"); c.width = c.clientWidth * 2; c.height = c.clientHeight * 2; }
function sonarDraw(t) {
  const c = $("#sonarCanvas"); if (!c.offsetParent) return;
  if (!c.width || c.width !== c.clientWidth * 2) sonarResize();
  const x = c.getContext("2d"), W = c.width, H = c.height, S = Math.min(W, H) / 2;
  const P = (p) => [W / 2 + p.x * S, H / 2 + p.y * S];
  x.clearRect(0, 0, W, H);
  x.strokeStyle = "rgba(29,255,161,0.07)"; x.lineWidth = 1;
  for (let g = -8; g <= 8; g++) { x.beginPath(); x.moveTo(W / 2 + g * S / 5, 0); x.lineTo(W / 2 + g * S / 5, H); x.stroke(); x.beginPath(); x.moveTo(0, H / 2 + g * S / 5); x.lineTo(W, H / 2 + g * S / 5); x.stroke(); }
  const nodes = sonarState.nodes || [];
  const ap = P(sonarXY("@ap", 0, 1));
  // sweep rings from the access point
  for (let k = 0; k < 3; k++) { const r = ((t * 0.12 + k / 3) % 1) * S * 1.1; x.strokeStyle = "rgba(29,255,161," + (0.22 * (1 - r / (S * 1.1))) + ")"; x.lineWidth = 2; x.beginPath(); x.arc(ap[0], ap[1], r, 0, 6.283); x.stroke(); }
  nodes.forEach((n, i) => {
    const np = P(sonarXY(n.node, i, nodes.length));
    const hot = n.state === "motion", k = Math.min(1, n.score / 100);
    const dx = np[0] - ap[0], dy = np[1] - ap[1], len = Math.hypot(dx, dy), ang = Math.atan2(dy, dx);
    // the sensed zone is the ellipse around the AP-to-node link
    x.save(); x.translate((ap[0] + np[0]) / 2, (ap[1] + np[1]) / 2); x.rotate(ang);
    const pulse = hot ? 0.55 + 0.45 * Math.sin(t * 9) : 1;
    x.fillStyle = hot ? "rgba(255,84,73," + (0.1 + 0.3 * k * pulse) + ")" : "rgba(29,255,161," + (n.state === "clear" ? 0.06 : 0.02) + ")";
    x.strokeStyle = hot ? "rgba(255,84,73,0.8)" : "rgba(29,255,161,0.3)"; x.lineWidth = hot ? 3 : 1.5;
    x.beginPath(); x.ellipse(0, 0, len / 2 + 14, 26 + len * 0.13 + (hot ? k * 26 : 0), 0, 0, 6.283); x.fill(); x.stroke();
    x.restore();
    x.strokeStyle = hot ? "rgba(255,84,73,0.9)" : "rgba(94,246,255,0.35)"; x.lineWidth = 2; x.setLineDash(n.state === "calibrating" ? [8, 8] : []);
    x.beginPath(); x.moveTo(ap[0], ap[1]); x.lineTo(np[0], np[1]); x.stroke(); x.setLineDash([]);
    // node marker
    const col = n.state === "offline" ? "#43727e" : hot ? "#ff5449" : n.state === "calibrating" ? "#ffb84a" : "#1dffa1";
    x.fillStyle = col; x.shadowColor = col; x.shadowBlur = 18;
    x.beginPath(); x.moveTo(np[0], np[1] - 16); x.lineTo(np[0] + 16, np[1]); x.lineTo(np[0], np[1] + 16); x.lineTo(np[0] - 16, np[1]); x.closePath(); x.fill(); x.shadowBlur = 0;
    x.font = "600 26px Rajdhani, sans-serif"; x.fillStyle = "#bdf9ff"; x.fillText(n.node.toUpperCase(), np[0] + 24, np[1] - 4);
    x.font = "20px ui-monospace, monospace"; x.fillStyle = col; x.fillText(n.state.toUpperCase() + (n.state === "offline" ? "" : " · " + Math.round(n.score)), np[0] + 24, np[1] + 22);
  });
  // estimated position of the movement, and where it has been
  if (sonarTrail.length) {
    x.lineWidth = 4; x.strokeStyle = "rgba(255,184,74,0.7)"; x.beginPath();
    sonarTrail.forEach((q, i) => { const pt = P(q); i ? x.lineTo(pt[0], pt[1]) : x.moveTo(pt[0], pt[1]); }); x.stroke();
    const last = P(sonarTrail[sonarTrail.length - 1]), r = 30 + 10 * Math.sin(t * 7);
    x.fillStyle = "rgba(255,84,73,0.35)"; x.strokeStyle = "#ff5449"; x.lineWidth = 3;
    x.beginPath(); x.arc(last[0], last[1], r, 0, 6.283); x.fill(); x.stroke();
    x.font = "600 22px Rajdhani, sans-serif"; x.fillStyle = "#ff5449"; x.fillText("PRESENCE ≈", last[0] + r + 8, last[1] + 7);
  }
  // access point
  x.strokeStyle = "#5ef6ff"; x.lineWidth = 3; x.shadowColor = "#5ef6ff"; x.shadowBlur = 16;
  x.beginPath(); x.arc(ap[0], ap[1], 14, 0, 6.283); x.stroke(); x.beginPath(); x.arc(ap[0], ap[1], 4, 0, 6.283); x.fillStyle = "#5ef6ff"; x.fill(); x.shadowBlur = 0;
  x.font = "600 24px Rajdhani, sans-serif"; x.fillStyle = "#bdf9ff"; x.fillText("ACCESS POINT", ap[0] + 24, ap[1] + 8);
}
function sonarRenderList() {
  const nodes = sonarState.nodes || [], list = $("#sonarList");
  const moving = nodes.filter((n) => n.state === "motion").length;
  $("#sonarMeta").textContent = nodes.length ? nodes.length + " nodes · " + (moving ? moving + " link" + (moving > 1 ? "s" : "") + " disturbed" : "field clear") : "no nodes reporting";
  $("#sonarSrc").textContent = sonarState.demo ? "simulated csi" : sonarState.listening ? "udp " + sonarState.listening : "wi-fi csi";
  const arm = $("#sonarArm");
  arm.classList.toggle("armed", !!sonarState.armed);
  arm.textContent = sonarState.armed ? "◉ ARMED" : "◎ ARM";
  $("#sonarArmNote").textContent = sonarState.armed ? "armed — movement raises an ICE alert" : "disarmed — movement is shown, no alerts raised";
  list.innerHTML = "";
  if (!nodes.length) {
    const h = el("div", "sonar-help");
    h.innerHTML = "No nodes are reporting yet.<br><br>1. Start the console with<br><b>netwatch gui --sonar-udp 0.0.0.0:5566</b><br>2. Flash <b>firmware/esp32s3-csi</b> to each ESP32-S3<br>&nbsp;&nbsp;&nbsp;(or try <b>netwatch sonar-sim</b> first)<br>3. Leave the room still for ~5 s to calibrate";
    list.appendChild(h); return;
  }
  nodes.forEach((n) => {
    const row = el("div", "snode " + n.state);
    const tr = n.trace || [];
    const pts = tr.map((v, i) => (i / Math.max(1, tr.length - 1) * 120).toFixed(1) + "," + (19 - Math.min(100, v) / 100 * 18).toFixed(1)).join(" ");
    row.innerHTML = '<div class="top"><span class="nm">' + esc(n.node.toUpperCase()) + '</span><span class="badge ' + (n.state === "motion" ? "bad" : "") + '">' + esc(n.state.toUpperCase()) + '</span><span class="sc">' + Math.round(n.score) + "</span></div>" +
      '<div class="bar"><i style="width:' + Math.min(100, n.score) + '%"></i></div>' +
      '<div class="meta2"><span>RSSI ' + esc(n.rssi) + " dBm · " + esc(n.fps) + ' fps</span><svg viewBox="0 0 120 20"><polyline fill="none" stroke="' + (n.state === "motion" ? "#ff5449" : "#1dffa1") + '" stroke-width="1.2" points="' + pts + '"/></svg></div>';
    list.appendChild(row);
  });
}
async function sonarPoll() {
  const visible = currentDeck() === "sonar";
  if (!visible && !audioOn) return;
  const r = await get("/api/sonar"); if (!r) return;
  sonarState = r;
  const est = sonarEstimate();
  if (est) { sonarTrail.push(est); if (sonarTrail.length > 36) sonarTrail.shift(); } else if (sonarTrail.length) sonarTrail.splice(0, 2);
  if (visible) sonarRenderList();
}
$("#sonarArm").addEventListener("click", async () => { await post("/api/sonar/arm", { armed: !sonarState.armed }); sonarPoll(); });
(function sonarPointer() {
  const c = $("#sonarCanvas");
  const norm = (e) => { const r = c.getBoundingClientRect(), S = Math.min(r.width, r.height) / 2; return { x: (e.clientX - r.left - r.width / 2) / S, y: (e.clientY - r.top - r.height / 2) / S }; };
  c.addEventListener("pointerdown", (e) => {
    const m = norm(e); let best = null, bd = 0.09;
    Object.keys(sonarPos).forEach((k) => { const d = Math.hypot(sonarPos[k].x - m.x, sonarPos[k].y - m.y); if (d < bd) { bd = d; best = k; } });
    if (best) { sonarDrag = best; c.setPointerCapture(e.pointerId); }
  });
  c.addEventListener("pointermove", (e) => { if (sonarDrag) sonarPos[sonarDrag] = norm(e); });
  c.addEventListener("pointerup", () => { if (sonarDrag) { sonarDrag = null; try { localStorage.setItem("nw.sonarPos", JSON.stringify(sonarPos)); } catch (_) {} } });
})();

// ---- SONAR: where is the movement? a score-weighted blend of the disturbed links ----
let sonarTrail = [], sonarMode = "";
function sonarEstimate() {
  const nodes = sonarState.nodes || [], ap = sonarXY("@ap", 0, 1);
  let wx = 0, wy = 0, w = 0;
  nodes.forEach((n, i) => {
    if (n.state !== "motion") return;
    const q = sonarXY(n.node, i, nodes.length);
    wx += (ap.x + q.x) / 2 * n.score; wy += (ap.y + q.y) / 2 * n.score; w += n.score;
  });
  return w > 0 ? { x: wx / w, y: wy / w } : null;
}

// ---- SONAR 3D view (three.js); falls back to 2D when WebGL is unavailable ----
const s3 = { r: null, failed: false, az: 0.9, el: 0.5, dist: 11.5, drag: null, lastUser: 0, sig: "", objs: {} };
const S3 = 4.4; // map units -> metres-ish
function textSprite(text, color) {
  const c = document.createElement("canvas"); c.width = 512; c.height = 96;
  const x = c.getContext("2d"); x.font = "600 46px Rajdhani, sans-serif"; x.fillStyle = color; x.shadowColor = color; x.shadowBlur = 12; x.fillText(text, 8, 62);
  const sp = new THREE.Sprite(new THREE.SpriteMaterial({ map: new THREE.CanvasTexture(c), transparent: true, depthWrite: false }));
  sp.scale.set(3.4, 0.64, 1); sp.center.set(0, 0.5); return sp;
}
function sonar3dInit() {
  if (s3.r) return true;
  if (s3.failed || typeof THREE === "undefined") { s3.failed = true; return false; }
  try {
    const host = $("#sonar3d");
    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
    host.appendChild(renderer.domElement);
    const scene = new THREE.Scene(), camera = new THREE.PerspectiveCamera(46, 1, 0.1, 100);
    const grid = new THREE.GridHelper(10, 20, 0x1dffa1, 0x0f5a48); grid.material.transparent = true; grid.material.opacity = 0.5; scene.add(grid);
    const room = new THREE.LineSegments(new THREE.EdgesGeometry(new THREE.BoxGeometry(10, 3, 10)), new THREE.LineBasicMaterial({ color: 0xff5449, transparent: true, opacity: 0.4 }));
    room.position.y = 1.5; scene.add(room);
    const ap = new THREE.Group();
    ap.add(new THREE.Mesh(new THREE.SphereGeometry(0.16, 20, 14), new THREE.MeshBasicMaterial({ color: 0x5ef6ff })));
    ap.add(new THREE.Mesh(new THREE.SphereGeometry(0.3, 20, 14), new THREE.MeshBasicMaterial({ color: 0x5ef6ff, wireframe: true, transparent: true, opacity: 0.35 })));
    const apLabel = textSprite("ACCESS POINT", "#bdf9ff"); apLabel.position.set(0.4, 0.5, 0); ap.add(apLabel);
    scene.add(ap);
    const rings = [0, 1, 2].map(() => { const m = new THREE.Mesh(new THREE.RingGeometry(0.97, 1, 72), new THREE.MeshBasicMaterial({ color: 0x1dffa1, transparent: true, side: THREE.DoubleSide, depthWrite: false })); m.rotation.x = -Math.PI / 2; scene.add(m); return m; });
    const blob = new THREE.Group();
    blob.add(new THREE.Mesh(new THREE.CylinderGeometry(0.3, 0.3, 1.5, 20, 1, true), new THREE.MeshBasicMaterial({ color: 0xff5449, transparent: true, opacity: 0.3, side: THREE.DoubleSide, depthWrite: false })));
    const head = new THREE.Mesh(new THREE.SphereGeometry(0.3, 18, 12), new THREE.MeshBasicMaterial({ color: 0xff5449, wireframe: true, transparent: true, opacity: 0.8 })); head.position.y = 1.05; blob.add(head);
    const foot = new THREE.Mesh(new THREE.RingGeometry(0.5, 0.56, 48), new THREE.MeshBasicMaterial({ color: 0xff5449, side: THREE.DoubleSide, transparent: true })); foot.rotation.x = -Math.PI / 2; foot.position.y = -0.72; blob.add(foot);
    const blobLabel = textSprite("PRESENCE ≈", "#ff5449"); blobLabel.position.set(0.45, 1.6, 0); blob.add(blobLabel);
    blob.visible = false; scene.add(blob);
    const trail = new THREE.Line(new THREE.BufferGeometry(), new THREE.LineBasicMaterial({ color: 0xffb84a, transparent: true, opacity: 0.85 })); scene.add(trail);
    const dyn = new THREE.Group(); scene.add(dyn);
    host.addEventListener("pointerdown", (e) => { s3.drag = { x: e.clientX, y: e.clientY }; host.setPointerCapture(e.pointerId); });
    host.addEventListener("pointermove", (e) => { if (!s3.drag) return; s3.az += (e.clientX - s3.drag.x) * 0.006; s3.el = Math.max(0.12, Math.min(1.45, s3.el + (e.clientY - s3.drag.y) * 0.005)); s3.drag = { x: e.clientX, y: e.clientY }; s3.lastUser = performance.now(); });
    host.addEventListener("pointerup", () => { s3.drag = null; });
    host.addEventListener("wheel", (e) => { e.preventDefault(); s3.dist = Math.max(6, Math.min(26, s3.dist + e.deltaY * 0.01)); s3.lastUser = performance.now(); }, { passive: false });
    s3.r = { renderer, scene, camera, ap, rings, blob, trail, dyn, host, w: 0, h: 0 };
    return true;
  } catch (e) { s3.failed = true; return false; }
}
function sonar3dNode(name) {
  const g = new THREE.Group();
  const marker = new THREE.Mesh(new THREE.OctahedronGeometry(0.24), new THREE.MeshBasicMaterial({ color: 0x1dffa1 }));
  const link = new THREE.Line(new THREE.BufferGeometry(), new THREE.LineBasicMaterial({ color: 0x5ef6ff, transparent: true, opacity: 0.6 }));
  const geo = new THREE.SphereGeometry(1, 28, 18);
  const zone = new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ color: 0x1dffa1, transparent: true, opacity: 0.08, depthWrite: false }));
  const wire = new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ color: 0x1dffa1, wireframe: true, transparent: true, opacity: 0.16, depthWrite: false }));
  g.add(marker, link, zone, wire);
  return { g, marker, link, zone, wire, label: null, text: "" };
}
function sonar3dRender(t) {
  const R = s3.r; if (!R || !R.host.offsetParent) return;
  const w = R.host.clientWidth, h = R.host.clientHeight;
  if (w !== R.w || h !== R.h) { R.w = w; R.h = h; R.renderer.setSize(w, h); R.camera.aspect = w / h; R.camera.updateProjectionMatrix(); }
  if (!s3.drag && performance.now() - s3.lastUser > 4000) s3.az += 0.0016;
  R.camera.position.set(Math.cos(s3.az) * Math.cos(s3.el) * s3.dist, Math.sin(s3.el) * s3.dist, Math.sin(s3.az) * Math.cos(s3.el) * s3.dist);
  R.camera.lookAt(0, 0.8, 0);

  const nodes = sonarState.nodes || [], apP = sonarXY("@ap", 0, 1), H = 1.1;
  const A = new THREE.Vector3(apP.x * S3, H + 0.5, apP.y * S3);
  R.ap.position.copy(A);
  R.rings.forEach((m, k) => { const f = (t * 0.12 + k / 3) % 1; m.scale.setScalar(0.2 + f * 5.5); m.position.set(A.x, 0.02, A.z); m.material.opacity = 0.5 * (1 - f); });

  const sig = nodes.map((n) => n.node).join("|");
  if (sig !== s3.sig) { s3.sig = sig; while (R.dyn.children.length) R.dyn.remove(R.dyn.children[0]); s3.objs = {}; nodes.forEach((n) => { s3.objs[n.node] = sonar3dNode(n.node); R.dyn.add(s3.objs[n.node].g); }); }
  nodes.forEach((n, i) => {
    const o = s3.objs[n.node], q = sonarXY(n.node, i, nodes.length);
    const N = new THREE.Vector3(q.x * S3, H, q.y * S3);
    const hot = n.state === "motion", k = Math.min(1, n.score / 100), pulse = hot ? 0.55 + 0.45 * Math.sin(t * 9) : 1;
    const col = n.state === "offline" ? 0x43727e : hot ? 0xff5449 : n.state === "calibrating" ? 0xffb84a : 0x1dffa1;
    o.marker.position.copy(N); o.marker.rotation.y = t * 1.2; o.marker.material.color.setHex(col);
    o.link.geometry.setFromPoints([A, N]); o.link.material.color.setHex(hot ? 0xff5449 : 0x5ef6ff);
    const mid = A.clone().add(N).multiplyScalar(0.5), dx = N.x - A.x, dz = N.z - A.z, len = Math.hypot(dx, dz);
    [o.zone, o.wire].forEach((m) => { m.position.copy(mid); m.rotation.y = -Math.atan2(dz, dx); m.scale.set(len / 2 + 0.3, 0.6 + (hot ? k * 0.4 : 0), 0.55 + len * 0.07 + (hot ? k * 0.45 : 0)); m.material.color.setHex(hot ? 0xff5449 : 0x1dffa1); });
    o.zone.material.opacity = hot ? 0.14 + 0.26 * k * pulse : (n.state === "clear" ? 0.07 : 0.03);
    o.wire.material.opacity = hot ? 0.45 : 0.14;
    const text = n.node.toUpperCase() + " · " + n.state.toUpperCase() + (n.state === "offline" ? "" : " " + Math.round(n.score));
    if (text !== o.text) { if (o.label) o.g.remove(o.label); o.text = text; o.label = textSprite(text, "#" + col.toString(16).padStart(6, "0")); o.g.add(o.label); }
    o.label.position.set(N.x + 0.35, N.y + 0.45, N.z);
  });

  const last = sonarTrail[sonarTrail.length - 1];
  R.blob.visible = !!last;
  if (last) { R.blob.position.set(last.x * S3, 0.75, last.y * S3); const sc = 1 + 0.12 * Math.sin(t * 7); R.blob.scale.set(sc, 1, sc); }
  R.trail.geometry.setFromPoints(sonarTrail.map((q) => new THREE.Vector3(q.x * S3, 0.05, q.y * S3)));
  R.renderer.render(R.scene, R.camera);
}
function sonarPref() { try { return localStorage.getItem("nw.sonarMode") || "3d"; } catch (_) { return "3d"; } }
function setSonarMode(m) {
  if (m === "3d" && !sonar3dInit()) m = "2d";
  sonarMode = m;
  document.querySelector(".sonar-map").dataset.mode = m;
  const b3 = $("#sonarMode3d"), b2 = $("#sonarMode2d");
  b3.classList.toggle("on", m === "3d"); b2.classList.toggle("on", m === "2d");
  if (s3.failed) { b3.disabled = true; b3.title = "3D needs WebGL, which this browser is not providing"; }
  $("#sonarHint").textContent = m === "3d" ? "drag to orbit · scroll to zoom · switch to 2D to arrange nodes" : "drag the access point and nodes to match your room";
  if (m === "2d") sonarResize();
}
document.querySelectorAll(".sonar-mode .hs-btn").forEach((b) => b.addEventListener("click", () => {
  setSonarMode(b.dataset.mode);
  if (!(b.dataset.mode === "3d" && s3.failed)) { try { localStorage.setItem("nw.sonarMode", sonarMode); } catch (_) {} }
}));

// ================= COMMANDS: typed or spoken =================
const DECK_WORDS = { watchtower: "netwatch", watch: "netwatch", breach: "breach", "kill chain": "killchain", killchain: "killchain", "attack": "attack", "att&ck": "attack", mitre: "attack", grid: "grid", globe: "grid", ar: "ar", optic: "ar", sonar: "sonar" };
let pendingConfirm = null;
function findDevice(words) {
  const w = words.toLowerCase().replace(/^the\s+/, "").trim(); if (!w) return null;
  const flat = w.replace(/[^a-z0-9.]/g, "");
  const devs = STATE.devices || [];
  if (/gateway|router/.test(w)) return devs.find((d) => d.gateway) || null;
  return devs.find((d) => d.ip === flat) || devs.find((d) => d.ip.endsWith("." + flat)) ||
    devs.find((d) => d.hostname && d.hostname.toLowerCase().replace(/[^a-z0-9]/g, "").includes(flat.replace(/\./g, ""))) || null;
}
// Returns a reply string when the text was a command, or null to hand it to the Copilot.
async function runCommand(text) {
  const t = text.toLowerCase().replace(/[.!?]+$/, "").trim();
  let m;
  if (pendingConfirm) {
    const p = pendingConfirm; pendingConfirm = null;
    if (/^(confirm|yes|do it|execute)$/.test(t) && Date.now() < p.until) { const r = await post("/api/blackwall", { ip: p.ip, action: "quarantine" }); return r && r.ok ? "BLACKWALL engaged. " + p.ip + " is quarantined." : "Quarantine failed: " + ((r && r.error) || "backend unreachable"); }
    if (/^(cancel|no|abort)$/.test(t)) return "Cancelled.";
  }
  if (/^(sweep|scan)( the)?( network| segment)?$/.test(t)) { if (replay) return "Go live first."; await post("/api/sweep"); return "Sweeping the segment."; }
  if ((m = t.match(/^(?:switch to|open|show|go to)\s+(?:the\s+)?(.+?)(?:\s+deck)?$/)) && DECK_WORDS[m[1]]) { selectDeck(DECK_WORDS[m[1]]); return "Switching to " + m[1].toUpperCase() + "."; }
  if ((m = t.match(/^(?:select|target|lock on(?: to)?)\s+(.+)$/))) { const d = findDevice(m[1]); if (!d) return "No host matches " + m[1] + "."; selectDeck("netwatch"); selectDevice(d.ip); return "Locked on " + (d.hostname || d.ip) + ". Threat " + threatOf(d) + " of 100."; }
  if ((m = t.match(/^(untrust|trust)(?:\s+(.+))?$/))) { const d = m[2] ? findDevice(m[2]) : STATE.devices.find((x) => x.ip === selectedIP); if (!d) return "Select a host first."; await post("/api/trust", { ip: d.ip, trusted: m[1] === "trust" }); return (d.hostname || d.ip) + (m[1] === "trust" ? " marked trusted." : " marked unverified."); }
  if ((m = t.match(/^(?:quarantine|isolate|blackwall)(?:\s+(.+))?$/))) { const d = m[1] ? findDevice(m[1]) : STATE.devices.find((x) => x.ip === selectedIP); if (!d) return "Select a host first."; if (d.self || d.gateway) return "Refusing: that is " + (d.self ? "this machine" : "the gateway") + "."; pendingConfirm = { ip: d.ip, until: Date.now() + 15000 }; return "Quarantine " + (d.hostname || d.ip) + " at the firewall? Say or type confirm."; }
  if ((m = t.match(/^release(?:\s+(.+))?$/))) { const d = m[1] ? findDevice(m[1]) : STATE.devices.find((x) => x.ip === selectedIP); if (!d) return "Select a host first."; await post("/api/blackwall", { ip: d.ip, action: "release" }); return (d.hostname || d.ip) + " released."; }
  if (/^(go )?live$/.test(t)) { goLive(); return "Back to live."; }
  if ((m = t.match(/^(?:rewind|go back)\s+(\d+)\s+min/))) { const target = Date.now() - (+m[1]) * 60000; let i = 0; hist.forEach((h, k) => { if (h.ts <= target) i = k; }); await scrubTo(i); return "Replaying " + m[1] + " minutes ago."; }
  if ((m = t.match(/^(disarm|arm)(?: the)? sonar$/))) { await post("/api/sonar/arm", { armed: m[1] === "arm" }); sonarPoll(); return "SONAR " + (m[1] === "arm" ? "armed." : "disarmed."); }
  if (/^(audio on|unmute)$/.test(t)) { setAudio(true); return "Spatial audio on."; }
  if (/^(audio off|mute)$/.test(t)) { setAudio(false); return "Spatial audio off."; }
  return null;
}
function speak(text) {
  if (!("speechSynthesis" in window)) return;
  const u = new SpeechSynthesisUtterance(String(text).slice(0, 320)); u.rate = 1.05; u.pitch = 0.7;
  window.speechSynthesis.cancel(); window.speechSynthesis.speak(u);
}
(function voice() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition, btn = $("#micBtn");
  if (!SR) { btn.disabled = true; btn.title = "speech recognition is not available in this browser — type commands instead"; return; }
  const rec = new SR(); rec.lang = "en-US"; rec.interimResults = false; rec.maxAlternatives = 1;
  let on = false;
  rec.onresult = (e) => { $("#copilotInput").value = e.results[0][0].transcript; sendCopilot(true); };
  rec.onend = () => { on = false; btn.classList.remove("listening"); btn.textContent = "◉ VOICE"; };
  rec.onerror = (e) => { status("voice: " + e.error); };
  btn.addEventListener("click", () => {
    if (on) { rec.stop(); return; }
    try { rec.start(); on = true; btn.classList.add("listening"); btn.textContent = "◉ LISTENING"; } catch (_) {}
  });
})();

// ================= SPATIAL AUDIO: hear where an alert comes from =================
let actx = null, audioOn = false, lastAlertTs = null;
function audioPosFor(source) {
  const n = (sonarState.nodes || []).find((x) => x.node === source);
  if (n && sonarPos[source]) return { x: sonarPos[source].x * 4, z: sonarPos[source].y * 4 };
  let h = 2166136261; for (const ch of String(source)) { h ^= ch.charCodeAt(0); h = Math.imul(h, 16777619); }
  h ^= h >>> 13; h = Math.imul(h, 0x5bd1e995); h ^= h >>> 15; // spread similar addresses around the listener
  const a = ((h >>> 0) % 360) * Math.PI / 180;
  return { x: Math.cos(a) * 3, z: Math.sin(a) * 3 };
}
function playAlert(a) {
  if (!audioOn || !actx) return null;
  const pos = audioPosFor(a.source), now = actx.currentTime;
  const pan = new PannerNode(actx, { panningModel: "HRTF", distanceModel: "inverse", positionX: pos.x, positionY: 0, positionZ: pos.z });
  const gain = actx.createGain(); gain.connect(pan).connect(actx.destination);
  const tones = a.severity === "CRITICAL" ? [[880, 0, "sawtooth"], [587, 0.16, "sawtooth"], [880, 0.34, "sawtooth"], [587, 0.5, "sawtooth"]] : a.severity === "WARN" ? [[660, 0, "square"], [660, 0.18, "square"]] : [[520, 0, "sine"]];
  tones.forEach(([f, at, type]) => {
    const o = actx.createOscillator(); o.type = type; o.frequency.value = f; o.connect(gain);
    gain.gain.setValueAtTime(0.0001, now + at); gain.gain.exponentialRampToValueAtTime(0.25, now + at + 0.02); gain.gain.exponentialRampToValueAtTime(0.0001, now + at + 0.14);
    o.start(now + at); o.stop(now + at + 0.15);
  });
  return pos;
}
function setAudio(on) {
  audioOn = on;
  if (on && !actx) { const AC = window.AudioContext || window.webkitAudioContext; if (AC) actx = new AC(); }
  if (on && actx && actx.state === "suspended") actx.resume();
  const b = $("#audioBtn"); b.classList.toggle("on", on); b.textContent = on ? "AUDIO ON" : "AUDIO OFF";
}
$("#audioBtn").addEventListener("click", () => { setAudio(!audioOn); if (audioOn) playAlert({ severity: "INFO", source: "self" }); });
function onNewAlerts(snap) {
  const alerts = snap.alerts || [], newest = alerts.reduce((mx, a) => Math.max(mx, a.ts || 0), 0);
  if (lastAlertTs === null) { lastAlertTs = newest; return; }
  alerts.filter((a) => a.ts > lastAlertTs).slice(0, 3).forEach(playAlert);
  lastAlertTs = Math.max(lastAlertTs, newest);
}

// test hooks (also handy from the browser console)
window.netwatch = { sonarMode: () => sonarMode, setSonarMode, sonarTrail: () => sonarTrail.length, runCommand, audioPosFor, playAlert, setAudio, scrubTo, goLive, state: () => STATE };

// ---------- boot ----------
initNeural();
runBoot();
initTicker();
killchainDeck.init();
attackDeck.init();
connect();
setTimeout(() => { if (!live) loadMock(); }, 600);
setTimeout(loadHistory, 900);
setInterval(loadHistory, 10000);
setInterval(sonarPoll, 400);
sysPoll(); setInterval(sysPoll, 1500);
(function sonarLoop() {
  if (currentDeck() === "sonar") { const t = performance.now() / 1000; if (sonarMode === "3d") sonar3dRender(t); else sonarDraw(t); }
  requestAnimationFrame(sonarLoop);
})();
