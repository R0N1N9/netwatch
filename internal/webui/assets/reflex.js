"use strict";
// REFLEX (scenes, gestures, threat light, phone companion), HUNT, X-RAY and
// the talking decoy. Loaded after senses.js and xray.js.

// ---------- cues from the server: say this, do that ----------
const cueSt = { seq: null };
RENDER_HOOKS.push(() => {
  const c = liveState && liveState.meta && liveState.meta.cue; if (!c) return;
  if (cueSt.seq === null) { cueSt.seq = c.seq; return; }
  if (c.seq <= cueSt.seq) return;
  cueSt.seq = c.seq; cueSt.last = c;
  if (c.say) { narr.log.push("scene: " + c.say); if ("speechSynthesis" in window) { const u = new SpeechSynthesisUtterance(String(c.say).slice(0, 200)); u.pitch = 0.55; window.speechSynthesis.speak(u); } status("scene: " + c.say); }
  if (c.ui === "next-deck") cycleDeck();
});

// ---------- REFLEX deck ----------
const reflex = { data: null };
const TRIGGER_WORDS = { tripwire: "a tripwire is crossed", presence: "movement on a link", alert: "an alert is raised", away: "everyone leaves", return: "someone returns", gesture: "a wave gesture" };
const ACTION_WORDS = { clip: "save a clip of the room", speak: "say something", alert: "raise an alert", note: "write a LEDGER note", light: "flash the light", ack: "acknowledge the newest alert", arm: "arm SONAR", disarm: "disarm SONAR", "next-deck": "switch deck", lock: "lock the screen" };
function reflexRender(r) {
  reflex.data = r;
  $("#rfMeta").textContent = r.scenes.length + " scenes" + (r.listening ? " · listening for gestures" : "") + (r.demo ? " · sample" : "");
  const box = $("#rfScenes");
  box.innerHTML = r.scenes.length ? r.scenes.map((sc, i) =>
    '<div class="m-row rf-scene' + (sc.off ? " off" : "") + '"><div class="m-row-top"><b>' + esc(sc.name) + '</b>' + (sc.armedOnly ? '<span class="badge">ARMED ONLY</span>' : "") + '<span class="spacer"></span><button class="hs-btn" data-fire="' + i + '">TEST</button><button class="hs-btn' + (sc.off ? "" : " on") + '" data-toggle="' + i + '">' + (sc.off ? "OFF" : "ON") + '</button><button class="hs-btn" data-rm="' + i + '">✕</button></div>' +
    '<div class="m-mono"><span class="m-dim">WHEN</span> ' + esc(TRIGGER_WORDS[sc.trigger] || sc.trigger) + (sc.match ? ' <span class="m-dim">·</span> ' + esc(sc.match) : "") + ' <span class="m-dim">→ DO</span> ' + sc.actions.map((a) => esc(ACTION_WORDS[a] || a)).join(", ") + (sc.say && sc.actions.includes("speak") ? ' <span class="m-dim">(“' + esc(sc.say) + '”)</span>' : "") + '</div></div>').join("")
    : '<div class="empty">no scenes yet — build one below</div>';
  box.querySelectorAll("button").forEach((b) => (b.onclick = async () => {
    const d = b.dataset, sc = r.scenes[+(d.fire || d.toggle || d.rm)];
    const body = d.fire ? { fire: sc.id } : d.rm ? { remove: sc.id } : { put: Object.assign({}, sc, { off: !sc.off }) };
    const x = await post("/api/reflex", body); if (x && x.ok) reflexRender(x);
  }));
  if (!$("#rfTrigger").options.length) {
    $("#rfTrigger").innerHTML = r.triggers.map((t) => '<option value="' + esc(t) + '">' + esc(TRIGGER_WORDS[t] || t) + '</option>').join("");
    $("#rfActions").innerHTML = r.actions.map((a) => '<label class="rf-check"><input type="checkbox" value="' + esc(a) + '" /> ' + esc(ACTION_WORDS[a] || a) + '</label>').join("");
    $("#rfTrigger").onchange = reflexMatches;
  }
  reflexMatches();
  $("#rfFired").innerHTML = r.fired.length ? r.fired.map((f) => '<div class="m-row"><div class="m-row-top"><b>' + esc(f.scene) + '</b><span class="m-dim">' + esc(f.cause) + '</span><time>' + esc(ago(f.at)) + '</time></div><div class="m-mono">' + esc((f.did || []).join(" · ") || "nothing to do") + '</div></div>').join("") : '<div class="m-dim">Nothing has fired yet. TEST runs a scene now.</div>';
  // light
  const c = r.light.color || {}, sw = $("#ltSwatch");
  sw.style.setProperty("--lc", "rgb(" + (c.r || 0) + "," + (c.g || 0) + "," + (c.b || 0) + ")"); sw.className = "lt-swatch " + (c.pulse || "steady");
  $("#ltWhy").textContent = (c.why || "—") + " · " + (c.pulse || "");
  $("#ltMeta").textContent = r.light.sending ? "sending to " + r.light.addr : r.light.addr ? "address set · demo sends nothing" : "preview only";
  if (document.activeElement !== $("#ltAddr") && !$("#ltAddr").value) $("#ltAddr").value = r.light.addr || "";
  // companion
  const cp = r.companion || {};
  $("#cpMeta").textContent = cp.on ? "listening" : "off";
  $("#cpBody").innerHTML = cp.on
    ? '<p>Open this on a phone on your network, accept the certificate warning once, and press START:</p><p class="m-mono cp-url">' + esc(cp.url) + '</p><p><b>Pairing code</b> <span class="cp-code">' + esc(cp.code) + '</span> · <b>certificate</b> <span class="m-mono">' + esc(cp.fingerprint) + '…</span></p><p class="m-dim">The phone gets alerts (as vibration patterns) and the X-RAY view, read-only. It cannot reach the console or change anything.</p>'
    : '<p>A read-only page for your phone: each kind of alert buzzes in its own pattern, and X-RAY shows what SONAR senses through the phone\'s camera.</p><p class="m-dim">It is off. Start NetWatch with <code>' + esc(cp.flag) + '</code> to serve it over HTTPS on your network, behind a pairing code. The console itself stays on localhost.</p>';
}
function reflexMatches() {
  const r = reflex.data; if (!r) return;
  const t = $("#rfTrigger").value, opts = t === "tripwire" ? r.wires : t === "presence" ? r.nodes : t === "alert" ? ["critical", "warn", "decoy", "new-device", "arp-spoof", "ward", "spectrum", "pulse", "usb"] : t === "gesture" ? ["double-wave", "triple-wave"] : [];
  $("#rfMatches").innerHTML = opts.map((o) => '<option value="' + esc(o) + '">').join("");
  $("#rfMatch").disabled = t === "away" || t === "return";
}
async function reflexLoad() { const r = await get("/api/reflex"); if (r) reflexRender(r); return r; }
async function reflexSave() {
  const actions = [...document.querySelectorAll("#rfActions input:checked")].map((i) => i.value);
  const r = await post("/api/reflex", { put: { name: $("#rfName").value, trigger: $("#rfTrigger").value, match: $("#rfMatch").disabled ? "" : $("#rfMatch").value, armedOnly: $("#rfArmed").checked, actions, say: $("#rfSay").value } });
  if (r && r.ok) { $("#rfName").value = ""; $("#rfMatch").value = ""; $("#rfSay").value = ""; document.querySelectorAll("#rfActions input").forEach((i) => (i.checked = false)); reflexRender(r); status("scene saved"); }
  return r;
}
$("#rfSave").onclick = reflexSave;
$("#rfWave").onclick = async () => { const r = await post("/api/reflex", { gesture: "double-wave" }); if (r && r.ok) { reflexRender(r); status(r.note); } };
$("#ltSet").onclick = async () => { const r = await post("/api/reflex", { lightAddr: $("#ltAddr").value }); if (r && r.ok) { reflexRender(r); status("threat light address saved"); } };
$("#ltTest").onclick = async () => { const r = await post("/api/reflex", { lightTest: true }); if (r && r.ok) { reflexRender(r); status(r.note); } };
MOD_DECKS.reflex = reflexLoad;

