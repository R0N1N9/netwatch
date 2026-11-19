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
  const log = $("#fgLog");
  if (r.job) { log.hidden = false; log.textContent = r.job.lines.join("\n") + (r.job.done ? "\n" + (r.job.ok ? "✓ finished" : "✕ " + r.job.error) : "\n…"); log.className = "fg-log" + (r.job.done ? (r.job.ok ? " ok" : " bad") : ""); log.scrollTop = log.scrollHeight; }
}
async function forgeLoad() { const r = await get("/api/forge"); if (r && forgeSt.open) forgeRender(r); return r; }
async function forgeFlash(port, kind, settingsOnly) {
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
Object.assign(window.netwatch, { forgeSt, forgeOpen, forgeClose, forgeFlash, forgeLoad });
