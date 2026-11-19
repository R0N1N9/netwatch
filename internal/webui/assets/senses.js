"use strict";
// NETDIVE, incident rehearsal, "while you were out", live commentary,
// sonification, scanner mode and the operator fatigue meter.
// Loaded after app.js and shares its globals. Network strings go through esc().

// ---------- NETDIVE: the network as a city ----------
const nd = { r: null, failed: false, az: 0.7, el: 0.5, dist: 34, tx: 0, tz: 0, drag: null, lastUser: 0, sig: "", edges: [], names: {}, flows: [], towers: {}, keys: {}, moved: 0 };
const ND_COL = { self: 0x1dffa1, gw: 0x5ef6ff, ok: 0xbdf9ff, unv: 0xffb84a, bad: 0xff5449, ext: 0xff3c8e };
function ndClass(d) { return d.quarantined || threatOf(d) >= 70 ? "bad" : d.self ? "self" : d.gateway ? "gw" : d.trusted ? "ok" : "unv"; }
function netdiveInit() {
  if (nd.r) return true;
  if (nd.failed || typeof THREE === "undefined") { nd.failed = true; return false; }
  try {
    const host = $("#netdive3d");
    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
    host.appendChild(renderer.domElement);
    const scene = new THREE.Scene(), camera = new THREE.PerspectiveCamera(50, 1, 0.1, 400);
    scene.fog = new THREE.FogExp2(0x050203, 0.012);
    const grid = new THREE.GridHelper(120, 60, 0xff5449, 0x143a44); grid.material.transparent = true; grid.material.opacity = 0.45; scene.add(grid);
    const city = new THREE.Group(); scene.add(city);
    const ray = new THREE.Raycaster();
    host.addEventListener("pointerdown", (e) => { nd.drag = { x: e.clientX, y: e.clientY }; nd.moved = 0; host.setPointerCapture(e.pointerId); });
    host.addEventListener("pointermove", (e) => { if (!nd.drag) return; const dx = e.clientX - nd.drag.x, dy = e.clientY - nd.drag.y; nd.moved += Math.abs(dx) + Math.abs(dy); nd.az += dx * 0.006; nd.el = Math.max(0.08, Math.min(1.4, nd.el + dy * 0.005)); nd.drag = { x: e.clientX, y: e.clientY }; nd.lastUser = performance.now(); });
    host.addEventListener("pointerup", (e) => {
      const click = nd.drag && nd.moved < 6; nd.drag = null; if (!click) return;
      const b = host.getBoundingClientRect();
      ray.setFromCamera(new THREE.Vector2(((e.clientX - b.left) / b.width) * 2 - 1, -((e.clientY - b.top) / b.height) * 2 + 1), camera);
      const hit = ray.intersectObjects(Object.values(nd.towers).map((t) => t.body))[0];
      netdivePick(hit ? hit.object.userData.ip : null);
    });
    host.addEventListener("wheel", (e) => { e.preventDefault(); nd.dist = Math.max(8, Math.min(90, nd.dist + e.deltaY * 0.03)); nd.lastUser = performance.now(); }, { passive: false });
    nd.r = { renderer, scene, camera, city, host, w: 0, h: 0 };
    return true;
  } catch (e) { nd.failed = true; return false; }
}
function netdivePick(ip) {
  nd.picked = ip;
  const box = $("#ndInfo"), d = ip && STATE.devices.find((x) => x.ip === ip);
  if (!d) { box.hidden = true; return null; }
  const out = nd.edges.filter((e) => e.src === ip), inn = nd.edges.filter((e) => e.dst === ip);
  box.hidden = false;
  box.innerHTML = '<div class="micro">▚▚ TOWER</div><b>' + esc(d.hostname || d.ip) + '</b><div class="m-mono">' + esc(d.ip) + ' · ' + esc(d.vendor || "unknown maker") + '</div><div class="m-mono">threat ' + threatOf(d) + '/100 · ' + out.length + ' out · ' + inn.length + ' in · ' + fmtBytes(out.concat(inn).reduce((a, e) => a + e.bytes, 0)) + '</div>' + (d.quarantined ? '<div class="m-verdict bad">BEHIND THE BLACKWALL</div>' : "") +
    '<div class="m-chips"><button class="hs-btn" data-a="lock">LOCK ON</button><button class="hs-btn" data-a="reach">REACH</button></div>';
  box.querySelector('[data-a="lock"]').onclick = () => { selectDeck("netwatch"); selectDevice(ip); };
  box.querySelector('[data-a="reach"]').onclick = () => { oracleSt.host = ip; selectDeck("oracle"); };
  return d;
}
function netdiveBuild() {
  const R = nd.r, devs = STATE.devices || [];
  const sig = devs.map((d) => d.ip + ndClass(d) + (d.quarantined ? "q" : "")).join("|") + "#" + nd.edges.length + "#" + nd.edges.reduce((a, e) => a + e.bytes, 0);
  if (sig === nd.sig) return;
  nd.sig = sig;
  while (R.city.children.length) R.city.remove(R.city.children[0]);
  nd.towers = {}; nd.flows = [];
  const vol = {}; nd.edges.forEach((e) => { vol[e.src] = (vol[e.src] || 0) + e.bytes; vol[e.dst] = (vol[e.dst] || 0) + e.bytes; });
  const ring = devs.filter((d) => !d.gateway).sort((a, b) => a.ip.localeCompare(b.ip, undefined, { numeric: true }));
  devs.forEach((d) => {
    const i = ring.indexOf(d), ang = i < 0 ? 0 : (i / Math.max(1, ring.length)) * Math.PI * 2 + 0.4, rad = i < 0 ? 0 : 11;
    const x = rad * Math.cos(ang), z = rad * Math.sin(ang), cls = ndClass(d), col = ND_COL[cls];
    const h = (d.gateway ? 5 : 2.2) + 1.5 * Math.log10(1 + (vol[d.ip] || 0) / 1000), w = d.gateway ? 2.4 : 1.7;
    const g = new THREE.Group(); g.position.set(x, 0, z);
    const geo = new THREE.BoxGeometry(w, h, w);
    const body = new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ color: col, transparent: true, opacity: 0.16, depthWrite: false })); body.position.y = h / 2; body.userData.ip = d.ip; g.add(body);
    const edge = new THREE.LineSegments(new THREE.EdgesGeometry(geo), new THREE.LineBasicMaterial({ color: col })); edge.position.y = h / 2; g.add(edge);
    for (let y = 0.6; y < h; y += 0.6) { const fl = new THREE.LineLoop(new THREE.BufferGeometry().setFromPoints([[-1, -1], [1, -1], [1, 1], [-1, 1]].map(([a, b]) => new THREE.Vector3((a * w) / 2, y, (b * w) / 2))), new THREE.LineBasicMaterial({ color: col, transparent: true, opacity: 0.25 })); g.add(fl); }
    const label = textSprite((d.hostname || d.ip).toUpperCase().slice(0, 22), "#" + col.toString(16).padStart(6, "0")); label.scale.set(5.2, 0.98, 1); label.position.set(-w / 2, h + 0.8, 0); g.add(label);
    if (d.quarantined) {
      const cage = new THREE.LineSegments(new THREE.EdgesGeometry(new THREE.BoxGeometry(w + 1.6, h + 1.2, w + 1.6, 3, 4, 3)), new THREE.LineBasicMaterial({ color: 0xff5449 })); cage.position.y = (h + 1.2) / 2; g.add(cage);
      const wall = new THREE.Mesh(new THREE.BoxGeometry(w + 1.6, h + 1.2, w + 1.6), new THREE.MeshBasicMaterial({ color: 0xff5449, transparent: true, opacity: 0.12, wireframe: true })); wall.position.y = (h + 1.2) / 2; g.add(wall);
      const bw = textSprite("BLACKWALL", "#ff5449"); bw.scale.set(4.2, 0.8, 1); bw.position.set(-w / 2, h + 1.9, 0); g.add(bw);
    }
    R.city.add(g);
    nd.towers[d.ip] = { g, body, top: new THREE.Vector3(x, h, z), cls, quarantined: !!d.quarantined };
  });
  // outside destinations float in a ring above the city
  const ext = [...new Set(nd.edges.filter((e) => e.external && nd.towers[e.src]).map((e) => e.dst))].slice(0, 14), sky = {};
  ext.forEach((dst, i) => {
    const ang = (i / ext.length) * Math.PI * 2, p = new THREE.Vector3(26 * Math.cos(ang), 15 + 2 * Math.sin(i * 1.7), 26 * Math.sin(ang));
    const m = new THREE.Mesh(new THREE.OctahedronGeometry(0.7), new THREE.MeshBasicMaterial({ color: ND_COL.ext, wireframe: true })); m.position.copy(p); R.city.add(m);
    const e0 = nd.edges.find((e) => e.dst === dst), l = textSprite(String((e0 && e0.name) || dst).slice(0, 26), "#ff3c8e"); l.scale.set(4.6, 0.86, 1); l.position.copy(p).add(new THREE.Vector3(0.9, 0.2, 0)); R.city.add(l);
    sky[dst] = p;
  });
  nd.edges.forEach((e, i) => {
    const a = nd.towers[e.src], b = nd.towers[e.dst], to = b ? b.top : sky[e.dst];
    if (!a || !to) return;
    const mid = a.top.clone().add(to).multiplyScalar(0.5); mid.y += e.external ? 3 : 4 + (i % 3);
    const curve = new THREE.QuadraticBezierCurve3(a.top, mid, to), col = a.quarantined || (b && b.quarantined) ? 0x5a2622 : e.external ? ND_COL.ext : a.cls === "bad" ? ND_COL.bad : 0x5ef6ff;
    R.city.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(curve.getPoints(24)), new THREE.LineBasicMaterial({ color: col, transparent: true, opacity: 0.35 })));
    if (a.quarantined || (b && b.quarantined)) return; // nothing moves through the wall
    const dot = new THREE.Mesh(new THREE.SphereGeometry(0.16, 8, 6), new THREE.MeshBasicMaterial({ color: col })); R.city.add(dot);
    nd.flows.push({ curve, dot, speed: 0.08 + 0.05 * Math.log10(1 + e.bytes / 1000), off: (i * 0.37) % 1 });
  });
  $("#ndMeta").textContent = devs.length + " towers · " + nd.flows.length + " live flows · " + ext.length + " outside" + (replay ? " · REPLAY" : "");
}
async function netdiveLoad() {
  const ok = netdiveInit();
  $("#ndNone").hidden = ok;
  if (!ok) return;
  const r = await get("/api/oracle?all=1"); if (r && r.list) { nd.edges = r.list; nd.names = r.names || {}; }
  netdiveBuild();
}
function netdiveRender(t) {
  const R = nd.r; if (!R) return;
  const w = R.host.clientWidth, h = R.host.clientHeight;
  if (w !== R.w || h !== R.h) { R.w = w; R.h = h; R.renderer.setSize(w, h, false); R.camera.aspect = w / Math.max(1, h); R.camera.updateProjectionMatrix(); }
  netdiveBuild();
  if (performance.now() - nd.lastUser > 6000) nd.az += 0.0012; // drift until the pilot takes over
  const sp = 0.35, fx = Math.cos(nd.az), fz = Math.sin(nd.az);
  if (nd.keys.w) { nd.tx -= fx * sp; nd.tz -= fz * sp; } if (nd.keys.s) { nd.tx += fx * sp; nd.tz += fz * sp; }
  if (nd.keys.a) { nd.tx -= fz * sp; nd.tz += fx * sp; } if (nd.keys.d) { nd.tx += fz * sp; nd.tz -= fx * sp; }
  R.camera.position.set(nd.tx + nd.dist * Math.cos(nd.el) * Math.cos(nd.az), nd.dist * Math.sin(nd.el) + 1, nd.tz + nd.dist * Math.cos(nd.el) * Math.sin(nd.az));
  R.camera.lookAt(nd.tx, 3, nd.tz);
  nd.flows.forEach((f) => f.dot.position.copy(f.curve.getPoint((t * f.speed + f.off) % 1)));
  R.renderer.render(R.scene, R.camera);
}
document.addEventListener("keydown", (e) => { if (currentDeck() === "netdive" && !/INPUT|TEXTAREA/.test(document.activeElement.tagName) && "wasd".includes(e.key)) { nd.keys[e.key] = true; nd.lastUser = performance.now(); } });
document.addEventListener("keyup", (e) => { delete nd.keys[e.key]; });
(function netdiveLoop() { if (currentDeck() === "netdive") netdiveRender(performance.now() / 1000); requestAnimationFrame(netdiveLoop); })();
MOD_DECKS.netdive = netdiveLoad;
Object.assign(DECK_WORDS, { netdive: "netdive", "net dive": "netdive", cyberspace: "netdive", dojo: "dojo", rehearsal: "dojo", reflex: "reflex" });

