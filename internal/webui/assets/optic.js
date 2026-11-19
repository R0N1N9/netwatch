"use strict";
// BRAINDANCE deck: replay what a paired phone was facing ("phone views") and
// what SONAR sensed ("room clips") on a tilted model of the room you can turn.
// The drawing is dance.js, shared with the phone's BD tab. Loaded after reflex.js.
const bz = { cam: danceCam(), kind: null, id: null, name: "", clip: null, room: null, frames: [], times: [], at: 0, playing: false, live: false, speed: 1, last: 0, phones: [], clips: [], sig: "", loop: false, picked: false };
const bzSpan = () => (bz.times.length ? [bz.times[0], bz.times[bz.times.length - 1]] : [0, 1]);

// ---- what to draw for the moment being played ----
function bzScene() {
  if (bz.kind === "clip") return danceClipScene(bz.clip, danceIndex(bz.times, bz.at));
  const v = bz.room || {};
  if (bz.kind !== "phone") return { layout: v.layout || {}, walls: v.walls || [], nodes: v.nodes || [], hosts: v.hosts || [], est: v.est && v.est.ok ? v.est : null, people: v.people || 0 };
  const i = danceIndex(bz.times, bz.at), f = bz.frames[i];
  // devices and nodes are drawn where they are now; the phone's position, heading, what was in view and where movement was are from then
  return { layout: v.layout || {}, walls: v.walls || [], nodes: (v.nodes || []).map((n) => ({ node: n.node, state: "clear", score: 0 })), hosts: (v.hosts || []).map((h) => ({ name: h.name || h.ip, x: h.x, y: h.y, hot: h.mac === f.g })), est: f.e ? { x: f.e[0], y: f.e[1] } : null,
    phone: { x: f.x, y: f.y, heading: f.h, fov: v.fov || 62, seen: f.s || [], path: bz.frames.slice(Math.max(0, i - 160), i + 1), label: bz.name.toUpperCase() }, frame: f };
}
// The first-person view: the things that were in front of the phone, placed where its X-RAY would have drawn them.
function bzOptic(c, f, fov) {
  const [W, H] = danceFit(c), g = c.getContext("2d"), heights = { ap: 1.8, node: 1.0, host: 0.8, presence: 1.0 }, colors = { ap: DANCE_C.soft, node: DANCE_C.green, host: DANCE_C.cyan, presence: DANCE_C.red };
  g.clearRect(0, 0, W, H); g.fillStyle = "rgba(3,9,16,0.92)"; g.fillRect(0, 0, W, H);
  const hy = xrayProject({ x: f.x, y: f.y - 5 }, f, 0, f.p, fov, W, H, 1.5).y; // the horizon, moved by the phone's tilt
  g.strokeStyle = "rgba(255,84,73,0.35)"; g.beginPath(); g.moveTo(0, hy); g.lineTo(W, hy); g.stroke();
  g.font = "600 10px Rajdhani, sans-serif"; g.textAlign = "center";
  for (let d = -180; d < 180; d += 15) { // a heading tape along the top
    const rel = ((d - f.h + 540) % 360) - 180; if (Math.abs(rel) > fov / 2) continue;
    const x = W / 2 + (Math.tan((rel * Math.PI) / 180) / Math.tan(((fov / 2) * Math.PI) / 180)) * (W / 2), deg = (d + 360) % 360;
    g.fillStyle = "rgba(189,249,255,0.5)"; g.fillRect(x, 0, 1, deg % 90 === 0 ? 9 : 5); g.fillText(deg + "°", x, 19);
  }
  (f.s || []).slice().reverse().forEach((s) => {
    const p = xrayProject(s, f, f.h, f.p, fov, W, H, heights[s.k] || 1), col = colors[s.k] || DANCE_C.cyan, r = Math.max(7, Math.min(20, 46 / Math.max(1, p.dist)));
    g.strokeStyle = col; g.lineWidth = 1.5; g.shadowColor = col; g.shadowBlur = 8;
    [[-1, -1], [1, -1], [1, 1], [-1, 1]].forEach(([sx, sy]) => { g.beginPath(); g.moveTo(p.x + sx * r, p.y + sy * r * 0.55); g.lineTo(p.x + sx * r, p.y + sy * r); g.lineTo(p.x + sx * r * 0.55, p.y + sy * r); g.stroke(); });
    g.shadowBlur = 0; g.fillStyle = col; g.font = "600 12px Rajdhani, sans-serif"; g.fillText(String(s.l), p.x, p.y - r - 5);
    g.fillStyle = "rgba(230,240,255,0.8)"; g.font = "10px ui-monospace, monospace"; g.fillText(s.d.toFixed(1) + " m", p.x, p.y + r + 12);
  });
  g.strokeStyle = "rgba(29,255,161,0.7)"; g.lineWidth = 1; g.beginPath(); g.moveTo(W / 2 - 8, H / 2); g.lineTo(W / 2 + 8, H / 2); g.moveTo(W / 2, H / 2 - 8); g.lineTo(W / 2, H / 2 + 8); g.stroke();
  if (!(f.s || []).length) { g.fillStyle = "rgba(189,249,255,0.45)"; g.font = "600 12px Rajdhani, sans-serif"; g.fillText("NOTHING KNOWN IN VIEW", W / 2, H / 2 + 28); }
}
function bzInfo(sc) {
  const [t0, t1] = bzSpan(), clock = (ms) => new Date(ms).toLocaleTimeString("en-GB");
  if (bz.kind === "clip") {
    const hot = sc.nodes.filter((n) => n.state === "motion").map((n) => n.node.toUpperCase() + " " + Math.round(n.score));
    const still = sc.nodes.filter((n) => n.state !== "motion" && n.breath > 0).length, count = sc.people ? " · ≈ " + sc.people + (sc.people === 1 ? " person" : " people") : "";
    return { time: clock(bz.at) + " · " + mmss(bz.at - t0) + " / " + mmss(t1 - t0), line: (hot.length ? "MOVEMENT · " + hot.join(" · ") : still ? "someone keeping still" : "nobody sensed") + count, hot: hot.length > 0 };
  }
  if (bz.kind === "phone") {
    const f = sc.frame, tabs = { find: "FIND", xray: "X-RAY", dance: "BD", alerts: "ALERTS" };
    let line = "tab " + (tabs[f.m] || f.m) + " · heading " + Math.round(f.h) + "°";
    const tgt = f.g && (bz.room.hosts || []).find((h) => h.mac === f.g);
    if (tgt) { const r = xrayTurn(f, tgt, f.h); line = "FIND → " + (tgt.name || tgt.ip) + " · " + r.dist.toFixed(1) + " m · " + xrayHint(r.turn, r.dist) + " · heading " + Math.round(f.h) + "°"; }
    line += (f.s || []).length ? " · IN VIEW: " + f.s.map((s) => s.l + " " + s.d.toFixed(1) + " m").join(", ") : " · nothing known in view";
    if (f.e) line += " · SOMEONE MOVING " + xrayBearing(f, { x: f.e[0], y: f.e[1] }).dist.toFixed(1) + " m from the phone";
    return { time: clock(bz.at) + " · " + mmss(bz.at - t0) + " / " + mmss(t1 - t0), line, hot: !!f.e };
  }
  const moving = (sc.nodes || []).filter((n) => n.state === "motion").map((n) => n.node.toUpperCase() + " " + Math.round(n.score)), stillNow = (sc.nodes || []).filter((n) => n.state !== "motion" && n.breath > 0).length;
  const who = moving.length ? "MOVEMENT NOW · " + moving.join(" · ") : stillNow ? "someone keeping still" : (sc.nodes || []).length ? "nobody sensed right now" : "no sensor nodes are reporting";
  return { time: "LIVE", line: who + (sc.people ? " · ≈ " + sc.people + (sc.people === 1 ? " person" : " people") : "") + " — pick a phone view or a room clip to replay it. Drag to turn, scroll to zoom.", hot: moving.length > 0 };
}
function bzDraw() {
  const sc = bzScene(); danceDraw($("#bzStage"), sc, bz.cam, performance.now());
  const phone = bz.kind === "phone"; $("#bzOpticBox").hidden = !phone;
  if (phone) bzOptic($("#bzOptic"), sc.frame, sc.phone.fov);
  const info = bzInfo(sc), [t0, t1] = bzSpan();
  $("#bzTime").textContent = info.time; $("#bzInfo").textContent = info.line; $("#bzInfo").className = "bz-info" + (info.hot ? " hot" : "");
  if (bz.kind) $("#bzSeek").value = Math.round(((bz.at - t0) / Math.max(1, t1 - t0)) * 1000);
}
function bzFrame(now) {
  if (currentDeck() !== "braindance" || document.hidden) { bz.loop = false; return; }
  const [, t1] = bzSpan();
  if (bz.kind && bz.live) bz.at = t1;
  else if (bz.kind && bz.playing) { bz.at += (now - bz.last) * bz.speed; if (bz.at >= t1) { bz.at = t1; bz.playing = false; bzButtons(); } }
  bz.last = now; bzDraw(); requestAnimationFrame(bzFrame);
}
function bzButtons() {
  $("#bzPlay").textContent = bz.playing ? "❚❚" : "▶"; $("#bzLive").classList.toggle("on", bz.live);
  $("#bzLive").hidden = bz.kind !== "phone"; $("#bzSonar").hidden = bz.kind !== "clip"; $("#bzBar").classList.toggle("idle", !bz.kind);
  $("#bzTitle").textContent = bz.kind === "phone" ? "PHONE VIEW · " + bz.name : bz.kind === "clip" ? "ROOM CLIP · " + bz.name : "LIVE ROOM";
}