// ---------- HUNT: home in on one radio ----------
const hunt = { id: null, name: "", timer: null, last: null, hist: [], sound: true, next: 0 };
// Seconds between ticks for a signal strength: slow when far, a buzz when on top of it.
function huntInterval(rssi) { const k = Math.max(0, Math.min(1, (rssi + 92) / 56)); return +(1.3 * Math.pow(0.04, k)).toFixed(3); }
function huntTick() {
  if (!hunt.sound || hunt.last == null || !actx) return;
  const t = actx.currentTime;
  while (hunt.next < t + 0.3) {
    const at = Math.max(hunt.next, t + 0.01), o = actx.createOscillator(), g = actx.createGain();
    o.type = "square"; o.frequency.value = 1400; o.connect(g).connect(actx.destination);
    g.gain.setValueAtTime(0.0001, at); g.gain.exponentialRampToValueAtTime(0.07, at + 0.003); g.gain.exponentialRampToValueAtTime(0.0001, at + 0.03);
    o.start(at); o.stop(at + 0.04); hunt.next = at + huntInterval(hunt.last);
  }
}
async function huntPoll() {
  if (!hunt.id) return;
  const r = await get("/api/spectrum?hunt=" + encodeURIComponent(hunt.id)); if (!r || !r.hunt || !hunt.id) return;
  const h = r.hunt, best = h.readings[0];
  if (!best) { $("#huntDbm").textContent = "—"; $("#huntTrend").textContent = "LOST — out of range of every node"; $("#huntTrend").className = "hunt-trend cold"; hunt.last = null; return; }
  hunt.hist.push(best.rssi); if (hunt.hist.length > 12) hunt.hist.shift();
  const old = hunt.hist.length >= 6 ? hunt.hist.slice(0, 3).reduce((a, v) => a + v, 0) / 3 : best.rssi, now = hunt.hist.slice(-3).reduce((a, v) => a + v, 0) / Math.min(3, hunt.hist.length), d = now - old;
  hunt.last = best.rssi;
  $("#huntDbm").textContent = best.rssi;
  $("#huntBar").style.width = Math.round(Math.max(0, Math.min(1, (best.rssi + 92) / 56)) * 100) + "%";
  $("#huntTrend").textContent = d > 2.5 ? "▲ WARMER" : d < -2.5 ? "▼ COLDER" : best.rssi > -45 ? "● RIGHT HERE" : "— HOLDING";
  $("#huntTrend").className = "hunt-trend " + (d > 2.5 || best.rssi > -45 ? "warm" : d < -2.5 ? "cold" : "");
  $("#huntNodes").innerHTML = h.readings.map((x) => '<span class="m-chip">' + esc(x.node) + ' <b>' + x.rssi + '</b></span>').join("");
  huntTick();
}
function huntStart(radio) {
  if (!radio) return null;
  huntStop();
  hunt.id = radio.id; hunt.name = radio.name || radio.id; hunt.hist = []; hunt.last = null; hunt.next = 0;
  if (!actx) { const AC = window.AudioContext || window.webkitAudioContext; if (AC) actx = new AC(); }
  if (actx && actx.state === "suspended") actx.resume();
  $("#huntName").textContent = hunt.name; $("#huntBox").hidden = false; $("#spScope").style.opacity = 0.15;
  hunt.timer = setInterval(huntPoll, 300); hunt.ticker = setInterval(huntTick, 100); huntPoll();
  return hunt.id;
}
function huntStop() { clearInterval(hunt.timer); clearInterval(hunt.ticker); hunt.id = null; hunt.last = null; $("#huntBox").hidden = true; $("#spScope").style.opacity = 1; }
$("#huntStop").onclick = huntStop;
$("#huntSound").onclick = () => { hunt.sound = !hunt.sound; $("#huntSound").textContent = "TICKS " + (hunt.sound ? "ON" : "OFF"); $("#huntSound").classList.toggle("on", hunt.sound); };
$("#huntSound").classList.add("on");