// ---------- DOJO: incident rehearsal ----------
const dojo = { data: null };
function dojoRender() {
  const r = dojo.data; if (!r) return;
  const st = r.state, box = $("#djBody");
  if (st && (st.active || st.finished)) {
    const now = Date.now();
    box.innerHTML = '<div class="dj-top ' + (st.finished ? "done" : "live") + '"><div><span class="micro">' + (st.finished ? "FINISHED" : "IN PROGRESS · SYNTHETIC") + '</span><b>' + esc(st.scenario.name) + '</b><span class="m-dim">target ' + esc(st.host) + '</span></div><div class="dj-score"><b>' + st.score + '</b><span class="micro cy">/ 100</span></div></div>' +
      (st.finished ? '<div class="dj-verdict">' + esc(st.verdict) + '</div>' : '<div class="m-dim dj-brief">' + esc(st.scenario.brief) + '</div>') +
      st.steps.map((s) => {
        const state = !s.injected ? "wait" : s.done ? "done" : s.expect ? "open" : "info";
        const right = !s.injected ? "not yet" : s.done ? "+" + s.points + " in " + s.seconds.toFixed(0) + " s" : s.expect ? (st.finished ? "missed" : ((now - s.injected) / 1000).toFixed(0) + " s") : "";
        return '<div class="m-row dj-step ' + state + '"><div class="m-row-top"><span class="badge' + (s.severity === "CRITICAL" ? " bad" : "") + '">' + esc(s.severity) + '</span><b>' + (s.injected ? esc(s.message) : "…") + '</b><time>' + esc(right) + '</time></div>' + (s.injected && s.expect ? '<div class="m-dim">' + (s.done ? "✓ " : "→ ") + esc(s.hint) + '</div>' : "") + '</div>';
      }).join("") +
      st.penalties.map((p) => '<div class="m-row crit"><b>' + esc(p) + '</b></div>').join("") +
      '<div class="btn-row" style="padding-left:0">' + (st.finished ? '<button class="btn" id="djAgain">BACK TO SCENARIOS</button>' : '<button class="btn danger" id="djStop">STOP</button>') + '</div>';
    if ($("#djStop")) $("#djStop").onclick = async () => { await post("/api/rehearsal", { stop: true }); dojoLoad(); };
    if ($("#djAgain")) $("#djAgain").onclick = () => { dojo.hideFinished = st.started; dojoRender(); };
    if (!(st.finished && dojo.hideFinished === st.started)) return;
  }
  box.innerHTML = r.scenarios.map((sc, i) => '<div class="dj-card"><div class="m-row-top"><b>' + esc(sc.name) + '</b><span class="m-dim">' + sc.steps.length + ' beats · about a minute</span><span class="spacer"></span><button class="btn" data-i="' + i + '">START</button></div><div class="m-dim">' + esc(sc.brief) + '</div></div>').join("") +
    '<div class="m-note">A rehearsal plays a scripted incident into the live console. Its alerts are marked [REHEARSAL], its intruder is a made-up host, and a quarantine during it is simulated — the firewall is never touched. You are scored on doing the right thing quickly.</div>' +
    (r.scores.length ? '<div class="micro cy" style="margin-top:10px">▚ PAST RUNS</div>' + r.scores.map((x) => '<div class="m-row"><div class="m-row-top"><b>' + x.score + '/100</b><span class="m-dim">' + esc(x.scenario) + ' — ' + esc(x.verdict || "stopped") + '</span><time>' + esc(ago(x.at)) + '</time></div></div>').join("") : "");
  box.querySelectorAll("button[data-i]").forEach((b) => (b.onclick = () => rehearse(r.scenarios[+b.dataset.i].id)));
}
async function dojoLoad() { const r = await get("/api/rehearsal"); if (r) { dojo.data = r; dojoRender(); } operatorLoad(); }
async function rehearse(id) {
  if (replay) { status("go live first"); return null; }
  dojo.hideFinished = 0;
  const r = await post("/api/rehearsal", { start: id }); await dojoLoad();
  return r;
}
MOD_DECKS.dojo = dojoLoad;
setInterval(() => { if (currentDeck() === "dojo" && dojo.data && dojo.data.state && dojo.data.state.active) dojoLoad(); }, 700);

