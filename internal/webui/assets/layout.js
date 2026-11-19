"use strict";
// Fitting the console to the screen: a compact mode for small displays, an
// expand button on every panel, and popping a deck or a panel out into its
// own window (each window is a full console showing just that part).
// Loaded last, after every deck's script.
const layout = { maxed: null };
const layoutQS = new URLSearchParams(location.search);
function panelsOf(deck) { return [...document.querySelectorAll('.deck[data-deck="' + deck + '"] .panel, .deck[data-deck="' + deck + '"] .tpanel')]; }
function layoutRefit() { setTimeout(() => window.dispatchEvent(new Event("resize")), 60); }

// ---- expand one panel to fill the window ----
function panelMax(p, on) {
  if (layout.maxed && layout.maxed !== p) layout.maxed.classList.remove("max");
  if (!p) { layout.maxed = null; document.body.classList.remove("has-max"); layoutRefit(); return; }
  p.classList.toggle("max", on);
  layout.maxed = on ? p : null;
  document.body.classList.toggle("has-max", !!on);
  const b = p.querySelector(".pn-max"); if (b) { b.textContent = on ? "⤡" : "⤢"; b.title = on ? "back to normal size (Esc)" : "expand this panel to fill the window"; }
  layoutRefit();
}

// ---- pop a deck, or one panel of it, out into its own window ----
function popOut(deck, idx) {
  const url = location.pathname + "?solo=1&deck=" + encodeURIComponent(deck) + (idx != null ? "&panel=" + idx : "");
  const w = window.open(url, "netwatch-" + deck + (idx != null ? "-" + idx : ""), "width=1100,height=780");
  if (!w) status("the browser blocked the new window — allow pop-ups for this page");
  return url;
}

document.querySelectorAll(".deck").forEach((d) => {
  panelsOf(d.dataset.deck).forEach((p, i) => {
    const head = p.querySelector(".panel-head, .tp-tabs"); if (!head) return;
    const tools = document.createElement("span"); tools.className = "pn-tools";
    tools.innerHTML = '<button class="pn-btn pn-max" title="expand this panel to fill the window">⤢</button><button class="pn-btn pn-pop" title="open this panel in its own window">⧉</button>';
    tools.querySelector(".pn-max").onclick = (e) => { e.stopPropagation(); panelMax(p, !p.classList.contains("max")); };
    tools.querySelector(".pn-pop").onclick = (e) => { e.stopPropagation(); popOut(d.dataset.deck, i); };
    head.appendChild(tools);
  });
});
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && layout.maxed && !document.body.classList.contains("solo-panel")) panelMax(layout.maxed, false); });
$("#popDeck").onclick = () => popOut(currentDeck());

// ---- compact mode: more room for the decks on a short screen ----
function compactPref() { try { return localStorage.getItem("nw.compact"); } catch (_) { return null; } }
function fitScreen() {
  const pref = compactPref(), on = pref ? pref === "on" : window.innerHeight < 880;
  if (document.body.classList.contains("compact") !== on) { document.body.classList.toggle("compact", on); layoutRefit(); }
  $("#compactBtn").textContent = on ? "▣ FULL HUD" : "▢ COMPACT";
}
function setCompact(on) { try { localStorage.setItem("nw.compact", on ? "on" : "off"); } catch (_) {} fitScreen(); return on ? "Compact layout on." : "Full layout on."; }
$("#compactBtn").onclick = () => setCompact(!document.body.classList.contains("compact"));
window.addEventListener("resize", () => { if (!compactPref()) fitScreen(); });
fitScreen();

// ---- a popped-out window shows one deck, or one panel, and nothing else ----
(function solo() {
  if (!layoutQS.get("solo")) return;
  const deck = layoutQS.get("deck"), tab = deck && document.querySelector('.tab[data-deck="' + CSS.escape(deck) + '"]');
  if (!tab) return;
  document.body.classList.add("solo");
  selectDeck(deck);
  document.title = "NetWatch · " + tab.textContent.trim();
  const idx = layoutQS.get("panel");
  if (idx != null) {
    const p = panelsOf(deck)[+idx];
    if (p) { document.body.classList.add("solo-panel"); panelMax(p, true); const name = p.querySelector(".panel-head > span, .tp-tab.active"); if (name) document.title = "NetWatch · " + name.textContent.trim(); }
  }
  layoutRefit();
})();

