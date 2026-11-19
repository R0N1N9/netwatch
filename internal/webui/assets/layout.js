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

EXTRA_COMMANDS.push(async (t) => {
  let m;
  if ((m = t.match(/^compact (on|off)$/))) return setCompact(m[1] === "on");
  if (/^(pop ?out|new window|pop ?out (this )?deck)$/.test(t)) { popOut(currentDeck()); return "Opened this deck in its own window."; }
  if ((m = t.match(/^pop ?out\s+(.+)$/)) && DECK_WORDS[m[1]]) { popOut(DECK_WORDS[m[1]]); return "Opened " + m[1].toUpperCase() + " in its own window."; }
  return null;
});
Object.assign(window.netwatch, { layout, panelsOf, panelMax, popOut, setCompact, fitScreen });