// ---------- operator watch ----------
const operator = { on: false, lastPing: 0, reading: null };
function operatorRender(r) {
  operator.on = r.on; operator.reading = r.reading; operatorLevel = r.on && r.reading ? r.reading.level : "";
  const rd = r.reading, cls = !rd ? "" : rd.level === "sharp" ? "ok" : rd.level === "tiring" ? "warn" : "bad";
  $("#opToggle").textContent = "OPERATOR WATCH " + (r.on ? "ON" : "OFF"); $("#opToggle").classList.toggle("on", r.on);
  $("#opMeta").textContent = r.on ? "at the console " + Math.round(r.sessionMin || 0) + " min" : "watch off";
  $("#opSharp").textContent = rd ? rd.sharpness : "—"; $("#opLevel").textContent = rd ? rd.level.toUpperCase() : "NOT WATCHING";
  $("#opDial").className = "pl-dial " + (cls === "warn" ? "" : cls) + (cls === "warn" ? " tiring" : "");
  if (rd) { $("#opFactors").className = ""; $("#opFactors").innerHTML = rd.factors.length ? rd.factors.map((f) => '<div class="cx-step"><span class="cx-w up">−' + f.cost + '</span><div><b>' + esc(f.name) + '</b><span>' + esc(f.detail) + '</span></div></div>').join("") : '<div class="m-dim">Nothing is costing you sharpness right now.</div>'; $("#opAdvice").textContent = rd.advice; }
  else { $("#opAdvice").textContent = ""; }
  $("#opStat").hidden = !rd; if (rd) { $("#opVal").textContent = rd.sharpness; $("#opStat").classList.toggle("hot", rd.level === "spent"); }
}
async function operatorLoad() { const r = await get("/api/operator"); if (r) operatorRender(r); return r; }
async function setOperator(on) { const r = await post("/api/operator", { on }); if (r) operatorRender(r); return r; }
$("#opToggle").onclick = () => setOperator(!operator.on);
["keydown", "pointerdown"].forEach((ev) => document.addEventListener(ev, () => { if (operator.on && Date.now() - operator.lastPing > 20000) { operator.lastPing = Date.now(); post("/api/operator", { ping: true }); } }, true));
setInterval(() => { if (operator.on) operatorLoad(); }, 20000);