// ---------- X-RAY in the AR deck ----------
const xr = { on: false, pose: { x: 0, y: 0.95, heading: 0, pitch: 0 }, fov: 62, view: null, timer: null, shown: 0, drag: null, compass: false };
function xrMap() {
  const c = $("#xrMap"), g = c.getContext("2d"), v = xr.view || {}, S = c.width, P = (p) => [S / 2 + (p.x / 1.3) * (S / 2), S / 2 + (p.y / 1.3) * (S / 2)];
  g.clearRect(0, 0, S, S); g.strokeStyle = "rgba(94,246,255,0.35)"; g.lineWidth = 1;
  (v.walls || []).forEach((w) => { g.beginPath(); g.moveTo(...P(w.a)); g.lineTo(...P(w.b)); g.stroke(); });
  xrayItems(v).forEach((it) => { const [x, y] = P(it); g.fillStyle = it.kind === "presence" ? "#ff5449" : it.kind === "host" ? "#5ef6ff" : it.kind === "ap" ? "#bdf9ff" : "#1dffa1"; g.fillRect(x - 3, y - 3, 6, 6); });
  const [mx, my] = P(xr.pose), a = ((xr.pose.heading - 90) * Math.PI) / 180, h = (xr.fov / 2) * (Math.PI / 180);
  g.fillStyle = "rgba(29,255,161,0.16)"; g.beginPath(); g.moveTo(mx, my); g.arc(mx, my, 70, a - h, a + h); g.closePath(); g.fill();
  g.fillStyle = "#1dffa1"; g.beginPath(); g.arc(mx, my, 5, 0, 7); g.fill();
}
function xrDraw() {
  if (!xr.on || !xr.view) return;
  xr.shown = xrayDraw($("#xrLayer"), xr.view, xr.pose, xr.fov);
  $("#xrHeadVal").textContent = Math.round(xr.pose.heading) + "°"; $("#xrPitchVal").textContent = Math.round(xr.pose.pitch) + "°";
  if (document.activeElement !== $("#xrHead")) $("#xrHead").value = Math.round(xr.pose.heading);
  xrMap();
}
async function xrPoll() { const v = await get("/api/sonar"); if (v) { xr.view = v; xrDraw(); } }
function setXray(on) {
  xr.on = on; clearInterval(xr.timer);
  $("#xrLayer").hidden = !on; $("#xrPanel").hidden = !on; $("#arOverlay").style.display = on ? "none" : "";
  $("#xrBtn").textContent = "X-RAY " + (on ? "ON" : "OFF"); $("#xrBtn").classList.toggle("on", on);
  if (on) { xr.timer = setInterval(xrPoll, 400); xrPoll(); $("#arNote").textContent = "X-RAY — SONAR's view of the room from where you stand. Positions are as good as the node layout you set."; }
  return on ? "X-ray on." : "X-ray off.";
}
$("#xrBtn").onclick = () => setXray(!xr.on);
$("#xrHead").oninput = (e) => { xr.pose.heading = +e.target.value; xr.compass = false; xrDraw(); };
$("#xrPitch").oninput = (e) => { xr.pose.pitch = +e.target.value; xrDraw(); };
$("#xrMap").onclick = (e) => { const b = e.target.getBoundingClientRect(); xr.pose.x = ((e.clientX - b.left) / b.width - 0.5) * 2.6; xr.pose.y = ((e.clientY - b.top) / b.height - 0.5) * 2.6; xrDraw(); };
(function xrLook() {
  const st = document.querySelector(".ar-stage");
  st.addEventListener("pointerdown", (e) => { if (xr.on && !e.target.closest(".xr-panel, .ar-controls")) xr.drag = { x: e.clientX, y: e.clientY }; });
  st.addEventListener("pointermove", (e) => { if (!xr.drag) return; xr.pose.heading = (xr.pose.heading - (e.clientX - xr.drag.x) * 0.15 + 360) % 360; xr.pose.pitch = Math.max(-40, Math.min(40, xr.pose.pitch + (e.clientY - xr.drag.y) * 0.1)); xr.drag = { x: e.clientX, y: e.clientY }; xr.compass = false; xrDraw(); });
  window.addEventListener("pointerup", () => { xr.drag = null; });
  window.addEventListener("deviceorientationabsolute", (e) => { const h = xrayHeading(e); if (h != null && xr.on && !xr.drag) { xr.compass = true; xr.pose.heading = h; if (typeof e.beta === "number") xr.pose.pitch = Math.max(-40, Math.min(40, e.beta - 90)); xrDraw(); } });
})();