// ---- the lists ----
function bzLists() {
  const sig = JSON.stringify([bz.phones, bz.clips.map((c) => c.id), bz.kind, bz.id]); if (sig === bz.sig) return; bz.sig = sig;
  const ph = $("#bzPhones"); ph.innerHTML = "";
  if (!bz.phones.length) ph.appendChild(el("div", "sonar-help", "No phone is sharing its view. On the phone, open the BD tab and switch on SHARE MY VIEW."));
  bz.phones.forEach((p) => {
    const row = el("div", "bdclip" + (bz.kind === "phone" && bz.id === p.phone ? " playing" : "")), info = el("div");
    info.appendChild(el("div", "nm", p.name === p.phone ? p.phone : p.name + " · " + p.phone));
    info.appendChild(el("div", "sub", (p.live ? "● SHARING NOW · " : "last seen " + new Date(p.end).toLocaleTimeString("en-GB") + " · ") + mmss(p.end - p.start) + " kept"));
    if (p.live) row.classList.add("live");
    const play = el("button", null, "▶"), del = el("button", "del", "✕");
    play.title = "replay what this phone was facing"; del.title = "forget this phone's view";
    play.onclick = () => bzOpenPhone(p.phone);
    del.onclick = async () => { if (bz.kind === "phone" && bz.id === p.phone) bzClose(); await post("/api/optic", { forget: p.phone }); bzLoad(); };
    row.appendChild(info); row.appendChild(play); row.appendChild(del); ph.appendChild(row);
  });
  const cl = $("#bzClips"); cl.innerHTML = "";
  if (!bz.clips.length) cl.appendChild(el("div", "sonar-help", "No room clips yet. SONAR saves one when it is armed and something moves, when a new device joins, or when you press SAVE LAST 60s in the SONAR deck."));
  bz.clips.forEach((c) => {
    const row = el("div", "bdclip" + (bz.kind === "clip" && bz.id === c.id ? " playing" : "")), info = el("div");
    info.appendChild(el("div", "nm", c.name));
    info.appendChild(el("div", "sub", new Date(c.start).toLocaleString("en-GB") + " · " + mmss(c.end - c.start) + " · peak " + Math.round(c.peak) + (c.auto ? " · auto" : "")));
    const play = el("button", null, "▶"); play.title = "replay this clip here"; play.onclick = () => bzOpenClip(c.id);
    row.appendChild(info); row.appendChild(play); cl.appendChild(row);
  });
  $("#bzMeta").textContent = bz.phones.length + " phone view" + (bz.phones.length === 1 ? "" : "s") + " · " + bz.clips.length + " room clip" + (bz.clips.length === 1 ? "" : "s");
}
async function bzLoad() {
  const [o, c] = await Promise.all([get("/api/optic"), get("/api/braindance")]);
  if (o) bz.phones = o.phones || []; if (c) bz.clips = c.clips || [];
  if (bz.kind === "phone") await bzMore(); else if (!bz.kind) { const v = await get("/api/sonar"); if (v) bz.room = v; }
  if (!bz.picked && !bz.kind && bz.phones.length && bz.phones[0].live) { bz.picked = true; await bzOpenPhone(bz.phones[0].phone); } // a phone is sharing right now: show it
  bzLists(); bzButtons();
  if (!bz.loop) { bz.loop = true; bz.last = performance.now(); requestAnimationFrame(bzFrame); }
  return bz;
}
// New frames for the open phone view (and the room as it is now).
async function bzMore() {
  const r = await get("/api/optic/trail?phone=" + encodeURIComponent(bz.id) + "&since=" + (bz.times.length ? bz.times[bz.times.length - 1] : 0)); if (!r) return;
  bz.room = r; (r.frames || []).forEach((f) => { bz.frames.push(f); bz.times.push(f.t); });
  if (bz.frames.length > 2400) { const cut = bz.frames.length - 2400; bz.frames.splice(0, cut); bz.times.splice(0, cut); bz.at = Math.max(bz.at, bz.times[0]); }
}
async function bzOpenPhone(phone) {
  const r = await get("/api/optic/trail?phone=" + encodeURIComponent(phone));
  if (!r || !r.frames || !r.frames.length) { status("that phone has not shared its view"); return false; }
  const p = bz.phones.find((x) => x.phone === phone) || { name: phone, live: false };
  Object.assign(bz, { kind: "phone", id: phone, name: p.name || phone, room: r, frames: r.frames, times: r.frames.map((f) => f.t), live: !!p.live, playing: !p.live, speed: 1, last: performance.now(), picked: true });
  bz.at = p.live ? bz.times[bz.times.length - 1] : Math.max(bz.times[0], bz.times[bz.times.length - 1] - 60000); // a finished view starts a minute before its end
  $("#bzSpeed").textContent = "1×"; bzLists(); bzButtons(); return true;
}
async function bzOpenClip(id) {
  const c = await get("/api/braindance/scene?id=" + encodeURIComponent(id));
  if (!c || !c.samples || c.samples.length < 2) { status("that clip could not be loaded"); return false; }
  Object.assign(bz, { kind: "clip", id, name: c.name, clip: c, times: c.samples.map((s) => s.t), live: false, playing: true, speed: 1, last: performance.now(), picked: true }); bz.at = bz.times[0];
  $("#bzSpeed").textContent = "1×"; bzLists(); bzButtons(); return true;
}
function bzClose() { Object.assign(bz, { kind: null, id: null, name: "", clip: null, frames: [], times: [], live: false, playing: false }); bzLists(); bzButtons(); bzLoad(); }