// ---------- while you were out ----------
const recapSt = { seq: null, playing: 0 };
async function recapShow(rc) {
  if (!rc) return null;
  const run = ++recapSt.playing, box = $("#recapBox");
  $("#rcHead").textContent = rc.headline;
  $("#rcStats").innerHTML = [[rc.critical, "CRITICAL", "bad"], [rc.warnings, "WARNINGS", "warn"], [rc.joined.length, "JOINED", ""], [rc.left.length, "LEFT", ""], [rc.moves, "MOVEMENTS", ""]].map(([n, l, c]) => '<div class="qt-stat"><b class="' + c + '">' + n + '</b><span class="micro cy">' + l + '</span></div>').join("") +
    (rc.joined.length ? '<div class="m-mono rc-who">+ ' + rc.joined.map(esc).join(" · ") + '</div>' : "") + (rc.left.length ? '<div class="m-mono rc-who">− ' + rc.left.map(esc).join(" · ") + '</div>' : "");
  $("#rcEvents").innerHTML = ""; $("#rcBar").style.width = "0%";
  box.hidden = false;
  if (narr.on) narrSay(rc.headline);
  const showUpTo = (ts) => { $("#rcEvents").innerHTML = rc.events.filter((e) => e.t <= ts).reverse().map((e) => '<div class="m-row ' + sevClass(e.severity) + '"><div class="m-row-top"><span class="badge' + (e.severity === "CRITICAL" ? " bad" : "") + '">' + esc(e.severity) + '</span><time>' + esc(fmtTime(e.t)) + '</time></div><div class="m-mono">' + esc(e.text) + '</div></div>').join("") || '<div class="m-dim">nothing yet…</div>'; };
  await loadHistory();
  const idx = hist.map((h, i) => i).filter((i) => hist[i].ts >= rc.from && hist[i].ts <= rc.to), steps = Math.min(idx.length, 24);
  for (let k = 0; k < steps && run === recapSt.playing && !box.hidden; k++) {
    const i = idx[Math.round((k / Math.max(1, steps - 1)) * (idx.length - 1))];
    await scrubTo(i); showUpTo(hist[i].ts); $("#rcBar").style.width = Math.round(((k + 1) / steps) * 100) + "%";
    await new Promise((res) => setTimeout(res, 450));
  }
  if (run === recapSt.playing) { goLive(); showUpTo(rc.to); $("#rcBar").style.width = "100%"; }
  return rc;
}
function recapClose() { recapSt.playing++; $("#recapBox").hidden = true; goLive(); }
$("#rcClose").onclick = recapClose;
async function recapNow(mins) { const r = await post("/api/recap", { mins: mins || 30 }); if (!r || !r.ok) return null; recapSt.seq = (recapSt.seq || 0) + 1; recapShow(r.recap); return r.recap; }
$("#rcPlay").onclick = () => recapNow(30);
RENDER_HOOKS.push(() => {
  const m = (liveState && liveState.meta) || {};
  $("#rehTag").hidden = !m.rehearsal;
  if (m.recap == null) return;
  if (recapSt.seq === null) { recapSt.seq = m.recap; return; }
  if (m.recap > recapSt.seq) { recapSt.seq = m.recap; get("/api/recap").then((r) => r && recapShow(r.recap)); }
});