// ---------- talking decoys in the DECOY deck ----------
const talk = { visits: [], playing: 0 };
function decoyExtra(r) {
  const ports = r.talking || [], box = $("#dcTalk");
  box.innerHTML = ports.length ? ports.map((p, i) => '<span class="m-chip">tcp/' + p + '<b data-i="' + i + '" title="close">✕</b></span>').join("") : '<span class="m-dim">none open</span>';
  box.querySelectorAll("b").forEach((b) => (b.onclick = () => decoyAct({ action: "talk-close", port: ports[+b.dataset.i] })));
  talk.visits = r.visits || [];
  $("#dcVisitMeta").textContent = talk.visits.length + " recorded";
  if (talk.playing) return; // do not redraw under a replay
  const vb = $("#dcVisits");
  vb.innerHTML = talk.visits.length ? talk.visits.map((v, i) => '<div class="m-row crit"><div class="m-row-top"><span class="badge bad">VISIT</span><b>' + esc(v.source) + '</b><span class="m-dim">→ tcp/' + v.port + ' · ' + ((v.end - v.start) / 1000).toFixed(1) + ' s · ' + v.lines.filter((l) => l.dir === "in").length + ' lines typed</span><time>' + esc(ago(v.end)) + '</time><button class="hs-btn" data-i="' + i + '">▶ REPLAY</button></div><div class="m-mono">' + esc(v.summary) + '</div></div>').join("")
    : '<div class="empty">nobody has talked to a decoy</div>';
  vb.querySelectorAll("button").forEach((b) => (b.onclick = () => talkReplay(talk.visits[+b.dataset.i])));
}
// Replay a visit line by line, at the speed it happened (capped), like a terminal recording.
async function talkReplay(v, fast) {
  if (!v) return 0;
  const run = ++talk.playing, vb = $("#dcVisits");
  vb.innerHTML = '<div class="talk-term"><div class="m-row-top"><span class="micro">▚▚ REPLAY · ' + esc(v.source) + ' → tcp/' + v.port + '</span><span class="spacer"></span><button class="hs-btn" id="talkClose">CLOSE</button></div><pre id="talkOut"></pre><div class="m-mono talk-sum">' + esc(v.summary) + '</div></div>';
  $("#talkClose").onclick = () => { talk.playing = 0; decoyLoad(); };
  const out = $("#talkOut"); let prev = v.start, n = 0;
  for (const l of v.lines) {
    if (run !== talk.playing) return n;
    await new Promise((res) => setTimeout(res, fast ? 5 : Math.min(700, Math.max(60, l.t - prev)))); prev = l.t;
    const span = document.createElement("span"); span.className = l.dir; span.textContent = (l.dir === "in" ? "  ◀ " : "") + l.text + "\n"; out.appendChild(span); out.scrollTop = out.scrollHeight; n++;
  }
  return n;
}
(function talkInit() {
  const go = async () => { const p = parseInt($("#dcTalkPort").value, 10); if (!(p >= 1 && p <= 65535)) { status("a port is a number from 1 to 65535"); return; } const r = await decoyAct({ action: "talk-open", port: p }); if (r && r.ok) $("#dcTalkPort").value = ""; };
  $("#dcTalkAdd").onclick = go; $("#dcTalkPort").addEventListener("keydown", (e) => { if (e.key === "Enter") go(); });
})();

