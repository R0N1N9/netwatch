"use strict";

// ---------- state ----------
let STATE = { meta: {}, devices: [], alerts: [], stats: {} };
let selectedIP = null;
let live = false;

const $ = (s) => document.querySelector(s);
const el = (tag, cls, txt) => { const e = document.createElement(tag); if (cls) e.className = cls; if (txt != null) e.textContent = txt; return e; };

// ---------- clock ----------
setInterval(() => { $("#clock").textContent = new Date().toLocaleTimeString("en-GB"); }, 1000);

// ---------- deck tabs ----------
document.querySelectorAll(".tab").forEach((t) => {
  t.addEventListener("click", () => selectDeck(t.dataset.deck));
});
function selectDeck(name) {
  document.querySelectorAll(".tab").forEach((t) => t.classList.toggle("active", t.dataset.deck === name));
  document.querySelectorAll(".deck").forEach((d) => (d.hidden = d.dataset.deck !== name));
  if (name === "netwatch" && selectedIP) lockReticle(selectedIP);
  else $("#reticle").hidden = true;
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
}

function threats() { return (STATE.alerts || []).filter((a) => a.severity === "CRITICAL").length; }

function renderDevices() {
  const list = $("#deviceList"); list.innerHTML = "";
  if (!STATE.devices.length) { list.appendChild(mk("empty", "scanning segment…")); return; }
  STATE.devices.forEach((d) => {
    const row = el("div", "dev");
    if (d.ip === selectedIP) row.classList.add("sel");
    if (d.quarantined) row.classList.add("quar");
    row.appendChild(el("span", "ip", d.ip));
    row.appendChild(el("span", "name", d.hostname || d.vendor || "unknown"));
    const tags = el("div", "tags");
    if (d.self) tags.appendChild(el("span", "chip you", "YOU"));
    if (d.gateway) tags.appendChild(el("span", "chip gw", "GW"));
    if (d.trusted) tags.appendChild(el("span", "chip trust", "✓"));
    if (d.quarantined) tags.appendChild(el("span", "chip quar", "LOCKED"));
    row.appendChild(tags);
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
  $("#bwTarget").innerHTML = d ? `target: <b>${d.ip}</b> ${d.hostname ? "(" + d.hostname + ")" : ""}` : "no host selected";
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
  const res = await post("/api/copilot", { query: q });
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
  const order = ["netwatch", "netrunner", "killchain", "ruview"];
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
  $("#bootLogo").textContent = "KIROSHI OPTIC  ·  MK.4  //  NETRUNNER NEURAL UPLINK";
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
    setTimeout(() => (boot.style.display = "none"), 700);
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
      'PKT/s <span class="hl">' + ((Math.random() * 9000 + 800) | 0) + "</span>",
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
  const risk = riskScore(d);
  const rc = risk >= 50 ? "risk-hi" : "risk-ok";
  $("#rtag").innerHTML =
    '<b>◣ TARGET LOCK</b><br>' +
    '<span class="lo">IP </span>' + d.ip + "<br>" +
    '<span class="lo">MAC</span> ' + (d.mac || "--") + "<br>" +
    '<span class="lo">VND</span> ' + (d.vendor || "unknown") + "<br>" +
    '<span class="lo">ROLE</span> ' + (d.self ? "SELF" : d.gateway ? "GATEWAY" : d.trusted ? "TRUSTED" : "UNVERIFIED") + "<br>" +
    '<span class="lo">THREAT</span> <span class="' + rc + '">' + risk + "/100</span>";
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
    { side: "red", ttl: "Delivery vector brief", att: "T1566", type: "info", info: ["Common vectors: spear-phish attachment/link, watering-hole, removable media, supply-chain.", "PLANNING ONLY — netrunner does not build phishing or delivery infrastructure."] },
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

// ---------- boot ----------
runBoot();
initTicker();
renderKcFlow();
renderKcStage();
connect();
setTimeout(() => { if (!live) loadMock(); }, 600);