// ---------- live commentary ----------
const narr = { on: false, log: [] };
const NARR = {
  "new-device": (a) => "New device on the segment" + (octet(a.source) ? ", " + octet(a.source) : "") + ".",
  "arp-spoof": () => "Warning. The gateway's address just changed. Possible interception.",
  beacon: (a) => (octet(a.source) ? "Host " + octet(a.source) + " is" : "Something is") + " phoning home on a timer.",
  dns: () => "Suspicious name lookup.",
  anomaly: (a) => "Unusual behaviour" + (octet(a.source) ? " from " + octet(a.source) : "") + ".",
  decoy: () => "Decoy tripped. Someone is probing.",
  "decoy-drill": () => "Decoy drill received. The alarm works.",
  ward: (a) => (octet(a.source) ? "Host " + octet(a.source) : "A device") + " stepped outside its profile.",
  spectrum: (a) => /look|announcing/.test(a.message) ? "Look-alike access point in range." : /disconnect/.test(a.message) ? "Disconnect flood in the air." : /tracker/.test(a.message) ? "A tracker tag is lingering nearby." : "Unknown radio lingering nearby.",
  pulse: () => "The typing at this keyboard is not yours.",
  tripwire: () => "Tripwire crossed.",
  presence: () => "Movement in the room.",
  usb: () => "New U S B device.",
  blackwall: (a) => "Blackwall engaged" + (octet(a.source) ? " on " + octet(a.source) : "") + ".",
  operator: () => "You are running low. Consider stopping.",
  fall: () => "Possible fall detected.",
};
function octet(src) { const m = /^\d+\.\d+\.\d+\.(\d+)$/.exec(src || ""); return m ? "dot " + m[1] : ""; }
function narrLine(a) {
  const reh = /^\[REHEARSAL\]/.test(a.message || ""), f = NARR[a.kind];
  const line = f ? f(a) : a.severity === "CRITICAL" ? "Critical alert." : null;
  return line ? (reh ? "Rehearsal. " : "") + line : null;
}
function narrSay(text) {
  narr.log.push(text); if (narr.log.length > 40) narr.log.shift();
  if (!("speechSynthesis" in window) || window.speechSynthesis.pending) return;
  const u = new SpeechSynthesisUtterance(text); u.rate = 1.02; u.pitch = 0.55; window.speechSynthesis.speak(u);
}
function setNarrate(on) {
  narr.on = on; try { localStorage.setItem("nw.narrate", on ? "on" : "off"); } catch (_) {}
  $("#narrBtn").textContent = "NARRATE " + (on ? "ON" : "OFF"); $("#narrBtn").classList.toggle("on", on);
  if (on) narrSay("Commentary online.");
  return on ? "Commentary on." : "Commentary off.";
}
$("#narrBtn").onclick = () => setNarrate(!narr.on);
ALERT_HOOKS.push((a) => {
  if (a.severity === "CRITICAL") { document.body.classList.add("crit-glitch"); setTimeout(() => document.body.classList.remove("crit-glitch"), 900); }
  if (!narr.on) return;
  const line = narrLine(a); if (line) narrSay(line);
});