$("#bzPlay").onclick = () => { if (!bz.kind) return; const [t0, t1] = bzSpan(); if (bz.at >= t1) bz.at = t0; bz.live = false; bz.playing = !bz.playing; bz.last = performance.now(); bzButtons(); };
$("#bzSeek").oninput = (e) => { if (!bz.kind) return; const [t0, t1] = bzSpan(); bz.live = false; bz.at = t0 + (+e.target.value / 1000) * (t1 - t0); bzButtons(); };
$("#bzSpeed").onclick = () => { bz.speed = bz.speed === 1 ? 2 : bz.speed === 2 ? 4 : bz.speed === 4 ? 0.5 : 1; $("#bzSpeed").textContent = bz.speed + "×"; };
$("#bzLive").onclick = () => { bz.live = !bz.live; bz.playing = false; bzButtons(); };
$("#bzSonar").onclick = () => { if (bz.kind === "clip") bdOpen(bz.id); };
$("#bzClose").onclick = bzClose;
danceOrbit($("#bzStage"), bz.cam, () => { if (!bz.loop) bzDraw(); });
setInterval(async () => { // a live phone view and the live room keep up between the slower list refreshes
  if (currentDeck() !== "braindance" || document.hidden) return;
  if (bz.kind === "phone" && bz.live) bzMore(); else if (!bz.kind) { const v = await get("/api/sonar"); if (v && !bz.kind) bz.room = v; }
}, 700);

