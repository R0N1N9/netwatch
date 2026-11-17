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
}

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
  const order = ["netwatch", "netrunner", "ruview"];
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

// ---------- boot ----------
connect();
setTimeout(() => { if (!live) loadMock(); }, 600);