// ---------- sonification ----------
const sonify = { on: false, nodes: null, timer: null, next: 0 };
// The whole mapping from network state to sound, in one place.
function sonifyParams(st) {
  const pps = (st.meta && st.meta.pps) || 0, crit = (st.alerts || []).filter((a) => a.severity === "CRITICAL" && !a.acked).length, hosts = (st.devices || []).length;
  const act = Math.min(1, Math.log10(1 + pps) / 4); // 0 at silence, 1 at 10k packets a second
  return { cutoff: Math.round(180 + 2400 * act), interval: +(1.3 - 1.15 * act).toFixed(2), tension: +Math.min(0.06, crit * 0.02).toFixed(3), pitch: Math.round(440 + 40 * Math.min(12, hosts)) };
}
function sonifyStep() {
  const n = sonify.nodes; if (!n) return;
  const p = sonifyParams(STATE), t = actx.currentTime;
  n.filter.frequency.setTargetAtTime(p.cutoff, t, 0.6); n.tension.gain.setTargetAtTime(p.tension, t, 0.8);
  while (sonify.next < t + 0.6) { // schedule the pulse a little ahead
    const at = Math.max(sonify.next, t + 0.02), o = actx.createOscillator(), g = actx.createGain();
    o.type = "sine"; o.frequency.value = p.pitch; o.connect(g).connect(n.master);
    g.gain.setValueAtTime(0.0001, at); g.gain.exponentialRampToValueAtTime(0.05, at + 0.01); g.gain.exponentialRampToValueAtTime(0.0001, at + 0.09);
    o.start(at); o.stop(at + 0.1); sonify.next = at + p.interval;
  }
}
function setSonify(on) {
  if (on && !actx) { const AC = window.AudioContext || window.webkitAudioContext; if (AC) actx = new AC(); }
  if (on && !actx) { status("this browser has no audio output"); on = false; }
  if (on && actx.state === "suspended") actx.resume();
  if (on && !sonify.nodes) {
    const master = actx.createGain(); master.gain.value = 0.9; master.connect(actx.destination);
    const filter = actx.createBiquadFilter(); filter.type = "lowpass"; filter.frequency.value = 200; filter.Q.value = 4;
    const drone = actx.createGain(); drone.gain.value = 0.05; filter.connect(drone).connect(master);
    const oscs = [55, 55.4, 110.3].map((f) => { const o = actx.createOscillator(); o.type = "sawtooth"; o.frequency.value = f; o.connect(filter); o.start(); return o; });
    const tension = actx.createGain(); tension.gain.value = 0; tension.connect(master);
    const to = actx.createOscillator(); to.type = "triangle"; to.frequency.value = 77.8 * 2; to.connect(tension); to.start(); oscs.push(to);
    sonify.nodes = { master, filter, tension, oscs }; sonify.next = 0;
    sonify.timer = setInterval(sonifyStep, 250); sonifyStep();
  }
  if (!on && sonify.nodes) { clearInterval(sonify.timer); sonify.nodes.oscs.forEach((o) => o.stop()); sonify.nodes.master.disconnect(); sonify.nodes = null; }
  sonify.on = on; $("#sonifyBtn").textContent = "SONIFY " + (on ? "ON" : "OFF"); $("#sonifyBtn").classList.toggle("on", on);
  return on ? "Sonification on." : "Sonification off.";
}
$("#sonifyBtn").onclick = () => setSonify(!sonify.on);
ALERT_HOOKS.push((a) => { // a bell for each alert, lower and longer the worse it is
  if (!sonify.on || !sonify.nodes) return;
  const t = actx.currentTime, o = actx.createOscillator(), g = actx.createGain(), f = a.severity === "CRITICAL" ? 196 : a.severity === "WARN" ? 392 : 784;
  o.type = "triangle"; o.frequency.value = f; o.connect(g).connect(sonify.nodes.master);
  g.gain.setValueAtTime(0.0001, t); g.gain.exponentialRampToValueAtTime(0.12, t + 0.02); g.gain.exponentialRampToValueAtTime(0.0001, t + (a.severity === "CRITICAL" ? 2.2 : 0.9));
  o.start(t); o.stop(t + 2.3);
});

