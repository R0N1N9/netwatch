"use strict";
// X-RAY: draw what SONAR senses where it is, as seen from a point in the room.
// Shared by the console's AR deck and the phone companion. No dependencies.
const XRAY_M = 4.4; // map units to metres, the same scale SONAR uses

// Project a point on the room map into a camera view.
//   pt, me: {x, y} in map units (y grows towards the bottom of the map)
//   heading: degrees clockwise from "up the map"; pitch: degrees, up is positive
//   fov: horizontal field of view in degrees; W, H: the view in pixels
// Returns {x, y, dist (metres), rel (degrees off centre), visible}.
function xrayProject(pt, me, heading, pitch, fov, W, H, height) {
  const dx = (pt.x - me.x) * XRAY_M, dy = (pt.y - me.y) * XRAY_M, dist = Math.hypot(dx, dy);
  let rel = (Math.atan2(dx, -dy) * 180) / Math.PI - heading;
  rel = ((((rel + 180) % 360) + 360) % 360) - 180;
  const half = fov / 2, rad = Math.PI / 180;
  const visible = Math.abs(rel) < half && dist > 0.05;
  const x = W / 2 + (Math.tan(Math.max(-89, Math.min(89, rel)) * rad) / Math.tan(half * rad)) * (W / 2);
  const vhalf = (Math.atan(Math.tan(half * rad) * (H / W)) * 180) / Math.PI;
  const elev = (Math.atan2((height == null ? 1.0 : height) - 1.5, Math.max(0.3, dist)) * 180) / Math.PI - (pitch || 0); // eye level is 1.5 m
  const y = H / 2 - (Math.tan(Math.max(-80, Math.min(80, elev)) * rad) / Math.tan(vhalf * rad)) * (H / 2);
  return { x, y, dist, rel, visible };
}

// Everything worth drawing, from the server's SONAR view.
function xrayItems(v) {
  const lay = v.layout || {}, items = [], ap = lay["@ap"];
  if (ap) items.push({ kind: "ap", label: "ACCESS POINT", x: ap.x, y: ap.y, h: 1.8 });
  (v.nodes || []).forEach((n) => {
    const p = lay[n.node]; if (!p) return;
    items.push({ kind: n.state === "motion" ? "node hot" : "node", label: n.node.toUpperCase() + (n.state === "motion" ? " · MOTION " + Math.round(n.score) : ""), x: p.x, y: p.y, h: 1.0 });
    if (ap && n.breath > 0 && n.state !== "motion") items.push({ kind: "still", label: "STILL · " + Math.round(n.breath) + "/MIN", x: (ap.x + p.x) / 2, y: (ap.y + p.y) / 2, h: 1.0 });
  });
  (v.hosts || []).forEach((h) => items.push({ kind: "host", label: String(h.name || h.ip), x: h.x, y: h.y, h: 0.8 }));
  if (v.est && v.est.ok) items.push({ kind: "presence", label: "PRESENCE", x: v.est.x, y: v.est.y, h: 1.0 });
  return items;
}

// Draw the items into a container as positioned tags. Returns how many are in view.
function xrayDraw(box, view, pose, fov) {
  const W = box.clientWidth, H = box.clientHeight, items = xrayItems(view);
  let shown = 0;
  const html = items.map((it) => {
    const p = xrayProject(it, pose, pose.heading, pose.pitch, fov, W, H, it.h), label = String(it.label).replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
    if (!p.visible) {
      if (it.kind !== "presence") return "";
      return '<div class="xr-edge ' + (p.rel < 0 ? "left" : "right") + '">' + (p.rel < 0 ? "◀ " : "") + label + " · " + p.dist.toFixed(1) + " m" + (p.rel < 0 ? "" : " ▶") + "</div>";
    }
    shown++;
    const scale = Math.max(0.55, Math.min(1.6, 3 / Math.max(1, p.dist)));
    return '<div class="xr-tag ' + it.kind + '" style="left:' + p.x.toFixed(0) + "px;top:" + p.y.toFixed(0) + "px;--s:" + scale.toFixed(2) + '"><i></i><b>' + label + "</b><span>" + p.dist.toFixed(1) + " m</span></div>";
  }).join("");
  box.innerHTML = html;
  return shown;
}

// Compass heading from a deviceorientation event, or null if the device has none.
function xrayHeading(e) {
  if (typeof e.webkitCompassHeading === "number") return e.webkitCompassHeading;
  if (e.absolute && typeof e.alpha === "number") return (360 - e.alpha) % 360;
  return null;
}