// ---- the tab icon: its dot turns from green to red while a critical alert is unopened ----
const favi = { svg: null, state: "" };
fetch("favicon.svg").then((r) => (r.ok ? r.text() : null)).then((t) => { favi.svg = t; faviUpdate(); }).catch(() => {});
function faviUpdate() {
  if (!favi.svg) return;
  const hot = ((liveState && liveState.alerts) || []).some((a) => a.severity === "CRITICAL" && !a.acked), state = hot ? "alert" : "calm";
  if (state === favi.state) return;
  favi.state = state;
  $("#favicon").href = "data:image/svg+xml," + encodeURIComponent(hot ? favi.svg.replace(/#1dffa1/g, "#ff5449") : favi.svg);
}
RENDER_HOOKS.push(faviUpdate);

EXTRA_COMMANDS.push(async (t) => {
  let m;
  if ((m = t.match(/^compact (on|off)$/))) return setCompact(m[1] === "on");
  if (/^(pop ?out|new window|pop ?out (this )?deck)$/.test(t)) { popOut(currentDeck()); return "Opened this deck in its own window."; }
  if ((m = t.match(/^pop ?out\s+(.+)$/)) && DECK_WORDS[m[1]]) { popOut(DECK_WORDS[m[1]]); return "Opened " + m[1].toUpperCase() + " in its own window."; }
  return null;
});
Object.assign(window.netwatch, { layout, panelsOf, panelMax, popOut, setCompact, fitScreen, favi });

// ---- UPDATE card: the commands to stop, update and restart, ready to copy ----
const help = { open: false };
function helpCopy(text, btn) {
  const done = () => { btn.textContent = "COPIED"; btn.classList.add("on"); setTimeout(() => { btn.textContent = "COPY"; btn.classList.remove("on"); }, 1400); };
  const fallback = () => { const t = document.createElement("textarea"); t.value = text; document.body.appendChild(t); t.select(); try { document.execCommand("copy"); done(); } catch (_) {} t.remove(); };
  if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(done, fallback); else fallback();
}
function helpSteps(a) {
  const cd = a.repo ? "cd " + (/[^\w@%+=:,./-]/.test(a.repo) ? "'" + a.repo.replace(/'/g, "'\\''") + "'" : a.repo) : "cd ~/netwatch   # the folder you cloned NetWatch into";
  return [
    ["THE SHORT WAY · one command does all of it", [[cd + " && ./update.sh", "stops NetWatch, pulls, rebuilds, restores capture rights and starts it again"]]],
    ["STEP BY STEP", [[cd, "go to the NetWatch folder"], ["pkill -x netwatch", "stop the running copy (use sudo pkill if you started it with sudo)"], ["git pull", "get the latest code"], ["make build", "rebuild"], ["sudo setcap cap_net_raw,cap_net_admin+eip ./bin/netwatch", "let the new build capture packets without sudo — needed after every build"], ["./bin/netwatch gui", "start it"]]],
    ["JUST RESTART · no update", [["pkill -x netwatch; " + cd + " && ./bin/netwatch gui", "stop it and start it again"]]],
  ];
}
async function helpOpen() {
  const a = (await get("/api/about")) || {};
  help.about = a; help.open = true; $("#helpBox").hidden = false;
  $("#hpBuild").textContent = "this build: " + (a.build || "unknown") + (a.demo ? " · demo mode" : "");
  $("#hpData").textContent = a.dataDir || "memory only (no data folder in this run)";
  const body = $("#hpBody"); body.innerHTML = "";
  helpSteps(a).forEach(([title, lines]) => {
    body.appendChild(el("div", "micro cy", "▚▚ " + title));
    lines.forEach(([cmd, what]) => {
      const row = el("div", "hp-row"), code = el("code", null, cmd), b = el("button", "hs-btn", "COPY");
      b.onclick = () => helpCopy(cmd, b); row.appendChild(code); row.appendChild(b); row.appendChild(el("span", "hp-what", what)); body.appendChild(row);
    });
  });
  return a;
}
function helpClose() { help.open = false; $("#helpBox").hidden = true; }
$("#helpBtn").onclick = helpOpen; $("#helpClose").onclick = helpClose;
$("#helpBox").addEventListener("click", (e) => { if (e.target.id === "helpBox") helpClose(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && help.open) helpClose(); });
EXTRA_COMMANDS.push(async (t) => {
  if (/^(how (do|can) i |how to )?(update|upgrade|restart|reboot|kill|stop)( netwatch| it| this| the app| the process)?( and restart)?$|^(update|restart) (commands|instructions|help)$|^help$/.test(t)) { const a = await helpOpen(); return "The commands are on screen. The short way: " + (a.repo ? "cd " + a.repo + " && " : "in the NetWatch folder, ") + "./update.sh"; }
  return null;
});
Object.assign(window.netwatch, { helpOpen, helpClose, helpSteps });