// ---------- scanner mode: hold Q ----------
const scan = { open: false, pinned: false, hosts: [], sel: null, armed: null };
async function scanOpen(pin) {
  if (scan.open) { if (pin) scan.pinned = true; return; }
  scan.open = true; scan.pinned = !!pin; scan.sel = null; scan.armed = null;
  document.body.classList.add("scanning"); $("#scanMode").hidden = false;
  $("#scHint").textContent = pin ? "Esc to close" : "release Q to close · click a host to pin";
  const r = await get("/api/scanner"); if (!r || !scan.open) return;
  scan.hosts = r.hosts;
  const box = $("#scHosts");
  box.innerHTML = r.hosts.map((h, i) => { const cls = h.quarantined ? "quar" : h.verdict === "likely threat" ? "bad" : h.verdict === "needs a look" ? "warn" : h.role === "unverified" ? "unv" : "ok"; return '<div class="sc-host ' + cls + '" data-i="' + i + '"><b>' + esc(h.name || h.ip) + '</b><span class="m-mono">' + esc(h.ip) + '</span><span class="m-verdict ' + (cls === "bad" || cls === "quar" ? "bad" : cls === "warn" || cls === "unv" ? "warn" : "ok") + '">' + esc((h.quarantined ? "quarantined" : h.verdict || h.role).toUpperCase()) + '</span></div>'; }).join("");
  box.querySelectorAll(".sc-host").forEach((n) => { n.onmouseenter = () => scanFocus(+n.dataset.i); n.onclick = () => { scan.pinned = true; $("#scHint").textContent = "pinned · Esc to close"; scanFocus(+n.dataset.i); }; });
  const first = r.hosts.findIndex((h) => h.ip === selectedIP); scanFocus(first >= 0 ? first : 0);
}
function scanClose() { scan.open = false; scan.pinned = false; document.body.classList.remove("scanning"); $("#scanMode").hidden = true; }
function scanFocus(i) {
  const h = scan.hosts[i]; if (!h) return;
  scan.sel = i; scan.armed = null;
  document.querySelectorAll(".sc-host").forEach((n) => n.classList.toggle("sel", +n.dataset.i === i));
  const rows = [["ROLE", h.role], ["MAKER", h.vendor || "unknown"], ["SYSTEM", h.os || "not fingerprinted"], ["TALKS TO", h.inside + " inside · " + h.outside + " outside" + (h.outsideNames && h.outsideNames.length ? " (" + h.outsideNames.join(", ") + ")" : "")], ["BLACKWALL WOULD STOP", h.toThis + " of " + (h.inside + h.outside) + " conversations"], ["ALERTS", h.alerts + (h.worst ? " · worst " + h.worst : "")], ["DECOYS TOUCHED", h.trips], ["OUTSIDE ITS PROFILE", h.deviations + " time" + (h.deviations === 1 ? "" : "s")]];
  if (h.beacon) rows.push(["CALLING HOME", h.beacon]);
  const canQ = h.role !== "self" && h.role !== "gateway";
  $("#scDetail").innerHTML = '<div class="sc-name">' + esc(h.name || h.ip) + '</div>' + (h.verdict ? '<div class="sc-verdict"><span class="m-verdict ' + verdictClass(h.verdict) + '">' + esc(h.verdict.toUpperCase()) + '</span> <span class="m-dim">' + h.confidence + '% · CORTEX proposes ' + esc(h.proposal) + '</span></div>' : '<div class="m-dim">no alerts from this host</div>') +
    (h.alert ? '<div class="m-mono sc-alert">' + esc(h.alert.message) + '</div>' : "") +
    '<div class="sc-kv">' + rows.map(([k, v]) => '<span class="micro cy">' + k + '</span><span class="m-mono">' + esc(v) + '</span>').join("") + '</div>' +
    '<div class="sc-radial">' + [["lock", "LOCK ON", 1], ["investigate", "INVESTIGATE", !!h.alert], ["reach", "REACH", 1], ["trust", h.role === "trusted" ? "UNTRUST" : "TRUST", canQ], [h.quarantined ? "release" : "quarantine", h.quarantined ? "RELEASE" : "QUARANTINE", canQ]].map(([a, l, ok], k) => '<button class="sc-act' + (a === "quarantine" ? " danger" : "") + '" style="--k:' + k + '" data-a="' + a + '"' + (ok ? "" : " disabled") + '>' + l + '</button>').join("") + '</div>';
  $("#scDetail").querySelectorAll(".sc-act").forEach((b) => (b.onclick = () => scanAct(b.dataset.a, b)));
}
async function scanAct(action, btn) {
  const h = scan.hosts[scan.sel]; if (!h) return null;
  if (replay && action !== "lock" && action !== "reach") { status("go live first"); return null; }
  if (action === "quarantine" && scan.armed !== h.ip) { scan.armed = h.ip; if (btn) btn.textContent = "CONFIRM?"; return "confirm"; }
  let msg = "";
  if (action === "lock") { scanClose(); selectDeck("netwatch"); selectDevice(h.ip); msg = "locked on"; }
  else if (action === "reach") { scanClose(); oracleSt.host = h.ip; selectDeck("oracle"); msg = "reach"; }
  else if (action === "investigate") { scanClose(); selectDeck("cortex"); await cortexLoad(); const a = cortex.queue.find((x) => x.ts === h.alert.ts && x.source === h.ip); if (a) await cortexOpen(a); msg = "investigating"; }
  else if (action === "trust") { const r = await post("/api/trust", { ip: h.ip, trusted: h.role !== "trusted" }); msg = r && r.ok ? "trust changed" : "refused"; }
  else { const r = await post("/api/blackwall", { ip: h.ip, action }); msg = r && r.ok ? (r.note || (action === "quarantine" ? "BLACKWALL engaged — " + h.ip + " quarantined" : h.ip + " released")) : "failed: " + ((r && r.error) || "backend unreachable"); status(msg); }
  if (scan.open) { const keep = scan.sel; scan.open = false; await scanOpen(true); scanFocus(keep); }
  return msg;
}
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && scan.open) { scanClose(); return; }
  if (e.key === "Escape" && !$("#recapBox").hidden) { recapClose(); return; }
  if ((e.key === "q" || e.key === "Q") && !e.repeat && !e.ctrlKey && !e.metaKey && !/INPUT|TEXTAREA/.test(document.activeElement.tagName)) scanOpen(false);
});
document.addEventListener("keyup", (e) => { if ((e.key === "q" || e.key === "Q") && scan.open && !scan.pinned) scanClose(); });

