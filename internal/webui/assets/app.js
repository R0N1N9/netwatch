"use strict";

// ---------- state ----------
let STATE = { meta: {}, devices: [], alerts: [], stats: {} };
let selectedIP = null;
let live = false;
let tickerRefresh = null, tickerPrimed = false;
let liveState = null, replay = false;
// extension points for senses.js and reflex.js
const EXTRA_COMMANDS = [], ALERT_HOOKS = [], RENDER_HOOKS = [];
let operatorLevel = "";

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
  if (name === "sonar") { bdRefresh(); setTimeout(sleepPoll, 300); }
  if (name === "sonar") setTimeout(() => { if (!sonarMode) setSonarMode(sonarPref()); sonarResize(); }, 40);
  if (MOD_DECKS[name]) setTimeout(MOD_DECKS[name], 30);
}
function currentDeck() { return document.querySelector(".tab.active").dataset.deck; }

// ---------- API ----------
async function post(path, body) {
  try {
    const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
    if (!r.ok) throw new Error("HTTP " + r.status);
    const j = await r.json();
    if (j && j.ok === false && j.error) status(j.error);
    return j;
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
  renderGate(m.gate);
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
  RENDER_HOOKS.forEach((f) => f());
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
    const row = el("div", "alert sev-" + cls + (cls === "crit" ? " crit" : "") + (a.acked ? " acked" : ""));
    if (!a.acked) { row.title = "click to acknowledge"; row.onclick = () => { if (!replay) post("/api/ack", { ts: a.ts, source: a.source, kind: a.kind }); }; }
    row.appendChild(el("span", "sev", a.severity || "INFO"));
    const msg = el("div", "msg"); msg.textContent = a.message;
    const t = el("time"); t.textContent = (a.kind || "") + " · " + fmtTime(a.ts);
    msg.appendChild(t); row.appendChild(msg);
    if (a.clip) { const b = el("button", "hs-btn", "▶ ROOM"); b.title = "replay what SONAR sensed in the room when this happened"; b.onclick = (e) => { e.stopPropagation(); selectDeck("sonar"); bdOpen(a.clip); }; row.appendChild(b); }
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
  $("#gateSet").disabled = !d || d.self || d.gateway;
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
  const order = ["netwatch", "breach", "killchain", "attack", "grid", "netdive", "ar", "sonar", "mobile", "decoy", "cortex", "spectrum", "oracle", "pulse", "ward", "quantum", "ledger", "dojo", "reflex"];
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
    { side: "blue", ttl: "Detector fire drill", att: "self-test", type: "api", endpoint: "/api/kc/drill", btn: "DRILL" },
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
    { side: "blue", ttl: "Surface diff since yesterday", att: "DS0009", type: "api", endpoint: "/api/kc/surface", btn: "DIFF" },
  ]},
  { name: "C2", attck: "T1071 · T1571 · T1572", actions: [
    { side: "red", ttl: "Beacon-profile brief", att: "T1071", type: "info", info: ["Beacons = periodic low-jitter callbacks on app-layer protocols / odd ports / DNS tunnels.", "PLANNING ONLY — no C2 implant is generated. Hunt for it with the BLUE tools."] },
    { side: "blue", ttl: "Established peers", att: "T1071", type: "api", endpoint: "/api/kc/connections", btn: "HUNT" },
    { side: "blue", ttl: "Non-standard listeners", att: "T1571", type: "api", endpoint: "/api/kc/listeners", btn: "AUDIT" },
    { side: "blue", ttl: "Process lineage", att: "T1071", type: "api", endpoint: "/api/kc/lineage", btn: "TRACE" },
  ]},
  { name: "ACTIONS", attck: "T1041 · T1048 · T1486", actions: [
    { side: "red", ttl: "Exfil scenario brief", att: "T1041", type: "info", info: ["Exfil over C2 channel, over alternate protocol (DNS/ICMP), or to cloud storage.", "PLANNING ONLY. Detect it below by watching large/odd outbound flows."] },
    { side: "blue", ttl: "Exfil watch (public peers)", att: "T1048", type: "api", endpoint: "/api/kc/connections", btn: "WATCH" },
  ]},
];
const ATTACK = [
  { name: "RECON", attck: "TA0043", actions: [
    { side: "red", ttl: "Active scanning", att: "T1595", type: "api", endpoint: "/api/scan", needsTarget: true, btn: "PROBE" },
    { side: "blue", ttl: "Detector fire drill", att: "self-test", type: "api", endpoint: "/api/kc/drill", btn: "DRILL" },
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
    { side: "blue", ttl: "Surface diff since yesterday", att: "DS0009", type: "api", endpoint: "/api/kc/surface", btn: "DIFF" },
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
    { side: "blue", ttl: "Process lineage", att: "T1071", type: "api", endpoint: "/api/kc/lineage", btn: "TRACE" },
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
  const res = await post("/api/globe");
  const pts = (res && res.points) || [];
  if (globe) while (globe.dyn.children.length) globe.dyn.remove(globe.dyn.children[0]);
  pts.forEach((p) => {
    if (!globe) return; // no WebGL: the egress list below still works
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
  const lin = await get("/api/lineage"), owners = {};
  ((lin && lin.conns) || []).forEach((c) => { (owners[c.remote] = owners[c.remote] || []).push(c.comm + " · pid " + c.pid); });
  renderGlobeList(pts, owners);
}
function renderGlobeList(pts, owners) {
  const list = $("#globeList"); list.innerHTML = "";
  if (!pts.length) { list.appendChild(el("div", "empty", "no external connections")); return; }
  pts.slice().sort((a, b) => b.bytes - a.bytes).forEach((p) => {
    const sus = !p.sni || p.port === 8443;
    const n = el("div", "gnode" + (sus ? " sus" : ""));
    n.innerHTML = '<span class="d">' + esc(p.dst) + ":" + esc(p.port) + '</span><span class="r">' + esc(p.region) + " · " + fmtBytes(p.bytes) + "</span>" +
      '<span class="s">' + (p.sni ? esc(p.sni) : "⚠ no SNI — opaque egress") + "</span>" +
      ((owners || {})[p.dst + ":" + p.port] ? '<span class="p">← ' + esc(owners[p.dst + ":" + p.port].join(", ")) + "</span>" : "");
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
// Gauges show what is LEFT, like a cyberdeck's RAM: full when the machine is
// idle, draining from the right as it is used, red when nearly spent.
function fillBar(id, segs, freePct) {
  const bar = $(id);
  if (bar.children.length !== segs) { bar.innerHTML = ""; for (let i = 0; i < segs; i++) { const seg = el("i", "off"); seg.style.animationDelay = (i * 0.03) + "s"; bar.appendChild(seg); } }
  const left = Math.min(segs, Math.max(freePct > 0 ? 1 : 0, Math.round(freePct / 100 * segs))), low = freePct <= 15;
  [...bar.children].forEach((seg, i) => { seg.className = i < left ? (low ? "hot" : "") : "off"; });
}
async function sysPoll() {
  const r = await get("/api/sys");
  if (!r || !r.ok) { $("#ramText").textContent = "n/a"; $("#cpuText").textContent = "n/a"; return; }
  const gb = (b) => (b / 1073741824).toFixed(1);
  const ramFree = Math.max(0, 100 - r.memPct), cpuFree = Math.max(0, 100 - r.cpu);
  $("#ramText").textContent = gb(r.memTotal - r.memUsed) + "/" + gb(r.memTotal) + " GB";
  $("#cpuText").textContent = Math.round(cpuFree) + "%";
  $("#cpuCores").textContent = r.cores;
  $("#cpuLoad").textContent = r.load1.toFixed(2);
  fillBar("#ramBar", 16, ramFree);
  fillBar("#cpuBar", 12, cpuFree);
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
let liveSonar = sonarState, sonarShown = {}, layoutSynced = false, layoutSent = 0;
const dance = { clips: [], recSince: 0, play: null };
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
  // floor plan and tripwires
  x.lineCap = "round";
  room.walls.forEach((w) => { const a = P(w.a), b = P(w.b); x.strokeStyle = "rgba(189,249,255,0.7)"; x.lineWidth = 6; x.beginPath(); x.moveTo(a[0], a[1]); x.lineTo(b[0], b[1]); x.stroke(); });
  room.wires.forEach((w) => {
    const a = P(w.a), b = P(w.b);
    x.strokeStyle = "#ff5449"; x.lineWidth = 4; x.setLineDash([14, 10]); x.lineDashOffset = -t * 30; x.shadowColor = "#ff5449"; x.shadowBlur = 12;
    x.beginPath(); x.moveTo(a[0], a[1]); x.lineTo(b[0], b[1]); x.stroke(); x.setLineDash([]); x.shadowBlur = 0;
    x.font = "600 20px Rajdhani, sans-serif"; x.fillStyle = "#ff5449"; x.fillText("⟂ " + (w.name || "tripwire").toUpperCase(), (a[0] + b[0]) / 2 + 10, (a[1] + b[1]) / 2 - 10);
  });
  if (sonarDrawing) { const a = P(sonarDrawing.a), b = P(sonarDrawing.b); x.strokeStyle = "#ffb84a"; x.lineWidth = 4; x.beginPath(); x.moveTo(a[0], a[1]); x.lineTo(b[0], b[1]); x.stroke(); }
  x.lineCap = "butt";
  // sweep rings from the access point
  for (let k = 0; k < 3; k++) { const r = ((t * 0.12 + k / 3) % 1) * S * 1.1; x.strokeStyle = "rgba(29,255,161," + (0.22 * (1 - r / (S * 1.1))) + ")"; x.lineWidth = 2; x.beginPath(); x.arc(ap[0], ap[1], r, 0, 6.283); x.stroke(); }
  nodes.forEach((n, i) => {
    const shown = sonarNodePos(n, i, nodes.length); sonarShown[n.node] = shown;
    const np = P(shown);
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
    x.font = "20px ui-monospace, monospace"; x.fillStyle = col; x.fillText(n.state.toUpperCase() + (n.state === "offline" ? "" : " · " + Math.round(n.score)) + (n.distM ? " · " + n.distM.toFixed(1) + " m" : ""), np[0] + 24, np[1] + 22);
    if (n.breath) { x.fillStyle = "#ffb84a"; x.fillText("RESP " + n.breath + "/min — someone still", np[0] + 24, np[1] + 46); }
  });
  // located network hosts
  (sonarHosts || []).forEach((hst) => {
    const pt = P(hst), dv = (STATE.devices || []).find((d) => d.ip === hst.ip), hot = dv && threatOf(dv) >= 50;
    const col = hot ? "#ff5449" : "#5ef6ff";
    x.strokeStyle = col; x.fillStyle = col; x.lineWidth = 3; x.shadowColor = col; x.shadowBlur = 10;
    x.beginPath(); x.rect(pt[0] - 9, pt[1] - 9, 18, 18); hst.nodes >= 3 ? x.fill() : x.stroke(); x.shadowBlur = 0;
    x.font = "600 21px Rajdhani, sans-serif"; x.fillText(hst.name.toUpperCase(), pt[0] + 16, pt[1] + 7);
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
  $("#sonarMeta").textContent = (nodes.length ? nodes.length + " nodes · " + (moving ? moving + " link" + (moving > 1 ? "s" : "") + " disturbed" : "field clear") : "no nodes reporting") +
    (!dance.play && sonarPeople ? " · ≈" + sonarPeople + " present" : "") + (!dance.play && sonarHosts.length ? " · " + sonarHosts.length + " hosts located" : "");
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
    const row = el("div", "snode " + n.state + (n.node === sleepNode ? " picked" : ""));
    row.addEventListener("click", () => { sleepNode = n.node; sleepPoll(); sonarRenderList(); });
    const tr = n.trace || [];
    const pts = tr.map((v, i) => (i / Math.max(1, tr.length - 1) * 120).toFixed(1) + "," + (19 - Math.min(100, v) / 100 * 18).toFixed(1)).join(" ");
    row.innerHTML = '<div class="top"><span class="nm">' + esc(n.node.toUpperCase()) + '</span><span class="badge ' + (n.state === "motion" ? "bad" : "") + '">' + esc(n.state.toUpperCase()) + "</span>" + (n.breath ? '<span class="badge resp">RESP ' + esc(n.breath) + "/MIN</span>" : "") + (n.activity && n.activity !== "empty" ? '<span class="badge ' + (n.activity === "fall?" ? "bad" : "act") + '">' + esc(n.activity.toUpperCase()) + "</span>" : "") + '<span class="sc">' + Math.round(n.score) + "</span></div>" +
      '<div class="bar"><i style="width:' + Math.min(100, n.score) + '%"></i></div>' +
      '<div class="meta2"><span>' + (n.rssi == null ? "recorded" : "RSSI " + esc(n.rssi) + " dBm · " + esc(n.fps) + " fps") + (n.distM ? " · " + esc(n.distM.toFixed(1)) + " m" : "") + '</span><svg viewBox="0 0 120 20"><polyline fill="none" stroke="' + (n.state === "motion" ? "#ff5449" : "#1dffa1") + '" stroke-width="1.2" points="' + pts + '"/></svg></div>';
    list.appendChild(row);
  });
}
async function sonarPoll() {
  const visible = currentDeck() === "sonar";
  if (!visible && !audioOn) return;
  const r = await get("/api/sonar"); if (!r) return;
  liveSonar = r;
  if (!sonarDrawing && !roomDirty) { room.walls = r.walls || []; room.wires = r.wires || []; renderWires(); }
  sonarHosts = r.hosts || []; sonarPeople = r.people || 0;
  if (!layoutSynced) { // adopt the server's room map once, or seed it with ours
    layoutSynced = true;
    if (r.layout && Object.keys(r.layout).length) sonarPos = Object.assign({}, r.layout);
  }
  if (dance.play) return;
  sonarState = r;
  const est = sonarEstimate();
  if (est) { sonarTrail.push(est); if (sonarTrail.length > 36) sonarTrail.shift(); } else if (sonarTrail.length) sonarTrail.splice(0, 2);
  if (visible) sonarRenderList();
  if (visible && Object.keys(sonarPos).length !== layoutSent) pushLayout();
}
function pushLayout() { layoutSent = Object.keys(sonarPos).length; post("/api/sonar/layout", { layout: sonarPos }); }
// A node that reports a Wi-Fi range sits at that distance from the access point; you choose the bearing.
function sonarNodePos(n, i, total) {
  const base = sonarXY(n.node, i, total);
  if (!n.distM) return base;
  const ap = sonarXY("@ap", 0, 1), dx = base.x - ap.x, dy = base.y - ap.y, len = Math.hypot(dx, dy) || 1;
  const r = n.distM / S3;
  return { x: ap.x + (len > 0.001 ? dx / len : 1) * r, y: ap.y + (len > 0.001 ? dy / len : 0) * r };
}
$("#sonarArm").addEventListener("click", async () => { await post("/api/sonar/arm", { armed: !sonarState.armed }); sonarPoll(); });
(function sonarPointer() {
  const c = $("#sonarCanvas");
  const norm = (e) => { const r = c.getBoundingClientRect(), S = Math.min(r.width, r.height) / 2; return { x: (e.clientX - r.left - r.width / 2) / S, y: (e.clientY - r.top - r.height / 2) / S }; };
  c.addEventListener("pointerdown", (e) => {
    if (dance.play) return;
    const m = norm(e);
    if (sonarTool !== "move") { sonarDrawing = { a: m, b: m }; c.setPointerCapture(e.pointerId); return; }
    let best = null, near = 0.09;
    Object.keys(sonarPos).forEach((k) => { const at = sonarShown[k] || sonarPos[k], d = Math.hypot(at.x - m.x, at.y - m.y); if (d < near) { near = d; best = k; } });
    if (best) { sonarDrag = best; c.setPointerCapture(e.pointerId); }
  });
  c.addEventListener("pointermove", (e) => { if (sonarDrawing) sonarDrawing.b = norm(e); else if (sonarDrag) sonarPos[sonarDrag] = norm(e); });
  c.addEventListener("pointerup", () => {
    if (sonarDrawing) { const seg = sonarDrawing; sonarDrawing = null; addSegment(sonarTool, seg.a, seg.b); return; }
    if (sonarDrag) { sonarDrag = null; try { localStorage.setItem("nw.sonarPos", JSON.stringify(sonarPos)); } catch (_) {} pushLayout(); }
  });
})();

// ---- floor plan walls and tripwires ----
const room = { walls: [], wires: [] };
let sonarTool = "move", sonarDrawing = null, roomDirty = false, sonarHosts = [], sonarPeople = 0;
async function saveRoom() { roomDirty = true; await post("/api/sonar/map", { walls: room.walls, wires: room.wires }); roomDirty = false; renderWires(); }
function addSegment(kind, a, b, name) {
  if (Math.hypot(a.x - b.x, a.y - b.y) < 0.04) return false;
  if (kind === "wall") room.walls.push({ id: "", name: "", a, b });
  else {
    if (name == null) { try { name = window.prompt("Name this tripwire", "tripwire " + (room.wires.length + 1)); } catch (_) { name = null; } }
    room.wires.push({ id: "", name: name || "tripwire " + (room.wires.length + 1), a, b });
  }
  saveRoom(); return true;
}
function renderWires() {
  const box = $("#wireList"); box.innerHTML = "";
  room.wires.forEach((w, i) => {
    const row = el("div", "wire-row"); row.appendChild(el("span", null, "⟂ " + (w.name || "tripwire")));
    const del = el("button", null, "✕"); del.title = "remove this tripwire";
    del.addEventListener("click", () => { room.wires.splice(i, 1); saveRoom(); });
    row.appendChild(del); box.appendChild(row);
  });
}
document.querySelectorAll("#sonarTools [data-tool]").forEach((btn) => btn.addEventListener("click", () => {
  sonarTool = btn.dataset.tool;
  document.querySelectorAll("#sonarTools [data-tool]").forEach((x) => x.classList.toggle("on", x === btn));
  document.querySelector(".sonar-map").dataset.tool = sonarTool;
}));
$("#wallUndo").addEventListener("click", () => { if (room.walls.length) { room.walls.pop(); saveRoom(); } });

// ---- respiration log (sleep tracking) ----
let sleepNode = "";
async function sleepPoll() {
  if (currentDeck() !== "sonar") return;
  const r = await get("/api/sonar/sleep" + (sleepNode ? "?node=" + encodeURIComponent(sleepNode) : ""));
  if (!r) return;
  if (!sleepNode) sleepNode = r.node || "";
  $("#sleepNode").textContent = (r.node || "—").toUpperCase();
  const pts = r.series || [], c = $("#sleepChart"), w = c.clientWidth || 300;
  c.width = w * 2; c.height = 128;
  const x = c.getContext("2d"); x.clearRect(0, 0, c.width, c.height);
  x.strokeStyle = "rgba(94,246,255,0.15)"; x.lineWidth = 1;
  [10, 15, 20].forEach((v) => { const Y = 100 - (v - 8) / 16 * 92; x.beginPath(); x.moveTo(0, Y); x.lineTo(c.width, Y); x.stroke(); x.fillStyle = "#43727e"; x.font = "18px ui-monospace, monospace"; x.fillText(v, 4, Y - 3); });
  if (pts.length < 2) { $("#sleepMeta").textContent = "collecting — one reading a minute"; return; }
  const t0 = pts[0].t, span = Math.max(1, pts[pts.length - 1].t - t0), X = (p) => 30 + (p.t - t0) / span * (c.width - 34);
  pts.forEach((p) => { if (p.m) { x.fillStyle = "rgba(255,84,73,0.75)"; x.fillRect(X(p) - 1, 128 - Math.min(26, 8 + p.m * 6), 3, 26); } });
  x.strokeStyle = "#ffb84a"; x.lineWidth = 2.5; x.beginPath(); let pen = false;
  pts.forEach((p) => { if (!p.b) { pen = false; return; } const Y = 100 - (Math.max(8, Math.min(24, p.b)) - 8) / 16 * 92; pen ? x.lineTo(X(p), Y) : x.moveTo(X(p), Y); pen = true; });
  x.stroke();
  const br = pts.filter((p) => p.b).map((p) => p.b), restless = pts.filter((p) => p.m).length;
  $("#sleepMeta").textContent = (span / 3600000).toFixed(1) + " h · " + (br.length ? "avg " + (br.reduce((a, v) => a + v, 0) / br.length).toFixed(1) + "/min" : "no breathing found") + " · " + restless + " restless min";
}

// ---- SONAR: where is the movement? a score-weighted blend of the disturbed links ----
let sonarTrail = [], sonarMode = "";
function sonarEstimate() {
  const nodes = sonarState.nodes || [], ap = sonarXY("@ap", 0, 1);
  let wx = 0, wy = 0, w = 0;
  nodes.forEach((n, i) => {
    if (n.state !== "motion") return;
    const q = sonarNodePos(n, i, nodes.length);
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
    const plan = new THREE.Group(); scene.add(plan);
    const hostsG = new THREE.Group(); scene.add(hostsG);
    host.addEventListener("pointerdown", (e) => { s3.drag = { x: e.clientX, y: e.clientY }; host.setPointerCapture(e.pointerId); });
    host.addEventListener("pointermove", (e) => { if (!s3.drag) return; s3.az += (e.clientX - s3.drag.x) * 0.006; s3.el = Math.max(0.12, Math.min(1.45, s3.el + (e.clientY - s3.drag.y) * 0.005)); s3.drag = { x: e.clientX, y: e.clientY }; s3.lastUser = performance.now(); });
    host.addEventListener("pointerup", () => { s3.drag = null; });
    host.addEventListener("wheel", (e) => { e.preventDefault(); s3.dist = Math.max(6, Math.min(26, s3.dist + e.deltaY * 0.01)); s3.lastUser = performance.now(); }, { passive: false });
    s3.r = { renderer, scene, camera, ap, rings, blob, trail, dyn, plan, hostsG, planSig: "", hostSig: "", hostObjs: {}, host, w: 0, h: 0 };
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
    const o = s3.objs[n.node], q = sonarNodePos(n, i, nodes.length);
    const N = new THREE.Vector3(q.x * S3, H, q.y * S3);
    const hot = n.state === "motion", k = Math.min(1, n.score / 100), pulse = hot ? 0.55 + 0.45 * Math.sin(t * 9) : 1;
    const col = n.state === "offline" ? 0x43727e : hot ? 0xff5449 : n.state === "calibrating" ? 0xffb84a : 0x1dffa1;
    o.marker.position.copy(N); o.marker.rotation.y = t * 1.2; o.marker.material.color.setHex(col);
    o.link.geometry.setFromPoints([A, N]); o.link.material.color.setHex(hot ? 0xff5449 : 0x5ef6ff);
    const mid = A.clone().add(N).multiplyScalar(0.5), dx = N.x - A.x, dz = N.z - A.z, len = Math.hypot(dx, dz);
    [o.zone, o.wire].forEach((m) => { m.position.copy(mid); m.rotation.y = -Math.atan2(dz, dx); m.scale.set(len / 2 + 0.3, 0.6 + (hot ? k * 0.4 : 0), 0.55 + len * 0.07 + (hot ? k * 0.45 : 0)); m.material.color.setHex(hot ? 0xff5449 : 0x1dffa1); });
    o.zone.material.opacity = hot ? 0.14 + 0.26 * k * pulse : (n.state === "clear" ? 0.07 : 0.03);
    o.wire.material.opacity = hot ? 0.45 : 0.14;
    const text = n.node.toUpperCase() + " · " + n.state.toUpperCase() + (n.state === "offline" ? "" : " " + Math.round(n.score)) + (n.breath ? " · RESP " + n.breath : "");
    if (text !== o.text) { if (o.label) o.g.remove(o.label); o.text = text; o.label = textSprite(text, "#" + col.toString(16).padStart(6, "0")); o.g.add(o.label); }
    o.label.position.set(N.x + 0.35, N.y + 0.45, N.z);
  });

  const last = sonarTrail[sonarTrail.length - 1];
  R.blob.visible = !!last;
  if (last) { R.blob.position.set(last.x * S3, 0.75, last.y * S3); const sc = 1 + 0.12 * Math.sin(t * 7); R.blob.scale.set(sc, 1, sc); }
  R.trail.geometry.setFromPoints(sonarTrail.map((q) => new THREE.Vector3(q.x * S3, 0.05, q.y * S3)));
  // floor plan walls and tripwire curtains (rebuilt only when they change)
  const planSig = JSON.stringify([room.walls, room.wires]);
  if (planSig !== R.planSig) {
    R.planSig = planSig;
    while (R.plan.children.length) R.plan.remove(R.plan.children[0]);
    const curtain = (w, color, opacity, height) => {
      const ax = w.a.x * S3, az = w.a.y * S3, bx = w.b.x * S3, bz = w.b.y * S3, len = Math.hypot(bx - ax, bz - az);
      const m = new THREE.Mesh(new THREE.PlaneGeometry(len, height), new THREE.MeshBasicMaterial({ color, transparent: true, opacity, side: THREE.DoubleSide, depthWrite: false }));
      m.position.set((ax + bx) / 2, height / 2, (az + bz) / 2); m.rotation.y = -Math.atan2(bz - az, bx - ax);
      const edge = new THREE.LineSegments(new THREE.EdgesGeometry(m.geometry), new THREE.LineBasicMaterial({ color, transparent: true, opacity: Math.min(1, opacity * 4) }));
      edge.position.copy(m.position); edge.rotation.copy(m.rotation);
      R.plan.add(m, edge); return m;
    };
    room.walls.forEach((w) => curtain(w, 0xbdf9ff, 0.1, 2.4));
    R.wireMeshes = room.wires.map((w) => curtain(w, 0xff5449, 0.2, 2.2));
  }
  (R.wireMeshes || []).forEach((m) => { m.material.opacity = 0.14 + 0.1 * Math.sin(t * 5); });
  // located hosts
  const hostSig = sonarHosts.map((q) => q.mac).join("|");
  if (hostSig !== R.hostSig) {
    R.hostSig = hostSig; R.hostObjs = {};
    while (R.hostsG.children.length) R.hostsG.remove(R.hostsG.children[0]);
    sonarHosts.forEach((q) => {
      const g = new THREE.Group(), box = new THREE.Mesh(new THREE.BoxGeometry(0.22, 0.22, 0.22), new THREE.MeshBasicMaterial({ color: 0x5ef6ff, wireframe: q.nodes < 3 }));
      const label = textSprite(q.name.toUpperCase(), "#5ef6ff"); label.position.set(0.25, 0.2, 0); label.scale.set(2.6, 0.5, 1);
      g.add(box, label); R.hostsG.add(g); R.hostObjs[q.mac] = { g, box };
    });
  }
  sonarHosts.forEach((q) => {
    const o = R.hostObjs[q.mac]; if (!o) return;
    const dv = (STATE.devices || []).find((d) => d.ip === q.ip);
    o.box.material.color.setHex(dv && threatOf(dv) >= 50 ? 0xff5449 : 0x5ef6ff);
    o.g.position.set(q.x * S3, 0.5, q.y * S3); o.box.rotation.y = t;
  });
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

// ================= BRAINDANCE: record the presence field, replay it later =================
const mmss = (ms) => { const s = Math.max(0, Math.round(ms / 1000)); return String((s / 60) | 0).padStart(2, "0") + ":" + String(s % 60).padStart(2, "0"); };
async function bdRefresh() {
  const r = await get("/api/braindance"); if (!r) return;
  dance.clips = r.clips || []; dance.recSince = r.recordingSince || 0;
  $("#bdMeta").textContent = "buffer " + mmss(r.bufferSec * 1000) + (r.stored ? " · on disk" : " · memory only");
  const rec = $("#bdRec");
  rec.classList.toggle("on", !!dance.recSince);
  rec.textContent = dance.recSince ? "■ STOP " + mmss(Date.now() - dance.recSince) : "● REC";
  const list = $("#bdList"); list.innerHTML = "";
  if (!dance.clips.length) { list.appendChild(el("div", "sonar-help", "No clips yet. Record one, or save the last minute.")); return; }
  dance.clips.forEach((c) => {
    const row = el("div", "bdclip" + (dance.play && dance.play.clip.id === c.id ? " playing" : ""));
    const info = el("div");
    info.appendChild(el("div", "nm", c.name));
    info.appendChild(el("div", "sub", new Date(c.start).toLocaleString("en-GB") + " · " + mmss(c.end - c.start) + " · peak " + Math.round(c.peak) + (c.auto ? " · auto" : "")));
    const play = el("button", null, "▶"), del = el("button", "del", "✕");
    play.title = "replay this clip"; del.title = "delete this clip";
    play.addEventListener("click", () => bdOpen(c.id));
    del.addEventListener("click", async () => { if (dance.play && dance.play.clip.id === c.id) bdExit(); await post("/api/braindance/delete", { id: c.id }); bdRefresh(); });
    row.appendChild(info); row.appendChild(play); row.appendChild(del);
    list.appendChild(row);
  });
}
$("#bdRec").addEventListener("click", async () => { await post("/api/braindance/rec", { on: !dance.recSince }); bdRefresh(); });
$("#bdSave").addEventListener("click", async () => { const r = await post("/api/braindance/save", { lastSec: 60 }); if (r && r.ok) status("clip saved: " + r.clip.name); bdRefresh(); });

async function bdOpen(id) {
  const clip = await get("/api/braindance/clip?id=" + encodeURIComponent(id));
  if (!clip || !clip.samples || clip.samples.length < 2) { status("that clip could not be loaded"); return false; }
  if (dance.play) bdExit();
  const livePos = sonarPos;
  if (clip.layout && Object.keys(clip.layout).length) sonarPos = Object.assign({}, livePos, clip.layout);
  // where the movement was at each recorded instant
  const frames = clip.samples.map((smp) => ({ t: smp.t, nodes: smp.n.map((q) => ({ node: q.n, state: q.s, score: q.v, breath: q.b || 0, distM: q.d || 0, trace: [] })) }));
  const path = frames.map((f) => { sonarState = { nodes: f.nodes }; return sonarEstimate(); });
  dance.play = { clip, frames, path, livePos, pos: 0, dur: clip.end - clip.start, playing: true, speed: 1, last: performance.now(), idx: -1 };
  document.querySelector(".sonar-map").classList.add("replaying");
  $("#bdPlayer").hidden = false; $("#bdTag").hidden = false;
  $("#bdTag").textContent = "BRAINDANCE · " + clip.name;
  $("#bdPlay").textContent = "❚❚";
  selectDeck("sonar"); bdRefresh();
  return true;
}
function bdExit() {
  if (!dance.play) return;
  sonarPos = dance.play.livePos; dance.play = null;
  sonarState = liveSonar; sonarTrail = [];
  document.querySelector(".sonar-map").classList.remove("replaying");
  $("#bdPlayer").hidden = true; $("#bdTag").hidden = true; $("#bdThen").textContent = "";
  sonarRenderList(); bdRefresh();
}
// Advance the replay; called every animation frame while a clip is open.
function bdTick() {
  const pl = dance.play; if (!pl) return;
  const now = performance.now();
  if (pl.playing) { pl.pos += (now - pl.last) * pl.speed; if (pl.pos >= pl.dur) { pl.pos = pl.dur; pl.playing = false; $("#bdPlay").textContent = "▶"; } }
  pl.last = now;
  const tAbs = pl.clip.start + pl.pos;
  let idx = 0; while (idx < pl.frames.length - 1 && pl.frames[idx + 1].t <= tAbs) idx++;
  $("#bdSeek").value = Math.round(pl.pos / pl.dur * 1000);
  $("#bdTime").textContent = new Date(tAbs).toLocaleTimeString("en-GB") + " · " + mmss(pl.pos) + " / " + mmss(pl.dur);
  if (idx === pl.idx) return;
  pl.idx = idx;
  sonarState = { nodes: pl.frames[idx].nodes, armed: liveSonar.armed, demo: liveSonar.demo, listening: liveSonar.listening };
  sonarTrail = pl.path.slice(Math.max(0, idx - 60), idx + 1).filter(Boolean);
  if (!pl.path[idx]) sonarTrail = []; // nothing was moving at this instant
  sonarRenderList();
  // what the network looked like at that moment, if the console still remembers it
  let then = null; hist.forEach((hh) => { if (hh.ts <= tAbs) then = hh; });
  $("#bdThen").textContent = then && hist.length && tAbs <= hist[hist.length - 1].ts + 60000 ? "NETWORK THEN · " + then.hosts + " hosts · " + then.alerts + " alerts" + (then.crit ? " · " + then.crit + " critical" : "") : "";
}
$("#bdPlay").addEventListener("click", () => { const pl = dance.play; if (!pl) return; if (pl.pos >= pl.dur) pl.pos = 0; pl.playing = !pl.playing; pl.last = performance.now(); $("#bdPlay").textContent = pl.playing ? "❚❚" : "▶"; });
$("#bdSeek").addEventListener("input", (e) => { const pl = dance.play; if (!pl) return; pl.pos = (+e.target.value) / 1000 * pl.dur; pl.idx = -1; });
$("#bdSpeed").addEventListener("click", () => { const pl = dance.play; if (!pl) return; pl.speed = pl.speed === 1 ? 2 : pl.speed === 2 ? 4 : pl.speed === 4 ? 0.5 : 1; $("#bdSpeed").textContent = pl.speed + "×"; });
$("#bdExit").addEventListener("click", bdExit);

// ================= PRESENCE GATE =================
function renderGate(g) {
  g = g || { enabled: false, open: true };
  const st = $("#gateState");
  st.className = "gate-state" + (g.enabled ? (g.open ? " open" : " shut") : "");
  st.textContent = !g.enabled ? "no key set — controls are always available" : g.open ? "key " + g.key + " is on the network — controls unlocked" : "key " + g.key + " is away — trust and BLACKWALL are locked";
  $("#gateClear").hidden = !g.enabled;
  document.body.classList.toggle("gated", g.enabled && !g.open);
}
$("#gateSet").addEventListener("click", async () => { if (selectedIP) await post("/api/gate", { ip: selectedIP }); });
$("#gateClear").addEventListener("click", async () => { await post("/api/gate", { ip: "" }); });

// ================= ULTRASONIC PROXIMITY LOCK (experimental) =================
// The speaker plays a quiet 19 kHz tone and the microphone listens to it. A body
// moving nearby smears energy into the bands beside the tone (Doppler). When
// that stops for long enough, nobody is at the laptop and the screen is locked.
const prox = { on: false, ctx: null, stream: null, timer: null, floor: null, lastMove: 0, calib: [], awaySec: 60, graceSec: 10 };
const PROX_HZ = 19000;
// Ratio of sideband power to carrier power in a power spectrum (linear units).
function proxIndex(power, carrierBin) {
  let carrier = 0, side = 0;
  for (let k = -2; k <= 2; k++) carrier += power[carrierBin + k] || 0;
  for (let k = 4; k <= 34; k++) side += (power[carrierBin + k] || 0) + (power[carrierBin - k] || 0);
  return carrier > 0 ? side / carrier : 0;
}
// One step of the presence logic. Returns "calibrating" | "present" | "away" | "warn" | "lock".
function proxStep(st, index, now) {
  if (st.floor == null) {
    st.calib.push(index);
    if (st.calib.length < 12) return "calibrating";
    st.floor = st.calib.slice().sort((a, b) => a - b)[Math.floor(st.calib.length / 2)] || 1e-9;
    st.lastMove = now;
  }
  if (index > st.floor * 3) st.lastMove = now;
  const idle = (now - st.lastMove) / 1000;
  if (idle >= st.awaySec + st.graceSec) return "lock";
  if (idle >= st.awaySec) return "warn";
  return idle > 5 ? "away" : "present";
}
async function setProx(on) {
  const btn = $("#proxBtn"), banner = $("#proxBanner");
  if (!on) {
    prox.on = false; clearInterval(prox.timer); banner.hidden = true;
    if (prox.stream) prox.stream.getTracks().forEach((t) => t.stop());
    if (prox.ctx) prox.ctx.close();
    prox.ctx = prox.stream = null; btn.className = "hs-btn"; btn.textContent = "PROX LOCK OFF"; return "Proximity lock off.";
  }
  try {
    prox.stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false } });
  } catch (e) { status("proximity lock needs the microphone: " + e.name); return "I need microphone access for that."; }
  const AC = window.AudioContext || window.webkitAudioContext; prox.ctx = new AC();
  if (prox.ctx.sampleRate < 40000) { setProx(false); return "This audio device cannot carry a 19 kHz tone."; }
  const osc = prox.ctx.createOscillator(), gain = prox.ctx.createGain(); osc.frequency.value = PROX_HZ; gain.gain.value = 0.08; osc.connect(gain).connect(prox.ctx.destination); osc.start();
  const an = prox.ctx.createAnalyser(); an.fftSize = 8192; an.smoothingTimeConstant = 0.2; prox.ctx.createMediaStreamSource(prox.stream).connect(an);
  const db = new Float32Array(an.frequencyBinCount), power = new Float32Array(an.frequencyBinCount), bin = Math.round(PROX_HZ / (prox.ctx.sampleRate / an.fftSize));
  Object.assign(prox, { on: true, floor: null, calib: [], lastMove: Date.now() });
  btn.classList.add("on");
  prox.timer = setInterval(async () => {
    an.getFloatFrequencyData(db);
    for (let i = 0; i < db.length; i++) power[i] = Math.pow(10, db[i] / 10);
    const st = proxStep(prox, proxIndex(power, bin), Date.now()), idle = Math.round((Date.now() - prox.lastMove) / 1000);
    btn.classList.toggle("away", st === "away" || st === "warn");
    btn.textContent = st === "calibrating" ? "PROX · CALIBRATING" : st === "present" ? "PROX · PRESENT" : "PROX · AWAY " + idle + "s";
    banner.hidden = st !== "warn";
    if (st === "warn") banner.textContent = "No movement near the laptop — locking in " + Math.max(0, prox.awaySec + prox.graceSec - idle) + " s · move to cancel";
    if (st === "lock") { await post("/api/lock"); setProx(false); status("screen locked by proximity lock"); }
  }, 500);
  return "Proximity lock armed. I will lock the screen after " + prox.awaySec + " seconds with nobody moving nearby.";
}
$("#proxBtn").addEventListener("click", () => setProx(!prox.on));

// ================= COMMANDS: typed or spoken =================
const DECK_WORDS = { watchtower: "netwatch", watch: "netwatch", breach: "breach", "kill chain": "killchain", killchain: "killchain", "attack": "attack", "att&ck": "attack", mitre: "attack", grid: "grid", globe: "grid", ar: "ar", optic: "ar", sonar: "sonar", decoy: "decoy", decoys: "decoy", cortex: "cortex", cases: "cortex", spectrum: "spectrum", radios: "spectrum", oracle: "oracle", pulse: "pulse", ward: "ward", quantum: "quantum", ledger: "ledger" };
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
  for (const f of EXTRA_COMMANDS) { const r = await f(t, text); if (r != null) return r; }
  if (/^(sweep|scan)( the)?( network| segment)?$/.test(t)) { if (replay) return "Go live first."; await post("/api/sweep"); return "Sweeping the segment."; }
  if ((m = t.match(/^(?:switch to|open|show|go to)\s+(?:the\s+)?(.+?)(?:\s+deck)?$/)) && DECK_WORDS[m[1]]) { selectDeck(DECK_WORDS[m[1]]); return "Switching to " + m[1].toUpperCase() + "."; }
  if ((m = t.match(/^(?:select|target|lock on(?: to)?)\s+(.+)$/))) { const d = findDevice(m[1]); if (!d) return "No host matches " + m[1] + "."; selectDeck("netwatch"); selectDevice(d.ip); return "Locked on " + (d.hostname || d.ip) + ". Threat " + threatOf(d) + " of 100."; }
  if ((m = t.match(/^(untrust|trust)(?:\s+(.+))?$/))) { const d = m[2] ? findDevice(m[2]) : STATE.devices.find((x) => x.ip === selectedIP); if (!d) return "Select a host first."; await post("/api/trust", { ip: d.ip, trusted: m[1] === "trust" }); return (d.hostname || d.ip) + (m[1] === "trust" ? " marked trusted." : " marked unverified."); }
  if ((m = t.match(/^(?:quarantine|isolate|blackwall)(?:\s+(.+))?$/))) { const d = m[1] ? findDevice(m[1]) : STATE.devices.find((x) => x.ip === selectedIP); if (!d) return "Select a host first."; if (d.self || d.gateway) return "Refusing: that is " + (d.self ? "this machine" : "the gateway") + "."; pendingConfirm = { ip: d.ip, until: Date.now() + 15000 }; return "Quarantine " + (d.hostname || d.ip) + " at the firewall? Say or type confirm."; }
  if ((m = t.match(/^release(?:\s+(.+))?$/))) { const d = m[1] ? findDevice(m[1]) : STATE.devices.find((x) => x.ip === selectedIP); if (!d) return "Select a host first."; await post("/api/blackwall", { ip: d.ip, action: "release" }); return (d.hostname || d.ip) + " released."; }
  if (/^(go )?live$/.test(t)) { goLive(); return "Back to live."; }
  if ((m = t.match(/^(?:rewind|go back)\s+(\d+)\s+min/))) { const target = Date.now() - (+m[1]) * 60000; let i = 0; hist.forEach((h, k) => { if (h.ts <= target) i = k; }); await scrubTo(i); return "Replaying " + m[1] + " minutes ago."; }
  if ((m = t.match(/^(disarm|arm)(?: the)? sonar$/))) { await post("/api/sonar/arm", { armed: m[1] === "arm" }); sonarPoll(); return "SONAR " + (m[1] === "arm" ? "armed." : "disarmed."); }
  if (/^(start recording|record)$/.test(t)) { await post("/api/braindance/rec", { on: true }); bdRefresh(); return "Recording the presence field."; }
  if (/^(stop recording|stop)$/.test(t)) { const r = await post("/api/braindance/rec", { on: false }); bdRefresh(); return r && r.ok ? "Saved clip " + r.clip.name + "." : "I was not recording."; }
  if (/^save (the )?(last minute|clip)$/.test(t)) { const r = await post("/api/braindance/save", { lastSec: 60 }); bdRefresh(); return r && r.ok ? "Saved the last minute as " + r.clip.name + "." : "Nothing to save yet."; }
  if (/^(play|replay)( the)? (last|latest) clip$/.test(t)) { await bdRefresh(); if (!dance.clips.length) return "There are no clips."; return (await bdOpen(dance.clips[0].id)) ? "Replaying " + dance.clips[0].name + "." : "That clip could not be loaded."; }
  if (/^(exit|stop) (replay|braindance)$/.test(t)) { bdExit(); return "Back to the live field."; }
  if ((m = t.match(/^set key(?: to)?\s+(.+)$/))) { const d = findDevice(m[1]); if (!d) return "No host matches " + m[1] + "."; const r = await post("/api/gate", { ip: d.ip }); return r && r.ok ? (d.hostname || d.ip) + " is now the presence key." : "Could not set the key."; }
  if (/^clear key$/.test(t)) { const r = await post("/api/gate", { ip: "" }); return r && r.ok ? "Presence key removed." : "The key device has to be present to remove it."; }
  if (/^(surface diff|what changed|what's changed)$/.test(t)) { const r = await get("/api/surface"); return r ? r.lines.join("\n") : "I could not read the surface."; }
  if (/^(fire drill|run (a |the )?(fire )?drill|test (the )?detectors)$/.test(t)) { const r = await post("/api/drill"); return r ? r.lines.join("\n") : "The drill did not run."; }
  if (/^(usb|list usb|usb devices)$/.test(t)) { const r = await get("/api/usb"); return r ? (r.devices.length ? r.devices.map((d) => (d.vendor + " " + d.product).trim() + " [" + d.vid + ":" + d.pid + "]" + (d.keyboard ? " — keyboard" : "")).join("\n") : "No USB devices.") : "I could not list USB devices."; }
  if (/^lock( the)?( screen)?$/.test(t)) { const r = await post("/api/lock"); return r && r.ok ? (r.note || "Screen locked.") : "I could not lock the screen."; }
  if (/^(proximity|prox) lock (on|off)$/.test(t)) return await setProx(/on$/.test(t));
  if (/^(how many people|who is (home|here)|anyone (home|here))/.test(t)) { await sonarPoll(); const r = await get("/api/sonar"); if (!r || !r.nodes.length) return "SONAR has no nodes reporting."; const still = r.nodes.filter((n) => n.breath).length; return "About " + r.people + " present" + (still ? ", " + still + " of them still and breathing" : "") + ". " + r.nodes.filter((n) => n.state === "motion").length + " links disturbed."; }
  if (/^(sleep|respiration) (report|log)$/.test(t)) { const r = await get("/api/sonar/sleep"); if (!r || r.series.length < 2) return "There is no respiration data yet."; const b = r.series.filter((p) => p.b).map((p) => p.b); return "On " + r.node + ": " + ((r.series[r.series.length - 1].t - r.series[0].t) / 3600000).toFixed(1) + " hours logged, average " + (b.reduce((a, v) => a + v, 0) / Math.max(1, b.length)).toFixed(1) + " breaths a minute, " + r.series.filter((p) => p.m).length + " restless minutes."; }
  if ((m = t.match(/^(?:investigate|why)(?:\s+(.+))?$/))) { await cortexLoad(); const d = m[1] ? findDevice(m[1]) : null; const a = cortex.queue.find((x) => (d ? x.source === d.ip : true)); if (!a) return "There is no alert to investigate" + (d ? " for " + (d.hostname || d.ip) : "") + "."; selectDeck("cortex"); const r = await cortexOpen(a); return r ? "Verdict: " + r.case.verdict + ", " + r.case.confidence + " percent sure. Proposed: " + r.case.proposal.action + " " + r.case.proposal.target + ". Say approve or dismiss." : "I could not open that case."; }
  if (/^(approve|dismiss)$/.test(t) && currentDeck() === "cortex") return await cortexDecide(t === "approve");
  if ((m = t.match(/^(?:open|plant) (?:a )?(?:honeyport|decoy port)(?: on)?(?: port)? (\d+)$/))) { const r = await decoyAct({ action: "port-open", port: +m[1] }); return r && r.ok ? "Decoy port " + m[1] + " is listening. " + (r.note || "") : "Could not open it: " + ((r && r.error) || "backend unreachable"); }
  if (/^(decoy drill|test (the )?decoys)$/.test(t)) { const r = await decoyAct({ action: "test" }); return r && r.ok ? (r.note || "Drill sent.") : "The drill did not run."; }
  if (/^(quantum scan|inventory (the )?(crypto|cryptography|encryption))$/.test(t)) { selectDeck("quantum"); const r = await quantumScan(); return r && r.ok ? "Reading the TLS and SSH handshakes on your network." : "An inventory is already running."; }
  if (/^verify( the)? (ledger|chain|log)$/.test(t)) { const r = await ledgerLoad(true); return r ? (r.intact ? "The chain is intact: " + r.len + " entries." : "The chain is broken at entry " + r.bad + ".") + (r.disk ? " " + r.disk + "." : "") : "I could not read the ledger."; }
  if ((m = t.match(/^(?:note|log)[: ]\s*(.+)$/))) { const r = await post("/api/ledger/note", { text: text.replace(/^\s*\S+[: ]\s*/, "") }); return r && r.ok ? "Recorded as entry " + r.seq + "." : "I could not record that."; }
  if ((m = t.match(/^(?:incident )?report(?: (?:for|of))?(?: the)?(?: last)?(?: (hour|day|24 hours))?$/))) { const r = await ledgerReport(m[1] === "hour" ? 60 : m[1] ? 1440 : 0); return r ? r.report : "I could not write the report."; }
  if ((m = t.match(/^(?:reach|who does|what does)\s+(.+?)(?:\s+talk to)?$/))) { const d = findDevice(m[1]); if (!d) return "No host matches " + m[1] + "."; oracleSt.host = d.ip; selectDeck("oracle"); const r = await get("/api/oracle?host=" + encodeURIComponent(d.ip)); return r && r.reach ? (d.hostname || d.ip) + " talks to " + r.reach.internal.length + " hosts inside and " + r.reach.external.length + " outside. " + r.reach.isolating : "I have not seen it talk yet."; }
  if ((m = t.match(/^what if\s+(.+)$/))) { selectDeck("oracle"); const r = await oracleWhatIf(m[1]); return r && r.ok ? "That rule would have matched " + r.result.matches.length + " of " + r.result.of + " conversations from " + r.result.hosts.length + " hosts." : "I do not understand that rule. Try: port 23, unnamed, fanout > 3, bytes > 1000000."; }
  if ((m = t.match(/^pulse (on|off)$/))) return setPulse(m[1] === "on");
  if (/^(radios|what radios|who is in range|spectrum)$/.test(t)) { const r = await get("/api/spectrum"); if (!r) return "I could not read the spectrum."; const fl = r.radios.filter((x) => x.tag === "lingering" || x.tag === "tracker?"); return r.radios.length + " radios in range, " + fl.length + " flagged" + (fl.length ? ": " + fl.map((x) => (x.name || x.id) + " (" + x.tag + ")").join(", ") : "") + "."; }
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
const seenAlerts = new Set();
function onNewAlerts(snap) {
  // Alerts are told apart by time, source and kind: two raised in the same millisecond are both new.
  const alerts = snap.alerts || [], key = (a) => a.ts + "|" + a.source + "|" + a.kind + "|" + a.message, first = lastAlertTs === null;
  const fresh = alerts.filter((a) => !seenAlerts.has(key(a)));
  fresh.forEach((a) => seenAlerts.add(key(a)));
  lastAlertTs = alerts.reduce((mx, a) => Math.max(mx, a.ts || 0), lastAlertTs || 0);
  if (first) return;
  fresh.slice(0, 3).forEach(playAlert);
  fresh.slice(0, 4).reverse().forEach((a) => ALERT_HOOKS.forEach((f) => f(a)));
}

// ---------- module decks: DECOY · CORTEX · SPECTRUM · ORACLE · PULSE · WARD · QUANTUM · LEDGER ----------
// Everything that came off the network goes through esc() before it becomes HTML.
const ago = (ts) => { const s = Math.max(0, (Date.now() - ts) / 1000); return s < 90 ? Math.round(s) + "s ago" : s < 5400 ? Math.round(s / 60) + "m ago" : s < 129600 ? Math.round(s / 3600) + "h ago" : Math.round(s / 86400) + "d ago"; };
const named = (ip, names) => (names && names[ip] ? names[ip] : ip);
const sevClass = (s) => (s === "CRITICAL" ? "crit" : s === "WARN" ? "warn" : "info");
function hashAngle(id) { let h = 2166136261; for (let i = 0; i < id.length; i++) { h ^= id.charCodeAt(i); h = Math.imul(h, 16777619); } h ^= h >>> 13; h = Math.imul(h, 0x5bd1e995); h ^= h >>> 15; return ((h >>> 0) % 3600) / 10; }

// ---- DECOY ----
async function decoyAct(body) { const r = await post("/api/decoy", body); if (r && r.ok && r.note) status(r.note); await decoyLoad(); return r; }
async function decoyLoad() {
  const r = await get("/api/decoy"); if (!r) return;
  const st = r.state, total = st.ports.length + st.files.length + st.names.length;
  $("#dcMeta").textContent = total + " planted" + (r.demo ? " · sample · nothing real is opened" : "");
  const chips = (sel, items, label, onRemove) => {
    const box = $(sel);
    box.innerHTML = items.length ? items.map((x, i) => '<span class="m-chip">' + esc(label(x)) + '<b data-i="' + i + '" title="remove">✕</b></span>').join("") : '<span class="m-dim">none planted</span>';
    box.querySelectorAll("b").forEach((b) => (b.onclick = () => onRemove(items[+b.dataset.i])));
  };
  chips("#dcPorts", st.ports, (p) => "tcp/" + p, (p) => decoyAct({ action: "port-close", port: p }));
  chips("#dcFiles", st.files, (f) => f, (f) => decoyAct({ action: "file-remove", path: f }));
  chips("#dcNames", st.names, (n) => n, (n) => decoyAct({ action: "name-remove", name: n }));
  const kinds = { honeyport: "HONEYPORT", "canary-file": "CANARY FILE", "canary-name": "CANARY NAME" };
  $("#dcTrips").innerHTML = r.trips.length ? r.trips.map((t) =>
    '<div class="m-row ' + (/decoy drill/i.test(t.detail) ? "info" : "crit") + '"><div class="m-row-top"><span class="badge' + (/decoy drill/i.test(t.detail) ? '">DRILL · ' : ' bad">') + esc(kinds[t.kind] || t.kind) + '</span><b>' + esc(t.source) + '</b><span class="m-dim">→ ' + esc(t.decoy) + '</span><time>' + esc(ago(new Date(t.time).getTime())) + '</time></div><div class="m-mono">' + esc(t.detail) + '</div></div>').join("")
    : '<div class="empty">no decoy has been touched</div>';
  if (typeof decoyExtra === "function") decoyExtra(r);
}
function decoyInit() {
  const add = (btn, input, build) => {
    const go = async () => { const v = $(input).value.trim(); const body = build(v); if (!body) return; const r = await decoyAct(body); if (r && r.ok) $(input).value = ""; };
    $(btn).onclick = go; $(input).addEventListener("keydown", (e) => { if (e.key === "Enter") go(); });
  };
  add("#dcPortAdd", "#dcPort", (v) => { const p = parseInt(v, 10); if (!(p >= 1 && p <= 65535)) { status("a port is a number from 1 to 65535"); return null; } return { action: "port-open", port: p }; });
  add("#dcFileAdd", "#dcFile", (v) => ({ action: "file-plant", name: v }));
  add("#dcNameAdd", "#dcName", (v) => (v ? { action: "name-add", name: v } : null));
  $("#dcDrill").onclick = () => decoyAct({ action: "test" });
}

// ---- CORTEX ----
const cortex = { sel: null, queue: [], shown: null };
const cxKey = (a) => a.ts + "|" + a.source + "|" + a.kind;
const verdictClass = (v) => (v === "likely threat" ? "bad" : v === "needs a look" ? "warn" : "ok");
async function cortexLoad() {
  const r = await get("/api/cortex"); if (!r) return;
  cortex.queue = r.queue; cortex.backend = r.backend;
  $("#cxMeta").textContent = r.queue.length + " alerts triaged";
  $("#cxBackend").textContent = "reasoning: built-in rules · summary: " + (/^local model/.test(r.backend) ? r.backend : "none (no local model)");
  const box = $("#cxQueue");
  box.innerHTML = r.queue.length ? r.queue.map((a, i) =>
    '<div class="m-row cx-item ' + sevClass(a.severity) + (cortex.sel === cxKey(a) ? " sel" : "") + '" data-i="' + i + '"><div class="m-row-top"><span class="m-verdict ' + verdictClass(a.verdict) + '">' + esc(a.verdict.toUpperCase()) + '</span><span class="m-dim">' + a.confidence + '% sure</span><time>' + esc(fmtTime(a.ts)) + '</time></div><div class="m-mono">' + esc(a.message) + '</div></div>').join("")
    : '<div class="empty">no alerts to investigate</div>';
  box.querySelectorAll(".cx-item").forEach((n) => (n.onclick = () => cortexOpen(cortex.queue[+n.dataset.i])));
  if (!cortex.sel && r.queue.length) cortexOpen(r.queue.find((a) => a.verdict === "likely threat") || r.queue[0]);
}
async function cortexOpen(a, narrate) {
  cortex.sel = cxKey(a);
  document.querySelectorAll(".cx-item").forEach((n) => n.classList.toggle("sel", cxKey(cortex.queue[+n.dataset.i]) === cortex.sel));
  if (narrate) $("#cxNarr").textContent = "asking the local model…";
  const r = await post("/api/cortex", { ts: a.ts, source: a.source, kind: a.kind, narrate: !!narrate });
  if (!r || !r.ok) return null;
  cortex.shown = r;
  const c = r.case, p = c.proposal, vc = verdictClass(c.verdict);
  $("#cxTitle").textContent = "CASE // " + a.kind.toUpperCase() + " · " + a.source;
  const w = (n) => '<span class="cx-w ' + (n > 0 ? "up" : n < 0 ? "down" : "") + '">' + (n > 0 ? "+" : "") + n + '</span>';
  const canAct = p.action === "quarantine" || p.action === "trust";
  $("#cxCase").innerHTML =
    '<div class="cx-verdict ' + vc + '"><div><span class="micro">VERDICT</span><b>' + esc(c.verdict.toUpperCase()) + '</b></div><div class="cx-conf"><span class="micro cy">CONFIDENCE ' + c.confidence + '%</span><div class="m-bar"><i style="width:' + c.confidence + '%"></i></div></div><div><span class="micro cy">SCORE</span><b class="cx-score">' + c.score + '</b></div></div>' +
    '<div class="m-mono cx-alert">' + esc(r.alert.message) + '</div>' +
    '<div class="micro cy cx-h">▚ CHAIN OF REASONING</div>' +
    c.steps.map((s, i) => '<div class="cx-step"><span class="cx-n">' + (i + 1) + '</span>' + w(s.weight) + '<div><b>' + esc(s.title) + '</b><span>' + esc(s.finding) + '</span></div></div>').join("") +
    '<div class="cx-sides"><div class="cx-side for"><div class="micro">▚ THE CASE FOR A THREAT</div>' + c.for.map((x) => '<p>' + esc(x) + '</p>').join("") + '</div><div class="cx-side against"><div class="micro grn">▚ THE SCEPTIC</div>' + (c.against || []).map((x) => '<p>' + esc(x) + '</p>').join("") + '</div></div>' +
    '<div class="cx-prop ' + vc + '"><div><span class="micro">PROPOSED ACTION</span><b>' + esc(p.action.toUpperCase()) + ' · ' + esc(p.target) + '</b><p>' + esc(p.why) + '</p></div><div class="cx-btns"><button class="btn' + (p.action === "quarantine" ? " danger" : "") + '" id="cxApprove">' + (canAct ? "APPROVE" : "ACKNOWLEDGE") + '</button><button class="btn" id="cxDismiss">DISMISS</button></div></div>' +
    '<div class="m-note">CORTEX never acts on its own: nothing changes until you approve, and both choices are written to the LEDGER.' + (/^local model/.test(r.backend) ? ' <button class="hs-btn" id="cxAsk">SUMMARISE WITH THE LOCAL MODEL</button>' : "") + '</div>' +
    '<div class="m-mono cx-narr" id="cxNarr">' + esc(r.narrative || "") + '</div>';
  $("#cxApprove").onclick = () => cortexDecide(true);
  $("#cxDismiss").onclick = () => cortexDecide(false);
  if ($("#cxAsk")) $("#cxAsk").onclick = () => cortexOpen(a, true);
  return r;
}
async function cortexDecide(approve) {
  const r = cortex.shown; if (!r) return "No case is open.";
  const p = r.case.proposal;
  if (replay) { status("go live first"); return "Go live first."; }
  let msg;
  if (approve && p.action === "quarantine" && operatorLevel === "spent" && cortex.sure !== cortex.sel) { cortex.sure = cortex.sel; status("operator watch says you are spent — press APPROVE again if this cannot wait"); return "You are spent. Say approve again if this cannot wait."; }
  if (!approve) { await post("/api/ledger/note", { text: "CORTEX proposal dismissed: " + p.action + " " + p.target }); msg = "Dismissed. Nothing was changed."; }
  else if (p.action === "quarantine") { const q = await post("/api/blackwall", { ip: p.target, action: "quarantine" }); msg = q && q.ok ? "BLACKWALL engaged. " + p.target + " is quarantined." : "Quarantine failed: " + ((q && q.error) || "backend unreachable"); }
  else if (p.action === "trust") { const q = await post("/api/trust", { ip: p.target, trusted: true }); msg = q && q.ok ? p.target + " marked trusted." : "Could not mark it trusted: " + ((q && q.error) || "backend unreachable"); }
  else { await post("/api/ledger/note", { text: "CORTEX: acknowledged — " + p.action + " " + p.target }); msg = "Acknowledged and noted in the LEDGER."; }
  status(msg);
  const a = cortex.queue.find((x) => cxKey(x) === cortex.sel);
  await cortexLoad(); if (a) await cortexOpen(a);
  return msg;
}

// ---- SPECTRUM ----
const TAG_CLASS = { "network host": "ok", mine: "ok", "unknown radio": "", bluetooth: "", lingering: "warn", "tracker?": "bad" };
function spark(hist) {
  if (!hist || hist.length < 2) return "";
  const pts = hist.map((v, i) => ((i / (hist.length - 1)) * 80).toFixed(1) + "," + (18 - ((Math.max(-100, Math.min(-30, v)) + 100) / 70) * 16).toFixed(1)).join(" ");
  return '<svg class="m-spark" viewBox="0 0 80 20"><polyline points="' + pts + '"/></svg>';
}
async function spectrumLoad() {
  const r = await get("/api/spectrum"); if (!r) return;
  const flagged = r.radios.filter((x) => x.tag === "lingering" || x.tag === "tracker?").length;
  $("#spMeta").textContent = r.demo ? "simulated air" : (r.radios.length ? "live from the sensor nodes" : "waiting for sensor nodes");
  $("#spCount").textContent = r.radios.length + " radios · " + flagged + " flagged";
  // scope: radius is signal strength
  const R = (rssi) => 18 + ((Math.max(-95, Math.min(-35, rssi)) + 35) / -60) * 160;
  let svg = '<g class="sp-rings">' + [-50, -65, -80, -95].map((d) => '<circle cx="200" cy="200" r="' + R(d).toFixed(0) + '"/><text x="203" y="' + (200 - R(d) + 11).toFixed(0) + '">' + d + ' dBm</text>').join("") + '<line x1="200" y1="14" x2="200" y2="386"/><line x1="14" y1="200" x2="386" y2="200"/></g><g class="sp-sweep"><path d="M200 200 L200 22 A178 178 0 0 1 290 46 Z"/></g><circle class="sp-me" cx="200" cy="200" r="4"/>';
  r.radios.forEach((x) => {
    const a = (hashAngle(x.id) * Math.PI) / 180, rr = R(x.rssi), cx = 200 + rr * Math.cos(a), cy = 200 + rr * Math.sin(a), cls = TAG_CLASS[x.tag] || "";
    svg += x.kind === "ble" ? '<rect class="sp-dot ' + cls + '" x="' + (cx - 4).toFixed(1) + '" y="' + (cy - 4).toFixed(1) + '" width="8" height="8" transform="rotate(45 ' + cx.toFixed(1) + " " + cy.toFixed(1) + ')"/>' : '<circle class="sp-dot ' + cls + '" cx="' + cx.toFixed(1) + '" cy="' + cy.toFixed(1) + '" r="5"/>';
    if (x.name || cls === "warn" || cls === "bad") svg += '<text class="sp-lbl ' + cls + '" x="' + (cx + 9).toFixed(1) + '" y="' + (cy + 4).toFixed(1) + '">' + esc(x.name || x.tag) + '</text>';
  });
  $("#spScope").innerHTML = svg;
  $("#spRadios").innerHTML = r.radios.length ? r.radios.map((x, i) =>
    '<div class="m-row ' + (TAG_CLASS[x.tag] === "bad" ? "crit" : TAG_CLASS[x.tag] === "warn" ? "warn" : "") + '"><div class="m-row-top"><span class="badge' + (x.kind === "ble" ? " act" : "") + '">' + (x.kind === "ble" ? "BLE" : "WI-FI") + '</span><b>' + esc(x.name || x.id) + '</b><span class="m-verdict ' + (TAG_CLASS[x.tag] || "dim") + '">' + esc(x.tag.toUpperCase()) + '</span><time>' + x.rssi + ' dBm</time></div><div class="m-row-top"><span class="m-dim m-mono">' + esc(x.name ? x.id : x.kind === "ble" ? "no name announced" : "not on your network") + '</span>' + spark(x.history) + '<time>in range ' + (x.minutes < 1 ? Math.round(x.minutes * 60) + " s" : x.minutes.toFixed(0) + " min") + '</time>' + (x.tag === "network host" ? "" : '<button class="hs-btn" data-i="' + i + '" title="' + (x.tag === "mine" ? "stop treating this radio as yours" : "this radio is yours — never flag it") + '">' + (x.tag === "mine" ? "NOT MINE" : "MINE") + '</button>') + '<button class="hs-btn" data-h="' + i + '" title="carry a sensor node around and home in on this radio">HUNT</button></div></div>').join("")
    : '<div class="empty">no radios heard yet — SPECTRUM listens through the SONAR sensor nodes</div>';
  $("#spRadios").querySelectorAll("button").forEach((b) => (b.onclick = async () => { if (b.dataset.h) return huntStart(r.radios[+b.dataset.h]); const x = r.radios[+b.dataset.i]; await post("/api/spectrum", { radio: x.id, mine: x.tag !== "mine" }); spectrumLoad(); }));
  const approved = new Set(r.ssids);
  $("#spApMeta").textContent = approved.size ? "watching " + r.ssids.join(", ") : "no network approved yet";
  const names = [...new Set(r.aps.map((a) => a.ssid).filter((s) => s && !approved.has(s)))];
  $("#spAps").innerHTML = (r.aps.length ? r.aps.map((a) => {
    const twin = approved.has(a.ssid) && !a.trusted;
    return '<div class="m-row ' + (twin ? "crit" : "") + '"><div class="m-row-top"><b>' + esc(a.ssid || "(hidden)") + '</b><span class="m-verdict ' + (twin ? "bad" : a.trusted ? "ok" : "dim") + '">' + (twin ? "LOOK-ALIKE" : a.trusted ? "APPROVED" : "NEIGHBOUR") + '</span><time>' + a.rssi + ' dBm</time></div><div class="m-dim m-mono">' + esc(a.bssid) + '</div></div>';
  }).join("") : '<div class="m-dim">no access points heard yet</div>') +
    '<div class="m-chips sp-approve">' + names.slice(0, 6).map((s, i) => '<button class="hs-btn" data-i="' + i + '">THIS IS MINE: ' + esc(s) + '</button>').join("") + (approved.size ? '<button class="hs-btn" data-clear="1">FORGET APPROVALS</button>' : "") + '</div>';
  $("#spAps").querySelectorAll("button").forEach((b) => (b.onclick = async () => { await post("/api/spectrum", b.dataset.clear ? { clear: true } : { ssids: [names[+b.dataset.i]] }); spectrumLoad(); }));
  $("#spDeauth").textContent = r.deauth;
  $("#spDeauthBar").style.width = Math.min(100, r.deauth * 5) + "%";
  $("#spDeauthBar").parentElement.classList.toggle("hot", r.deauth >= 20);
  $("#spDeauthNote").textContent = r.deauth >= 20 ? "flood — devices are being forced off the network" : r.deauth > 0 ? "a few — normal when devices roam" : "quiet";
}

// ---- ORACLE ----
const oracleSt = { host: null };
async function oracleLoad() {
  const devs = STATE.devices || [];
  if (!oracleSt.host && devs.length) oracleSt.host = (devs.slice().sort((a, b) => threatOf(b) - threatOf(a))[0] || devs[0]).ip;
  const box = $("#orHosts");
  box.innerHTML = devs.map((d, i) => '<button class="hs-btn' + (d.ip === oracleSt.host ? " on" : "") + '" data-i="' + i + '">' + esc(d.hostname || d.ip) + '</button>').join("");
  box.querySelectorAll("button").forEach((b) => (b.onclick = () => { oracleSt.host = devs[+b.dataset.i].ip; oracleLoad(); }));
  if (!oracleSt.host) return;
  const r = await get("/api/oracle?by=operator&host=" + encodeURIComponent(oracleSt.host)); if (!r || !r.reach) return;
  const re = r.reach, names = r.names || {};
  $("#orMeta").textContent = r.edges + " conversations observed" + (r.demo ? " · sample" : "");
  const left = re.internal.map((e) => ({ e, peer: e.dst, out: true })).concat(re.inbound.map((e) => ({ e, peer: e.src, out: false })));
  const right = re.external.map((e) => ({ e, peer: e.name || e.dst, out: true, ext: true }));
  const maxB = Math.max(1, ...left.concat(right).map((x) => x.e.bytes));
  const row = (list, x, anchor) => list.slice(0, 9).map((it, i) => {
    const n = Math.min(9, list.length), y = 230 + (i - (n - 1) / 2) * Math.min(46, 400 / n), sw = (1 + 4 * Math.log10(1 + (9 * it.e.bytes) / maxB)).toFixed(1);
    const self = it.peer === r.self, cls = it.ext ? "ext" : self ? "self" : "int";
    const lx = anchor === "end" ? x - 12 : x + 12, mid = (450 + x) / 2;
    return '<path class="or-edge ' + cls + '" stroke-width="' + sw + '" d="M450 230 C' + mid + ' 230 ' + mid + ' ' + y + ' ' + x + ' ' + y + '"/>' +
      '<circle class="or-node ' + cls + '" cx="' + x + '" cy="' + y + '" r="6"/>' +
      '<text class="or-lbl" text-anchor="' + anchor + '" x="' + lx + '" y="' + (y - 3) + '">' + esc(it.ext ? it.peer : named(it.peer, names)) + (self ? " (this machine)" : "") + '</text>' +
      '<text class="or-sub" text-anchor="' + anchor + '" x="' + lx + '" y="' + (y + 10) + '">' + (it.out ? "→" : "←") + " port " + it.e.port + " · " + fmtBytes(it.e.bytes) + '</text>';
  }).join("");
  $("#orMap").innerHTML = '<text class="or-col" x="150" y="22" text-anchor="middle">INSIDE YOUR NETWORK · ' + left.length + '</text><text class="or-col" x="750" y="22" text-anchor="middle">OUTSIDE · ' + right.length + '</text>' +
    row(left, 250, "end") + row(right, 650, "start") +
    '<circle class="or-core" cx="450" cy="230" r="26"/><circle class="or-core2" cx="450" cy="230" r="34"/><text class="or-me" x="450" y="282" text-anchor="middle">' + esc(named(re.host, names)) + '</text><text class="or-sub" x="450" y="296" text-anchor="middle">' + esc(re.host) + ' · ' + fmtBytes(re.bytes) + '</text>';
  $("#orIso").textContent = re.isolating;
}
async function oracleWhatIf(rule) {
  if (rule != null) $("#orRule").value = rule;
  const text = $("#orRule").value.trim(); if (!text) return null;
  const r = await post("/api/oracle/whatif", { rule: text });
  if (!r || !r.ok) { $("#orResult").innerHTML = '<div class="m-row warn"><b>' + esc((r && r.error) || "could not evaluate that rule") + '</b></div>'; return r; }
  const w = r.result, names = r.names || {};
  $("#orResult").innerHTML = '<div class="or-sum"><b>' + w.matches.length + '</b> of ' + w.of + ' conversations would have matched · ' + fmtBytes(w.bytes) + ' · ' + w.hosts.length + ' host' + (w.hosts.length === 1 ? "" : "s") + '</div>' +
    (w.hosts.length ? '<div class="m-chips">' + w.hosts.map((h) => '<span class="m-chip">' + esc(named(h, names)) + '</span>').join("") + '</div>' : "") +
    w.matches.map((e) => '<div class="m-row"><div class="m-row-top"><b>' + esc(named(e.src, names)) + '</b><span class="m-dim">→ ' + esc(e.name || named(e.dst, names)) + '</span><time>port ' + e.port + ' · ' + fmtBytes(e.bytes) + '</time></div></div>').join("") +
    (w.matches.length ? "" : '<div class="m-dim">Nothing observed so far matches. The rule would have been silent.</div>');
  return r;
}
function oracleInit() {
  const ex = ["port 23", "unnamed", "fanout > 3", "bytes > 1000000", "src 192.168.1.99"];
  $("#orExamples").innerHTML = ex.map((x, i) => '<button class="hs-btn" data-i="' + i + '">' + esc(x) + '</button>').join("");
  $("#orExamples").querySelectorAll("button").forEach((b) => (b.onclick = () => oracleWhatIf(ex[+b.dataset.i])));
  $("#orRun").onclick = () => oracleWhatIf();
  $("#orRule").addEventListener("keydown", (e) => { if (e.key === "Enter") oracleWhatIf(); });
}

// ---- PULSE ----
// Key identity is used here only to pair a press with its release. It is never stored or sent.
const pulseCap = { on: false, down: new Map(), lastUp: 0, buf: [], trace: [], state: null, lock: false };
function pulsePref() { try { return localStorage.getItem("nw.pulse") === "on"; } catch (_) { return false; } }
function setPulse(on) {
  pulseCap.on = on; pulseCap.down.clear(); pulseCap.lastUp = 0;
  try { localStorage.setItem("nw.pulse", on ? "on" : "off"); } catch (_) {}
  $("#plToggle").textContent = on ? "PULSE ON" : "PULSE OFF"; $("#plToggle").classList.toggle("on", on);
  pulseRender();
  return on ? "PULSE on. Only key timing is measured." : "PULSE off.";
}
function pulseStroke(dwell, flight) {
  pulseCap.trace.push({ dwell, flight }); if (pulseCap.trace.length > 90) pulseCap.trace.shift();
  pulseCap.buf.push({ dwell: Math.round(dwell), flight: Math.round(flight) });
  if (pulseCap.buf.length >= 10) pulseFlush();
  if (currentDeck() === "pulse") pulseDraw();
}
async function pulseFlush() {
  if (!pulseCap.buf.length) return;
  const strokes = pulseCap.buf.splice(0);
  const r = await post("/api/pulse", { strokes });
  if (r && r.ok) { pulseCap.state = r.state; pulseCap.lock = r.lock; pulseRender(); }
}
document.addEventListener("keydown", (e) => {
  if (!pulseCap.on || e.repeat || e.ctrlKey || e.metaKey || e.altKey || (e.key.length !== 1 && e.key !== "Backspace" && e.key !== "Enter")) return;
  pulseCap.down.set(e.code, performance.now());
}, true);
document.addEventListener("keyup", (e) => {
  const t0 = pulseCap.down.get(e.code); if (t0 == null) return;
  pulseCap.down.delete(e.code);
  const now = performance.now();
  if (pulseCap.on && pulseCap.lastUp) pulseStroke(now - t0, t0 - pulseCap.lastUp);
  pulseCap.lastUp = now;
}, true);
function pulseDraw() {
  const c = $("#plTrace"), g = c.getContext("2d"), W = c.width, H = c.height, tr = pulseCap.trace;
  g.clearRect(0, 0, W, H);
  g.strokeStyle = "rgba(94,246,255,0.14)"; g.lineWidth = 1;
  for (let y = 0; y <= H; y += H / 4) { g.beginPath(); g.moveTo(0, y + 0.5); g.lineTo(W, y + 0.5); g.stroke(); }
  const bw = W / 90;
  tr.forEach((s, i) => { const h = Math.min(H - 4, (s.dwell / 300) * H); g.fillStyle = "rgba(255,84,73,0.75)"; g.fillRect(i * bw + 1, H - h, bw - 3, h); });
  g.strokeStyle = "#5ef6ff"; g.lineWidth = 2; g.beginPath();
  tr.forEach((s, i) => { const y = H - Math.min(H - 4, (Math.max(0, s.flight) / 600) * H) - 2; if (i) g.lineTo(i * bw + bw / 2, y); else g.moveTo(bw / 2, y); });
  g.stroke();
}
function pulseRender() {
  const st = pulseCap.state || { enrolled: false, progress: 0, needed: 150, match: 0 };
  $("#plMeta").textContent = pulseCap.on ? (st.enrolled ? "watching" : "enrolling") : "off";
  $("#plProgLabel").textContent = st.enrolled ? "ENROLLED · comparing every " + (pulseCap.window || 50) + " keystrokes" : "ENROLMENT · " + st.progress + " / " + st.needed + " keystrokes";
  $("#plProg").style.width = Math.round((100 * st.progress) / st.needed) + "%";
  const dial = $("#plMatch").parentElement;
  $("#plMatch").textContent = st.enrolled ? Math.round(st.match) + "%" : "—";
  $("#plMatchLabel").textContent = !st.enrolled ? "NOT ENROLLED" : st.mismatch ? "NOT THE OWNER" : "MATCH TO OWNER";
  dial.classList.toggle("bad", !!st.mismatch); dial.classList.toggle("ok", st.enrolled && !st.mismatch);
  $("#plLock").textContent = "LOCK ON MISMATCH: " + (pulseCap.lock ? "ON" : "OFF"); $("#plLock").classList.toggle("on", pulseCap.lock);
}
async function pulseLoad() {
  const r = await get("/api/pulse"); if (!r) return;
  pulseCap.state = r.state; pulseCap.lock = r.lock; pulseCap.window = r.window;
  pulseRender(); pulseDraw();
}
function pulseInit() {
  $("#plToggle").onclick = () => setPulse(!pulseCap.on);
  $("#plLock").onclick = async () => { const r = await post("/api/pulse", { lock: !pulseCap.lock }); if (r && r.ok) { pulseCap.lock = r.lock; pulseRender(); } };
  $("#plReset").onclick = async () => { const r = await post("/api/pulse", { reset: true }); if (r && r.ok) { pulseCap.state = r.state; pulseCap.trace = []; pulseRender(); pulseDraw(); status("typing rhythm forgotten"); } };
  if (pulsePref()) setPulse(true);
}

// ---- WARD ----
async function wardLoad() {
  const r = await get("/api/ward"); if (!r) return;
  const names = r.names || {}, enforcing = r.profiles.filter((p) => !p.learning).length;
  $("#wdMeta").textContent = enforcing + " enforced · " + (r.profiles.length - enforcing) + " learning" + (r.demo ? " · sample" : "");
  const top = (m, n) => Object.entries(m || {}).sort((a, b) => b[1] - a[1]).slice(0, n);
  const box = $("#wdProfiles");
  box.innerHTML = r.profiles.length ? r.profiles.map((p, i) => {
    const mx = Math.max(1, ...p.hours);
    return '<div class="wd-card' + (p.learning ? " learning" : "") + '"><div class="m-row-top"><b>' + esc(named(p.host, names)) + '</b><span class="m-dim m-mono">' + esc(p.host) + '</span><span class="m-verdict ' + (p.learning ? "warn" : "ok") + '">' + (p.learning ? "LEARNING" : "ENFORCED") + '</span><time>' + p.samples + ' observations</time><button class="hs-btn" data-i="' + i + '" title="forget this profile and learn it again">RELEARN</button></div>' +
      '<div class="wd-kv"><span class="micro cy">TALKS TO</span><span class="m-mono">' + (top(p.dsts, 5).map(([d]) => esc(d)).join(" · ") || "—") + '</span></div>' +
      '<div class="wd-kv"><span class="micro cy">PORTS</span><span class="m-mono">' + (top(p.ports, 8).map(([d]) => esc(d)).join(" · ") || "—") + '</span></div>' +
      '<div class="wd-kv"><span class="micro cy">ACTIVE HOURS</span><span class="wd-hours">' + p.hours.map((h, k) => '<i title="' + String(k).padStart(2, "0") + ':00" style="height:' + (h ? 4 + Math.round((10 * h) / mx) : 2) + 'px" class="' + (h ? "" : "off") + '"></i>').join("") + '</span></div></div>';
  }).join("") : '<div class="empty">no device has been watched long enough yet — WARD learns from live traffic</div>';
  box.querySelectorAll("button").forEach((b) => (b.onclick = async () => { await post("/api/ward", { action: "relearn", host: r.profiles[+b.dataset.i].host }); wardLoad(); }));
  $("#wdDevMeta").textContent = r.deviations.length + " open";
  const dv = $("#wdDevs");
  dv.innerHTML = r.deviations.length ? r.deviations.map((d, i) =>
    '<div class="m-row warn"><div class="m-row-top"><span class="badge resp">' + esc(d.kind.toUpperCase()) + '</span><b>' + esc(named(d.host, names)) + '</b><time>' + esc(ago(new Date(d.time).getTime())) + '</time></div><div class="m-mono">' + esc(d.detail) + '</div><div class="m-row-top"><span class="m-dim m-mono">saw: ' + esc(d.seen.dst) + ' port ' + d.seen.port + '</span><span class="spacer"></span><button class="hs-btn" data-i="' + i + '" title="this is fine — add it to the profile">ACCEPT AS NORMAL</button></div></div>').join("")
    : '<div class="empty">every enforced device is inside its profile</div>';
  dv.querySelectorAll("button").forEach((b) => (b.onclick = async () => { await post("/api/ward", { action: "accept", seen: r.deviations[+b.dataset.i].seen }); wardLoad(); }));
}

// ---- QUANTUM ----
let quantumWasBusy = false;
async function quantumLoad() {
  const r = await get("/api/quantum"); if (!r) return null;
  const f = r.findings, names = r.names || {}, isHNDL = (x) => /quantum-vulnerable/.test(x);
  const hndl = f.filter((x) => !x.pq).length, other = f.filter((x) => x.issues.some((i) => !isHNDL(i))).length;
  $("#qtMeta").textContent = r.busy ? "inventory running…" : r.at ? "last inventory " + ago(r.at) + (r.demo ? " · sample" : "") : "not run yet";
  $("#qtTotal").textContent = f.length; $("#qtPQ").textContent = r.pq; $("#qtHNDL").textContent = hndl; $("#qtOther").textContent = other;
  $("#qtScan").disabled = r.busy; $("#qtScan").textContent = r.busy ? "… READING HANDSHAKES" : "⌁ INVENTORY THE NETWORK";
  if (quantumWasBusy && !r.busy) status("inventory finished: " + f.length + " services, " + r.pq + " post-quantum ready");
  quantumWasBusy = r.busy;
  $("#qtList").innerHTML = f.length ? '<div class="qt-row qt-headrow"><span>SERVICE</span><span>PROTOCOL</span><span>KEY</span><span>KEY EXCHANGE</span><span>CERT</span><span>FINDINGS</span></div>' + f.map((x) => {
    const host = x.addr.replace(/:\d+$/, ""), port = x.addr.slice(host.length + 1), bad = x.issues.filter((i) => !isHNDL(i));
    return '<div class="qt-row ' + (x.pq ? "ok" : bad.length ? "bad" : "warn") + '"><span><b>' + esc(named(host, names)) + '</b><i class="m-mono">' + esc(host) + ' : ' + esc(port) + '</i></span><span><b>' + esc(x.proto.toUpperCase()) + '</b><i class="m-mono">' + esc(x.version) + '</i></span><span class="m-mono">' + esc(x.key || "—") + '</span><span><span class="m-verdict ' + (x.pq ? "ok" : "bad") + '">' + (x.pq ? "POST-QUANTUM" : "CLASSICAL") + '</span><i class="m-mono">' + esc(x.kex || "") + '</i></span><span class="m-mono">' + (x.proto === "tls" ? (x.daysLeft < 0 ? "expired " + -x.daysLeft + " d ago" : x.daysLeft + " d left") : "—") + '</span><span>' + (x.issues.length ? x.issues.map((i) => '<p class="' + (isHNDL(i) ? "hndl" : "") + '">' + esc(i) + '</p>').join("") : '<p class="fine">nothing to fix</p>') + '</span></div>';
  }).join("") : '<div class="empty">' + (r.busy ? "reading handshakes…" : "run an inventory to see what your network's encryption would be worth against a quantum computer") + '</div>';
  return r;
}
async function quantumScan() { const r = await post("/api/quantum"); if (r && r.ok) status("reading TLS and SSH handshakes on your network…"); await quantumLoad(); return r; }

// ---- LEDGER ----
const KIND_BADGE = { alert: "bad", action: "", note: "act" };
async function ledgerLoad(recheck) {
  const r = await get("/api/ledger" + (recheck ? "?recheck=1" : "")); if (!r) return null;
  $("#lgMeta").textContent = r.len + " entries · " + (r.stored ? "written to ledger.jsonl" : "in memory only");
  const seal = $("#lgSeal");
  seal.textContent = r.intact ? "✓ CHAIN INTACT" : "✕ CHAIN BROKEN AT #" + r.bad;
  seal.className = "lg-seal " + (r.intact ? "ok" : "bad");
  $("#lgHead").textContent = r.head ? r.head.slice(0, 16) + "…" : "—"; $("#lgHead").title = r.head || "";
  if (recheck) status(r.intact ? "every link checks out" + (r.disk ? " · " + r.disk : "") : "the chain is broken at entry " + r.bad + (r.disk ? " · " + r.disk : ""));
  $("#lgList").innerHTML = r.entries.length ? r.entries.map((e) =>
    '<div class="lg-entry ' + (e.kind === "alert" ? sevClass(e.severity) : e.kind) + (!r.intact && e.seq >= r.bad ? " broken" : "") + '"><div class="lg-link"><i></i></div><div class="lg-body"><div class="m-row-top"><span class="lg-seq">#' + e.seq + '</span><span class="badge ' + (KIND_BADGE[e.kind] || "") + '">' + esc((e.kind === "alert" ? e.severity : e.kind).toUpperCase()) + '</span>' + (e.source ? '<b>' + esc(e.source) + '</b>' : "") + '<time>' + esc(new Date(e.time).toLocaleString("en-GB")) + '</time></div><div class="m-mono">' + esc(e.text) + '</div><div class="lg-hash m-mono">' + esc((e.prev || "genesis").slice(0, 12)) + ' → ' + esc(e.hash.slice(0, 12)) + '</div></div></div>').join("")
    : '<div class="empty">nothing recorded yet</div>';
  return r;
}
async function ledgerReport(mins) {
  const r = await get("/api/ledger/report" + (mins ? "?mins=" + mins : "")); if (!r) return null;
  $("#lgReport").textContent = r.report; $("#lgRepMeta").textContent = r.entries + " entries"; $("#lgCopy").disabled = false;
  return r;
}
function ledgerInit() {
  const note = async () => { const v = $("#lgNote").value.trim(); if (!v) return; const r = await post("/api/ledger/note", { text: v }); if (r && r.ok) { $("#lgNote").value = ""; status("recorded as entry #" + r.seq); ledgerLoad(); } };
  $("#lgNoteAdd").onclick = note; $("#lgNote").addEventListener("keydown", (e) => { if (e.key === "Enter") note(); });
  $("#lgVerify").onclick = () => ledgerLoad(true);
  document.querySelectorAll('[data-deck="ledger"] [data-mins]').forEach((b) => (b.onclick = () => ledgerReport(+b.dataset.mins)));
  $("#lgCopy").onclick = async () => { try { await navigator.clipboard.writeText($("#lgReport").textContent); status("report copied"); } catch (_) { status("copy is not available here — select the text instead"); } };
}

const MOD_DECKS = { decoy: decoyLoad, cortex: cortexLoad, spectrum: spectrumLoad, oracle: oracleLoad, pulse: pulseLoad, ward: wardLoad, quantum: quantumLoad, ledger: () => ledgerLoad() };
function modPoll() { const f = MOD_DECKS[currentDeck()]; if (f && !document.hidden) f(); }
function modInit() { decoyInit(); oracleInit(); pulseInit(); ledgerInit(); $("#qtScan").onclick = quantumScan; }

// test hooks (also handy from the browser console)
window.netwatch = { room, addSegment, proxIndex, proxStep, sonarHosts: () => sonarHosts, dance, bdOpen, bdExit, sonarNodes: () => sonarState.nodes, sonarMode: () => sonarMode, setSonarMode, sonarTrail: () => sonarTrail.length, runCommand, audioPosFor, playAlert, setAudio, scrubTo, goLive, state: () => STATE, cortex, cortexOpen, cortexDecide, oracleWhatIf, pulseCap, setPulse, pulseStroke, pulseFlush, quantumScan, ledgerLoad, ledgerReport, decoyAct };

// ---------- boot ----------
initNeural();
runBoot();
initTicker();
killchainDeck.init();
attackDeck.init();
modInit();
connect();
setTimeout(() => { if (!live) loadMock(); }, 600);
setTimeout(loadHistory, 900);
setInterval(loadHistory, 10000);
setInterval(sonarPoll, 400);
sysPoll(); setInterval(sysPoll, 1500);
setInterval(() => { if (currentDeck() === "sonar") bdRefresh(); }, 2000);
setInterval(sleepPoll, 30000);
setInterval(modPoll, 2000);
setInterval(pulseFlush, 4000);
(function sonarLoop() {
  bdTick();
  if (currentDeck() === "sonar") { const t = performance.now() / 1000; if (sonarMode === "3d") sonar3dRender(t); else sonarDraw(t); }
  requestAnimationFrame(sonarLoop);
})();
