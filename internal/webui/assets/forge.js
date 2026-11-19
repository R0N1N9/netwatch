"use strict";
// NODE FORGE: flash a plugged-in ESP32-S3 from the console. Loaded after reflex.js.
const forgeSt = { open: false, timer: null, data: null };
function forgeRender(r) {
  forgeSt.data = r;
  const img = r.images || {}, st = [];
  st.push('<span class="m-verdict ' + (r.tool ? "ok" : "bad") + '">' + (r.tool ? "FLASHER: " + esc(r.tool).toUpperCase() : "ESPTOOL IS NOT INSTALLED") + '</span>');
  st.push('<span class="m-verdict ' + (img.node && img.node.have ? "ok" : "bad") + '">' + (img.node && img.node.have ? "NODE FIRMWARE READY" : "NO NODE FIRMWARE IN THIS BUILD") + '</span>');
  st.push('<span class="m-verdict ' + (r.listening ? "ok" : "dim") + '">' + (r.listening ? "LISTENING ON " + esc(r.listening) : r.demo ? "DEMO: SIMULATED NODES" : "NOT LISTENING YET") + '</span>');
  if (!r.listening && !r.demo) st.push('<button class="hs-btn" id="fgListen">LISTEN FOR NODES NOW</button>');
  $("#fgStatus").innerHTML = st.join("") + (r.tool ? "" : '<div class="m-dim">Install it once with <code>pipx install esptool</code> (or <code>sudo apt install esptool</code>), then reopen this panel.</div>');
  if ($("#fgListen")) $("#fgListen").onclick = async () => { await post("/api/forge", { listen: true }); forgeLoad(); };
  if (document.activeElement !== $("#fgSsid") && !$("#fgSsid").value && r.ssid) $("#fgSsid").value = r.ssid;
  $("#fgPass").placeholder = r.havePass ? "remembered until NetWatch stops" : "";
  const busy = r.job && !r.job.done, box = $("#fgBoards");
  box.innerHTML = r.boards.length ? r.boards.map((b, i) =>
    '<div class="m-row' + (b.native ? " warn" : "") + '"><div class="m-row-top"><b>' + esc(b.port) + '</b><span class="m-dim">' + esc(b.chip) + '</span><span class="spacer"></span>' +
    '<button class="hs-btn" data-k="node" data-i="' + i + '"' + (busy ? " disabled" : "") + '>MAKE IT A SENSOR NODE</button><button class="hs-btn" data-k="light" data-i="' + i + '"' + (busy ? " disabled" : "") + '>THREAT LIGHT</button><button class="hs-btn" data-k="node" data-s="1" data-i="' + i + '"' + (busy ? " disabled" : "") + ' title="keep the firmware, rewrite only the Wi-Fi details">SETTINGS ONLY</button></div>' +
    (b.native ? '<div class="m-dim">This is the board\'s native USB port. It usually works, but if flashing cannot connect, move the cable to the port marked UART or COM.</div>' : "") + '</div>').join("")
    : '<div class="m-dim">No board found. Plug one in by its UART port; if it still does not appear, try another cable (many USB-C cables carry power only).</div>';
  box.querySelectorAll("button").forEach((b) => (b.onclick = () => forgeFlash(r.boards[+b.dataset.i].port, b.dataset.k, !!b.dataset.s)));
  forgeProgress(r.job);
  const log = $("#fgLog");
  if (r.job) { log.hidden = false; log.textContent = r.job.lines.join("\n") + (r.job.done ? "\n" + (r.job.ok ? "✓ finished" : "✕ " + r.job.error) : "\n…"); log.className = "fg-log" + (r.job.done ? (r.job.ok ? " ok" : " bad") : ""); log.scrollTop = log.scrollHeight; }
}
// The steps of a flash, what each looks like when it is the current one, and the big result line.
const FG_STEPS = [["connect", "CONNECT"], ["write", "WRITE"], ["verify", "VERIFY"], ["restart", "RESTART"], ["online", "ONLINE"]];
function forgeState(j) {
  if (!j) return null;
  if (j.done && !j.ok) return { at: Math.max(0, FG_STEPS.findIndex((x) => x[0] === j.stage)), cls: "bad", text: "✕ FLASHING FAILED", sub: j.error + " — see the log below." };
  if (!j.done) { const at = Math.max(0, FG_STEPS.findIndex((x) => x[0] === j.stage)); return { at, cls: "run", text: j.stage === "write" ? "WRITING " + j.progress + "%" : j.stage === "verify" ? "VERIFYING…" : j.stage === "restart" ? "RESTARTING…" : "CONNECTING TO THE BOARD…", sub: "Leave the cable in. This takes about twenty seconds." }; }
  if (j.kind !== "node") return { at: 4, cls: "ok", text: "✓ FLASHED — THE LIGHT IS READY", sub: "Power it up; it shows faint white until NetWatch starts sending it colours." };
  if (j.online) return { at: 5, cls: "ok", text: "✓ " + j.online.toUpperCase() + " IS ONLINE", sub: "It is reporting to SONAR. Unplug it and power it wherever you want it — or plug in the next board." };
  if (j.stalled) return { at: 4, cls: "warn", text: "FLASHED, BUT IT HAS NOT REPORTED IN", sub: "After a minute there is still nothing from it. Check the Wi-Fi name and password (2.4 GHz only), then press SETTINGS ONLY to rewrite them." };
  return { at: 4, cls: "wait", text: "✓ FLASHED — JOINING YOUR WI-FI… " + j.waitSec + " s", sub: "Leave it plugged in. It should report within about ten seconds." };
}
function forgeProgress(j) {
  const st = forgeState(j), box = $("#fgRun");
  box.hidden = !st; if (!st) return;
  const steps = j.kind === "node" ? FG_STEPS : FG_STEPS.slice(0, 4);
  $("#fgSteps").innerHTML = steps.map(([, label], i) => '<span class="fg-step ' + (i < st.at ? "done" : i === st.at && st.cls !== "ok" ? "now " + st.cls : "") + '">' + (i < st.at ? "✓ " : "") + label + '</span>').join('<i></i>');
  $("#fgBar").style.width = (j.done && j.ok ? 100 : j.progress) + "%";
  box.className = "fg-run " + st.cls;
  $("#fgBanner").innerHTML = "<b>" + esc(st.text) + "</b><span>" + esc(st.sub) + "</span>";
  // one chime when the outcome is known: up for online, down for a failure
  const key = j.port + "|" + j.lines.length + "|" + st.cls + "|" + (j.online || "");
  if ((st.cls === "ok" || st.cls === "bad" || st.cls === "warn") && forgeSt.rang !== (j.port + st.cls + (j.online || ""))) {
    forgeSt.rang = j.port + st.cls + (j.online || "");
    if (forgeSt.armed) { forgeChime(st.cls === "ok"); if (typeof narr !== "undefined" && narr.on) narrSay(st.cls === "ok" ? (j.online ? "Node online." : "Flash complete.") : "The flash needs attention."); }
  }
  forgeSt.key = key;
}
function forgeChime(good) {
  if (!actx) return;
  const t = actx.currentTime;
  (good ? [523, 659, 784, 1047] : [392, 294]).forEach((f, i) => {
    const o = actx.createOscillator(), g = actx.createGain(); o.type = good ? "triangle" : "sawtooth"; o.frequency.value = f; o.connect(g).connect(actx.destination);
    g.gain.setValueAtTime(0.0001, t + i * 0.12); g.gain.exponentialRampToValueAtTime(0.12, t + i * 0.12 + 0.02); g.gain.exponentialRampToValueAtTime(0.0001, t + i * 0.12 + 0.3);
    o.start(t + i * 0.12); o.stop(t + i * 0.12 + 0.32);
  });
}
async function forgeLoad() { const r = await get("/api/forge"); if (r && forgeSt.open) forgeRender(r); return r; }
async function forgeFlash(port, kind, settingsOnly) {
  forgeSt.armed = true; forgeSt.rang = null; // this click is what may make a sound later
  if (!actx) { const AC = window.AudioContext || window.webkitAudioContext; if (AC) actx = new AC(); }
  if (actx && actx.state === "suspended") actx.resume();
  const r = await post("/api/forge", { flash: port, kind, ssid: $("#fgSsid").value, pass: $("#fgPass").value, name: $("#fgName").value, settingsOnly });
  if (r && r.ok) { $("#fgPass").value = ""; $("#fgName").value = ""; }
  await forgeLoad();
  return r;
}
function forgeOpen() { forgeSt.open = true; $("#forgeBox").hidden = false; clearInterval(forgeSt.timer); forgeSt.timer = setInterval(forgeLoad, 700); forgeLoad(); }
function forgeClose() { forgeSt.open = false; $("#forgeBox").hidden = true; clearInterval(forgeSt.timer); }
$("#forgeOpen").onclick = forgeOpen;
$("#forgeClose").onclick = forgeClose;
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && forgeSt.open && !/INPUT/.test(document.activeElement.tagName)) forgeClose(); });
EXTRA_COMMANDS.push(async (t) => {
  if (/^(add (a )?node|flash( a)?( board| node)?|node forge|forge)$/.test(t)) { selectDeck("sonar"); forgeOpen(); return "Plug the board into its UART port, enter your Wi-Fi details and press make it a sensor node."; }
  if (/^(listen|listen for nodes)$/.test(t)) { const r = await post("/api/forge", { listen: true }); return r && r.ok ? "Listening for sensor nodes." : (r && r.error) || "I could not start listening."; }
  return null;
});
Object.assign(window.netwatch, { forgeSt, forgeOpen, forgeClose, forgeFlash, forgeLoad, forgeState });
