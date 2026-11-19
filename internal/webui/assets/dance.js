"use strict";
// BRAINDANCE stage: the room drawn as a tilted model you can turn, on a plain
// 2D canvas. Shared by the console's BRAINDANCE deck and the phone's BD tab.
// No dependencies. Every label is drawn with fillText, so names that came off
// the network never become HTML.
const DANCE_M = 4.4; // map units to metres, the same scale SONAR uses
const DANCE_WALL = 2.4 / DANCE_M; // how tall a wall is drawn, in map units
const DANCE_C = { cyan: "#5ef6ff", soft: "#bdf9ff", green: "#1dffa1", red: "#ff5449", amber: "#ffb84a", dim: "rgba(189,249,255,0.45)" };

function danceCam() { return { yaw: -28, tilt: 52, zoom: 1 }; }

// Project a point on the map (x, y, and a height z, all in map units) to canvas
// pixels. tilt 90 looks straight down. The third value is depth: larger is nearer.
function danceProject(cam, W, H, x, y, z) {
  const a = (cam.yaw * Math.PI) / 180, t = (cam.tilt * Math.PI) / 180;
  const rx = x * Math.cos(a) - y * Math.sin(a), ry = x * Math.sin(a) + y * Math.cos(a);
  const s = (Math.min(W, H) / 3.3) * cam.zoom;
  return [W / 2 + rx * s, H * 0.56 + ry * s * Math.sin(t) - (z || 0) * s * Math.cos(t), ry];
}

// Size the canvas's pixels to its box. Returns the size in CSS pixels.
function danceFit(c) {
  const d = Math.min(2, window.devicePixelRatio || 1), W = c.clientWidth || 300, H = c.clientHeight || 300;
  if (c.width !== Math.round(W * d) || c.height !== Math.round(H * d)) { c.width = Math.round(W * d); c.height = Math.round(H * d); }
  c.getContext("2d").setTransform(d, 0, 0, d, 0, 0);
  return [W, H];
}

// The sample that was current at time t (times ascending).
function danceIndex(times, t) {
  let lo = 0, hi = times.length - 1;
  while (lo < hi) { const m = (lo + hi + 1) >> 1; if (times[m] <= t) lo = m; else hi = m - 1; }
  return lo;
}

// The scene for one instant of a clip served by /api/braindance/scene (or the phone's /dance/clip).
function danceClipScene(clip, i) {
  const smp = clip.samples[i] || { n: [] }, trail = [];
  for (let k = Math.max(0, i - 45); k <= i; k++) { const e = clip.est[k]; if (e) trail.push({ x: e[0], y: e[1] }); }
  const e = clip.est[i];
  return { layout: clip.layout || {}, walls: clip.walls || [], nodes: (smp.n || []).map((q) => ({ node: q.n, state: q.s, score: q.v, breath: q.b || 0 })), hosts: [], est: e ? { x: e[0], y: e[1] } : null, trail: e ? trail : [] };
}