MOD_DECKS.braindance = bzLoad;
Object.assign(DECK_WORDS, { braindance: "braindance", "brain dance": "braindance", bd: "braindance", recordings: "braindance", "phone views": "braindance", "phone view": "braindance" });
EXTRA_COMMANDS.push(async (t) => {
  if (/^(braindance|brain dance|recordings|phone views?)$/.test(t)) { selectDeck("braindance"); await bzLoad(); return bz.phones.length + " phone view" + (bz.phones.length === 1 ? "" : "s") + " and " + bz.clips.length + " room clip" + (bz.clips.length === 1 ? "" : "s") + " to replay."; }
  if (/^(replay|play|show)( the| my)? phone( view)?$|^what did (the|my) phone see$/.test(t)) { selectDeck("braindance"); await bzLoad(); if (!bz.phones.length) return "No phone has shared its view. Switch on SHARE MY VIEW in the phone's BD tab."; return (await bzOpenPhone(bz.phones[0].phone)) ? "Replaying what " + bz.name + " was facing." : "That view could not be loaded."; }
  if (/^forget (the |all )?phone views?$/.test(t)) { await post("/api/optic", { forget: "*" }); if (bz.kind === "phone") bzClose(); else bzLoad(); return "Every phone view is forgotten."; }
  return null;
});
Object.assign(window.netwatch, { bz, bzLoad, bzOpenPhone, bzOpenClip, bzClose, bzScene });