// ---------- commands ----------
EXTRA_COMMANDS.push(async (t) => {
  let m;
  if ((m = t.match(/^(?:start |run )?(?:rehearsal|rehearse)(?: (ghost|mirror))?$/))) { selectDeck("dojo"); const r = await rehearse(m[1] || "ghost"); return r && r.ok ? "Rehearsal started: " + r.state.scenario.name + ". Everything it raises is synthetic." : "Could not start: " + ((r && r.error) || "go live first") + "."; }
  if (/^(stop|end) (the )?rehearsal$/.test(t)) { const r = await post("/api/rehearsal", { stop: true }); dojoLoad(); return r && r.state ? "Rehearsal stopped at " + r.state.score + " of 100." : "No rehearsal is running."; }
  if ((m = t.match(/^(?:while i was (?:out|away)|what did i miss|recap)(?: (\d+) ?min(?:utes)?)?$/))) { const r = await recapNow(m[1] ? +m[1] : 30); return r ? r.headline : "I could not build a recap."; }
  if ((m = t.match(/^(?:narrate|commentary) (on|off)$/))) return setNarrate(m[1] === "on");
  if ((m = t.match(/^(?:sonify|sonification|soundscape) (on|off)$/))) return setSonify(m[1] === "on");
  if (/^(scanner|scan mode|scanner mode)$/.test(t)) { scanOpen(true); return "Scanner up. Escape closes it."; }
  if ((m = t.match(/^operator watch (on|off)$/))) { const r = await setOperator(m[1] === "on"); return r && r.on ? "Operator watch on. Sharpness " + r.reading.sharpness + " of 100." : "Operator watch off."; }
  if (/^(how am i|how sharp am i|am i tired)$/.test(t)) { const r = await operatorLoad(); return r && r.reading ? "Sharpness " + r.reading.sharpness + " of 100 — " + r.reading.level + ". " + r.reading.advice : "Operator watch is off. Say operator watch on."; }
  if (/^(ack|acknowledge|dismiss)( all| everything)$/.test(t)) { const r = await post("/api/ack", { all: true }); return r ? "Acknowledged " + r.acknowledged + " alerts." : "I could not do that."; }
  if (/^(ack|acknowledge)$/.test(t)) { const r = await post("/api/ack", { newest: true }); return r && r.acknowledged ? "Acknowledged the newest alert." : "Nothing to acknowledge."; }
  return null;
});

Object.assign(window.netwatch, { nd, netdiveLoad, netdivePick, rehearse, dojo, operator, setOperator, recapNow, recapShow, recapClose, narr, narrLine, setNarrate, sonify, sonifyParams, setSonify, scan, scanOpen, scanClose, scanFocus, scanAct });
try { if (localStorage.getItem("nw.narrate") === "on") { narr.on = true; $("#narrBtn").textContent = "NARRATE ON"; $("#narrBtn").classList.add("on"); } } catch (_) {}
operatorLoad();