// Draw a scene:
//   layout {name: {x, y}} ("@ap" is the access point), walls [{a, b}],
//   nodes [{node, state, score, breath}], hosts [{name, x, y, hot}],
//   est {x, y} or null, trail [{x, y}],
//   phone {x, y, heading, fov, seen: [{x, y}], path: [{x, y}], label}
// now is a clock in milliseconds for the pulsing parts.
function danceDraw(c, scene, cam, now) {
  const [W, H] = danceFit(c), g = c.getContext("2d"), C = DANCE_C;
  const P = (x, y, z) => danceProject(cam, W, H, x, y, z);
  const fs = Math.max(10, Math.min(15, W / 44)), pulse = 0.5 + 0.5 * Math.sin((now || 0) / 260);
  const line = (a, b, style, w) => { g.strokeStyle = style; g.lineWidth = w || 1; g.beginPath(); g.moveTo(a[0], a[1]); g.lineTo(b[0], b[1]); g.stroke(); };
  const poly = (pts, fill, stroke, w) => { g.beginPath(); pts.forEach((p, i) => (i ? g.lineTo(p[0], p[1]) : g.moveTo(p[0], p[1]))); g.closePath(); if (fill) { g.fillStyle = fill; g.fill(); } if (stroke) { g.strokeStyle = stroke; g.lineWidth = w || 1; g.stroke(); } };
  const ring = (x, y, r, fill, stroke, w) => poly(Array.from({ length: 28 }, (_, i) => P(x + r * Math.cos((i / 28) * 6.2832), y + r * Math.sin((i / 28) * 6.2832), 0)), fill, stroke, w);
  const label = (p, text, color, dy) => { g.font = "600 " + fs + "px Rajdhani, system-ui, sans-serif"; g.textAlign = "center"; g.fillStyle = "rgba(5,2,3,0.75)"; const w = g.measureText(text).width + 8; g.fillRect(p[0] - w / 2, p[1] + (dy || 0) - fs, w, fs + 4); g.fillStyle = color; g.fillText(text, p[0], p[1] + (dy || 0)); };
  g.clearRect(0, 0, W, H);

  // the floor
  const R = 1.3;
  for (let i = -5; i <= 5; i++) { const v = (i / 5) * R, edge = Math.abs(i) === 5; line(P(v, -R, 0), P(v, R, 0), edge ? "rgba(255,84,73,0.5)" : "rgba(255,84,73,0.13)"); line(P(-R, v, 0), P(R, v, 0), edge ? "rgba(255,84,73,0.5)" : "rgba(255,84,73,0.13)"); }

  const lay = scene.layout || {}, ap = lay["@ap"];
  // each link between the access point and a node: the zone a body disturbs
  (scene.nodes || []).forEach((n) => {
    const p = lay[n.node]; if (!p || !ap) return;
    const mx = (ap.x + p.x) / 2, my = (ap.y + p.y) / 2, half = Math.hypot(p.x - ap.x, p.y - ap.y) / 2, ang = Math.atan2(p.y - ap.y, p.x - ap.x), hot = n.state === "motion";
    const pts = Array.from({ length: 32 }, (_, i) => { const t = (i / 32) * 6.2832, ex = Math.cos(t) * half, ey = Math.sin(t) * 0.13; return P(mx + ex * Math.cos(ang) - ey * Math.sin(ang), my + ex * Math.sin(ang) + ey * Math.cos(ang), 0); });
    const k = Math.min(1, (n.score || 0) / 70);
    poly(pts, hot ? "rgba(255,84,73," + (0.1 + 0.28 * k * (0.6 + 0.4 * pulse)).toFixed(3) + ")" : "rgba(29,255,161,0.05)", hot ? "rgba(255,84,73,0.75)" : "rgba(29,255,161,0.28)");
  });
  // where the movement has been
  (scene.trail || []).forEach((t, i, all) => { const p = P(t.x, t.y, 0); g.fillStyle = "rgba(255,84,73," + (0.15 + 0.5 * (i / all.length)).toFixed(2) + ")"; g.fillRect(p[0] - 1.5, p[1] - 1.5, 3, 3); });

  // the phone: where it walked, what it was facing, and lines to what was in view
  const ph = scene.phone;
  if (ph) {
    const path = ph.path || [];
    g.strokeStyle = "rgba(29,255,161,0.55)"; g.lineWidth = 1.5; g.setLineDash([4, 4]); g.beginPath(); path.forEach((t, i) => { const p = P(t.x, t.y, 0); if (i) g.lineTo(p[0], p[1]); else g.moveTo(p[0], p[1]); }); g.stroke(); g.setLineDash([]);
    const half = ((ph.fov || 62) / 2) * (Math.PI / 180), h = (ph.heading * Math.PI) / 180, reach = 3.2 / DANCE_M, cone = [P(ph.x, ph.y, 0)];
    for (let i = 0; i <= 12; i++) { const a = h - half + (i / 12) * 2 * half; cone.push(P(ph.x + Math.sin(a) * reach, ph.y - Math.cos(a) * reach, 0)); }
    poly(cone, "rgba(29,255,161,0.16)", "rgba(29,255,161,0.7)");
    (ph.seen || []).forEach((s) => line(P(ph.x, ph.y, 0.3), P(s.x, s.y, 0.2), "rgba(29,255,161,0.8)", 1.2));
  }

  // everything that stands up, drawn back to front
  const tall = [];
  (scene.walls || []).forEach((w) => tall.push({ d: (P(w.a.x, w.a.y, 0)[2] + P(w.b.x, w.b.y, 0)[2]) / 2, draw: () => {
    const a0 = P(w.a.x, w.a.y, 0), b0 = P(w.b.x, w.b.y, 0), a1 = P(w.a.x, w.a.y, DANCE_WALL), b1 = P(w.b.x, w.b.y, DANCE_WALL);
    poly([a0, b0, b1, a1], "rgba(189,249,255,0.07)", "rgba(189,249,255,0.25)"); line(a1, b1, "rgba(189,249,255,0.8)", 1.5); line(a0, b0, "rgba(189,249,255,0.5)");
  } }));
  const pillar = (x, y, h, color, text, top) => tall.push({ d: P(x, y, 0)[2], draw: () => {
    const a = P(x, y, 0), b = P(x, y, h);
    line(a, b, color, 2); g.fillStyle = color; g.shadowColor = color; g.shadowBlur = 12;
    if (top === "diamond") poly([[b[0], b[1] - 7], [b[0] + 6, b[1]], [b[0], b[1] + 7], [b[0] - 6, b[1]]], color); else { g.beginPath(); g.arc(b[0], b[1], top || 4, 0, 6.2832); g.fill(); }
    g.shadowBlur = 0; if (text) label(b, text, color, -12);
  } });
  if (ap) pillar(ap.x, ap.y, 0.42, C.soft, "ACCESS POINT", "diamond");
  (scene.nodes || []).forEach((n) => {
    const p = lay[n.node]; if (!p) return;
    const hot = n.state === "motion", color = hot ? C.red : n.state === "offline" ? "rgba(189,249,255,0.35)" : C.green;
    if (hot) ring(p.x, p.y, 0.06 + 0.1 * pulse, null, "rgba(255,84,73,0.8)", 1.5);
    pillar(p.x, p.y, 0.23, color, String(n.node).toUpperCase() + (hot ? " · " + Math.round(n.score) : n.breath > 0 ? " · STILL " + Math.round(n.breath) + "/MIN" : ""), 4);
  });
  (scene.hosts || []).forEach((h) => pillar(h.x, h.y, 0.16, h.hot ? C.red : C.cyan, String(h.name || h.ip || ""), h.hot ? 5 : 3));
  if (scene.est) {
    const e = scene.est;
    ring(e.x, e.y, 0.1 + 0.07 * pulse, "rgba(255,84,73,0.18)", "rgba(255,84,73,0.9)", 1.5);
    tall.push({ d: P(e.x, e.y, 0)[2], draw: () => { // a figure: someone is moving here
      const feet = P(e.x, e.y, 0), hip = P(e.x, e.y, 0.2), neck = P(e.x, e.y, 0.34), head = P(e.x, e.y, 0.39);
      g.shadowColor = C.red; g.shadowBlur = 16; line(feet, neck, C.red, 5); g.fillStyle = C.red; g.beginPath(); g.arc(head[0], head[1], 5.5, 0, 6.2832); g.fill();
      line([hip[0] - 7, (hip[1] + neck[1]) / 2], [hip[0] + 7, (hip[1] + neck[1]) / 2], C.red, 2); g.shadowBlur = 0; label(head, "PRESENCE", C.red, -12);
    } });
  }
  if (ph) pillar(ph.x, ph.y, 0.3, "#ffffff", ph.label || "PHONE", 5);
  tall.sort((a, b) => a.d - b.d).forEach((t) => t.draw());
}

// Drag to turn the room, drag up and down to tilt it, wheel or pinch-free zoom.
function danceOrbit(c, cam, redraw) {
  let last = null;
  c.style.touchAction = "none";
  c.addEventListener("pointerdown", (e) => { last = [e.clientX, e.clientY]; try { c.setPointerCapture(e.pointerId); } catch (_) {} });
  c.addEventListener("pointermove", (e) => {
    if (!last) return;
    cam.yaw = (cam.yaw + (e.clientX - last[0]) * 0.5) % 360; cam.tilt = Math.max(24, Math.min(90, cam.tilt + (e.clientY - last[1]) * 0.3));
    last = [e.clientX, e.clientY]; redraw();
  });
  const up = () => (last = null);
  c.addEventListener("pointerup", up); c.addEventListener("pointercancel", up);
  c.addEventListener("wheel", (e) => { e.preventDefault(); cam.zoom = Math.max(0.6, Math.min(2.4, cam.zoom * (e.deltaY < 0 ? 1.1 : 0.9))); redraw(); }, { passive: false });
}
