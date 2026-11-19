"use strict";

// ---------- state ----------
let STATE = { meta: {}, devices: [], alerts: [], stats: {} };
let selectedIP = null;
let live = false;
let tickerRefresh = null, tickerPrimed = false;

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

// ---------- live stream (SSE) ----------
function connect() {
  const es = new EventSource("/api/stream");
  es.onmessage = (ev) => {
    try { STATE = JSON.parse(ev.data); live = true; render(); } catch (_) {}
  };
  es.onerror = () => { if (!live) { loadMock(); } };
}

// ---------- render ----------
function render() {
  const m = STATE.meta || {}, s = STATE.stats || {};
  $("#segMeta").textContent = `${m.iface || "--"} · ${m.subnet || "--"}`;
  $("#statusText").textContent = m.sweeping ? "SCANNING" : (m.watching ? "WATCH ACTIVE" : "IDLE");
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
$("#copilotSend").addEventListener("click", sendCopilot);
$("#copilotInput").addEventListener("keydown", (e) => { if (e.key === "Enter") sendCopilot(); });
async function sendCopilot() {
  const inp = $("#copilotInput"); const q = inp.value.trim(); if (!q) return;
  addCp("user", "OPERATOR", q); inp.value = "";
  const thinking = addCp("ai", "DAEMON", "▌ parsing…");
  neuralBusy = true;
  const res = await post("/api/copilot", { query: q });
  setTimeout(() => { neuralBusy = false; }, 900);
  thinking.querySelector(".body").textContent = res && res.answer ? res.answer : "uplink down — backend not reachable.";
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
  if (e.key === "Tab") { e.preventDefault(); cycleDeck(); }
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    const ips = STATE.devices.map((d) => d.ip); if (!ips.length) return;
    let i = ips.indexOf(selectedIP); i = e.key === "ArrowDown" ? Math.min(ips.length - 1, i + 1) : Math.max(0, i - 1);
    selectDevice(ips[i]); e.preventDefault();
  }
});
function cycleDeck() {
  const order = ["netwatch", "netrunner", "killchain", "grid", "ar", "ruview"];
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
let kcStage = 0;
function kcCoverage(st) {
  const api = st.actions.filter((a) => a.type === "api").length;
  return Math.round((api / st.actions.length) * 100);
}
function renderKcFlow() {
  const flow = $("#kcFlow");
  flow.innerHTML = "";
  KILLCHAIN.forEach((st, i) => {
    const n = el("div", "kc-node" + (i === kcStage ? " active" : ""));
    n.innerHTML = '<div class="num">0' + (i + 1) + '</div><div class="nm">' + st.name + '</div><div class="cov"><i style="width:' + kcCoverage(st) + '%"></i></div>';
    n.addEventListener("click", () => { kcStage = i; renderKcFlow(); renderKcStage(); });
    flow.appendChild(n);
  });
}
function renderKcStage() {
  const st = KILLCHAIN[kcStage];
  const title = (kcStage + 1) + " · " + st.name;
  $("#kcStageTitle").textContent = title;
  $("#kcAttck").textContent = "ATT&CK " + st.attck;
  const body = $("#kcStageBody");
  body.innerHTML = "";
  [["red", "◢ RED · OFFENSE (authorized/lab)"], ["blue", "◤ BLUE · DEFENSE"]].forEach(([side, label]) => {
    const acts = st.actions.filter((a) => a.side === side);
    if (!acts.length) return;
    const sec = el("div", "kc-sec " + side);
    sec.appendChild(el("h4", null, label));
    acts.forEach((a) => {
      const row = el("div", "kc-tech");
      const ttl = el("span", "ttl", a.ttl);
      const att = el("span", "att", a.att);
      const btn = el("button", "go" + (a.type === "info" ? " info" : ""), a.btn || "INFO");
      btn.addEventListener("click", () => runKc(a));
      row.appendChild(ttl); row.appendChild(att); row.appendChild(btn);
      sec.appendChild(row);
    });
    body.appendChild(sec);
  });
  scramble($("#kcStageTitle"), title, 450);
}
async function runKc(a) {
  if (a.type === "info") { opLog(a.side, a.ttl, a.info); return; }
  if (a.endpoint === "/api/sweep") { await post("/api/sweep"); opLog(a.side, a.ttl, ["sweep dispatched → results populate the NETWATCH deck"]); return; }
  let body = {};
  if (a.needsTarget) {
    const t = $("#kcTarget").value.trim();
    if (!t) { opLog(a.side, a.ttl, ["⚠ enter a lab target (an IP you own / are authorized to test) in the field below"]); return; }
    body = { target: t };
  }
  const bodyEl = opLog(a.side, a.ttl, ["▌ executing…"]);
  const res = await post(a.endpoint, body);
  bodyEl.textContent = (res && res.lines ? res.lines : ["no response / backend offline"]).join("\n");
}
function opLog(side, head, lines) {
  const log = $("#kcOpsLog");
  const op = el("div", "op" + (side === "red" ? " red" : ""));
  op.appendChild(el("span", "ohead", "▸ " + head + "  ·  " + new Date().toLocaleTimeString("en-GB")));
  const bodyEl = el("div", "obody");
  bodyEl.textContent = (lines || []).join("\n");
  op.appendChild(bodyEl);
  log.insertBefore(op, log.firstChild);
  return bodyEl;
}

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
let ramSig = "", neuralBusy = false, booting = true;

function renderHud() {
  const devs = STATE.devices || [], m = STATE.meta || {};
  const risky = devs.filter((d) => threatOf(d) >= 50).length;
  $("#ramText").textContent = (devs.length - risky) + "/" + devs.length;
  const sig = devs.length + ":" + risky;
  if (sig !== ramSig) {
    ramSig = sig;
    const bar = $("#ramBar"); bar.innerHTML = "";
    for (let i = 0; i < devs.length; i++) {
      const seg = el("i", i >= devs.length - risky ? "risk" : "");
      seg.style.animationDelay = (i * 0.05) + "s";
      bar.appendChild(seg);
    }
  }
  const thr = threats();
  $("#ppsVal").textContent = m.pps || 0;
  $("#thrVal").textContent = thr;
  $("#thrStat").classList.toggle("hot", thr > 0);
  const link = $("#linkState");
  link.textContent = thr > 0 ? "INTRUSION DETECTED" : "SYNCED";
  link.classList.toggle("bad", thr > 0);
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

// ---------- boot ----------
initNeural();
runBoot();
initTicker();
renderKcFlow();
renderKcStage();
connect();
setTimeout(() => { if (!live) loadMock(); }, 600);