// ---------- commands ----------
EXTRA_COMMANDS.push(async (t) => {
  let m;
  if ((m = t.match(/^hunt\s+(.+)$/))) { const r = await get("/api/spectrum"); if (!r) return "I could not read the spectrum."; const q = m[1].replace(/[^a-z0-9]/g, ""), x = r.radios.find((k) => (k.name || "").toLowerCase().replace(/[^a-z0-9]/g, "").includes(q) || k.id.replace(/:/g, "").includes(q)) || (/(stranger|rogue|lingering)/.test(m[1]) && r.radios.find((k) => k.tag === "lingering")) || (/tracker/.test(m[1]) && r.radios.find((k) => k.tag === "tracker?")); if (!x) return "No radio in range matches " + m[1] + "."; selectDeck("spectrum"); huntStart(x); return "Hunting " + (x.name || x.id) + ". Carry a node towards it; the ticks speed up as you close in."; }
  if (/^(stop hunt|stop hunting|end hunt)$/.test(t)) { huntStop(); return "Hunt stopped."; }
  if ((m = t.match(/^x-?ray (on|off)$/))) { selectDeck("ar"); return setXray(m[1] === "on"); }
  if ((m = t.match(/^(?:fire|test|run) scene\s+(.+)$/))) { const r = await reflexLoad(); const sc = r && r.scenes.find((s) => s.name.toLowerCase().includes(m[1])); if (!sc) return "No scene matches " + m[1] + "."; const x = await post("/api/reflex", { fire: sc.id }); if (x && x.ok) reflexRender(x); return x && x.ok ? "Fired " + sc.name + ": " + ((x.fired[0] && x.fired[0].did) || []).join(", ") + "." : "It did not fire."; }
  if (/^(scenes|list scenes)$/.test(t)) { const r = await reflexLoad(); return r && r.scenes.length ? r.scenes.map((s) => s.name + (s.off ? " (off)" : "")).join("\n") : "There are no scenes."; }
  if (/^(light test|test (the )?light)$/.test(t)) { const r = await post("/api/reflex", { lightTest: true }); return r && r.ok ? "The light " + r.note.replace(/^the light /, "") + "." : "I could not do that."; }
  if (/^(companion|phone|pair( my)? phone)$/.test(t)) { const r = await reflexLoad(); const c = r && r.companion; return c && c.on ? "Open " + c.url + " on your phone. Pairing code " + c.code.split("").join(" ") + "." : "The companion is off. Start NetWatch with --companion 0.0.0.0:7443."; }
  if ((m = t.match(/^(?:open|start) (?:a )?talking decoy(?: on)?(?: port)? (\d+)$/))) { const r = await decoyAct({ action: "talk-open", port: +m[1] }); return r && r.ok ? "A talking decoy is listening on port " + m[1] + "." : "Could not open it: " + ((r && r.error) || "backend unreachable"); }
  return null;
});

Object.assign(window.netwatch, { reflex, reflexLoad, reflexSave, cueSt, hunt, huntStart, huntStop, huntInterval, xr, setXray, xrDraw, talk, talkReplay });
