"use strict";
// The dashboard. The server sends everything in °C, m/s, hPa, mm and km;
// this file converts for display and draws the charts as plain SVG.
(() => {
  const $ = (id) => document.getElementById(id);
  const SVG = "http://www.w3.org/2000/svg";
  const root = document.documentElement;

  // Viewer preferences. Storage can be missing or throw (private windows,
  // blocked site data); the page works the same without it.
  const store = {
    get(k) { try { return localStorage.getItem("sk." + k); } catch { return null; } },
    set(k, v) { try { localStorage.setItem("sk." + k, v); } catch { /* fine */ } },
  };
  let units = store.get("units") || root.dataset.units || "us";
  let range = store.get("range") || "day";
  if (!["day", "week", "month", "year"].includes(range)) range = "day";

  let now = null;        // the last /api/now response
  let hist = null;       // the last /api/history response
  let windRecent = [];   // rapid wind samples, newest last
  let streamLive = false;

  // ---- Units ---------------------------------------------------------------

  const KINDS = {
    temp: { us: (c) => c * 9 / 5 + 32, metric: (c) => c, unit: { us: "°F", metric: "°C" }, dp: 1 },
    speed: { us: (v) => v * 2.2369363, metric: (v) => v * 3.6, unit: { us: "mph", metric: "km/h" }, dp: 1 },
    press: { us: (v) => v * 0.0295299831, metric: (v) => v, unit: { us: "inHg", metric: "hPa" }, dp: { us: 2, metric: 1 } },
    rain: { us: (v) => v / 25.4, metric: (v) => v, unit: { us: "in", metric: "mm" }, dp: { us: 2, metric: 1 } },
    rate: { us: (v) => v / 25.4, metric: (v) => v, unit: { us: "in/h", metric: "mm/h" }, dp: { us: 2, metric: 1 } },
    dist: { us: (v) => v * 0.621371, metric: (v) => v, unit: { us: "mi", metric: "km" }, dp: 0 },
    pct: { us: (v) => v, metric: (v) => v, unit: { us: "%", metric: "%" }, dp: 0 },
    solar: { us: (v) => v, metric: (v) => v, unit: { us: "W/m²", metric: "W/m²" }, dp: 0 },
    uv: { us: (v) => v, metric: (v) => v, unit: { us: "", metric: "" }, dp: 1 },
  };

  const conv = (kind, v) => (v == null ? null : KINDS[kind][units](v));
  const unitOf = (kind) => KINDS[kind].unit[units];
  function dpOf(kind, v) {
    const dp = KINDS[kind].dp;
    const n = typeof dp === "number" ? dp : dp[units];
    // Wind under 10 reads better with a decimal; above it the decimal is noise.
    if (kind === "speed" && Math.abs(v) >= 10) return 0;
    return n;
  }
  function numText(kind, v) {
    if (v == null || Number.isNaN(v)) return "–";
    return v.toLocaleString(undefined, { minimumFractionDigits: dpOf(kind, v), maximumFractionDigits: dpOf(kind, v) });
  }
  // fmt converts and formats a server value, with its unit.
  function fmt(kind, v, { unit = true } = {}) {
    const c = conv(kind, v);
    if (c == null) return "–";
    const u = unitOf(kind);
    if (!unit || !u) return numText(kind, c);
    return kind === "temp" ? numText(kind, c) + u : numText(kind, c) + " " + u;
  }
  const degrees = (v) => (v == null ? "–" : numText("temp", conv("temp", v)) + "°");

  // ---- Words ---------------------------------------------------------------

  const POINTS = ["N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"];
  const compassPoint = (d) => POINTS[Math.round(((d % 360) + 360) % 360 / 22.5) % 16];

  // The Beaufort scale's names, by its m/s bounds.
  function windWords(ms) {
    const scale = [[0.5, "Calm"], [1.6, "Light air"], [3.4, "Light breeze"], [5.5, "Gentle breeze"], [8, "Moderate breeze"],
      [10.8, "Fresh breeze"], [13.9, "Strong breeze"], [17.2, "Near gale"], [20.8, "Gale"], [24.5, "Strong gale"], [Infinity, "Storm"]];
    return scale.find(([max]) => ms < max)[1];
  }
  // How the dew point feels, by the usual °F bands.
  function dewWords(c) {
    if (c == null) return "";
    const f = c * 9 / 5 + 32;
    if (f < 50) return "Dry";
    if (f < 60) return "Comfortable";
    if (f < 65) return "Sticky";
    if (f < 70) return "Humid";
    if (f < 75) return "Muggy";
    return "Oppressive";
  }
  function uvWords(uv) {
    if (uv == null) return "–";
    if (uv < 3) return "Low";
    if (uv < 6) return "Moderate";
    if (uv < 8) return "High";
    if (uv < 11) return "Very high";
    return "Extreme";
  }

  // ---- Time ----------------------------------------------------------------

  const fTime = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
  const fHour = new Intl.DateTimeFormat(undefined, { hour: "numeric" });
  const fDay = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
  const fWeekday = new Intl.DateTimeFormat(undefined, { weekday: "short" });
  const fMonth = new Intl.DateTimeFormat(undefined, { month: "short" });
  const fDate = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" });
  const fDayTime = new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });
  const d = (t) => new Date(t * 1000);

  function ago(t) {
    if (!t) return "never";
    const s = Math.max(0, Math.round(Date.now() / 1000 - t));
    if (s < 5) return "just now";
    if (s < 60) return s + " s ago";
    if (s < 3600) return Math.round(s / 60) + " min ago";
    if (s < 86400 * 2) return Math.round(s / 3600) + " h ago";
    return fDate.format(d(t));
  }
  const isToday = (t) => t && new Date(t * 1000).toDateString() === new Date().toDateString();
  const at = (t) => (t ? (isToday(t) ? "at " + fTime.format(d(t)) : fDayTime.format(d(t))) : "");

  // ---- Data ----------------------------------------------------------------

  async function getJSON(url) {
    const r = await fetch(url, { headers: { Accept: "application/json" } });
    if (!r.ok) throw new Error(url + ": " + r.status);
    return r.json();
  }

  let nowPending = false;
  async function fetchNow() {
    if (nowPending) return;
    nowPending = true;
    try {
      now = await getJSON("/api/now");
      if (now.windRecent) windRecent = now.windRecent;
      renderNow();
    } catch (e) {
      showBanner("The dashboard can't reach its server right now. Retrying.");
    } finally {
      nowPending = false;
    }
  }

  async function fetchHistory() {
    const charts = $("charts");
    charts.classList.add("is-loading");
    try {
      hist = await getJSON("/api/history?range=" + range);
      renderCharts();
    } catch (e) {
      // Keep the previous render; the next refresh tries again.
    } finally {
      charts.classList.remove("is-loading");
    }
  }

  function connectStream() {
    if (!window.EventSource) return;
    const es = new EventSource("/api/stream");
    es.addEventListener("open", () => { streamLive = true; renderUpdated(); });
    es.addEventListener("error", () => { streamLive = false; renderUpdated(); });
    es.addEventListener("wind", (e) => {
      const w = JSON.parse(e.data);
      windRecent.push(w);
      const cutoff = w.t - 600;
      while (windRecent.length && windRecent[0].t < cutoff) windRecent.shift();
      if (now) now.health.lastPacket = w.t;
      renderWind();
      renderUpdated();
    });
    for (const kind of ["obs", "strike", "rain"]) es.addEventListener(kind, fetchNow);
  }

  // ---- Rendering: current conditions ---------------------------------------

  const text = (id, s) => { const el = $(id); if (el.textContent !== s) el.textContent = s; };

  function showBanner(msg) {
    const b = $("banner");
    if (!msg) { b.hidden = true; return; }
    text("banner-text", msg);
    b.hidden = false;
  }

  function renderUpdated() {
    if (!now) return;
    const last = now.health.lastPacket || now.time;
    const fresh = last && Date.now() / 1000 - last < 180;
    $("updated").classList.toggle("is-live", streamLive && fresh);
    text("updated-text", now.source === "live" && fresh ? "Live · updated " + ago(last) : "Last reading " + ago(now.time));
  }

  function renderNow() {
    const c = now.current;
    text("temp", c.temp == null ? "–" : numText("temp", conv("temp", c.temp)) + "°");
    text("feels", degrees(c.feels));
    text("today-high", degrees(now.today.high.v));
    text("today-high-at", at(now.today.high.t));
    text("today-low", degrees(now.today.low.v));
    text("today-low-at", at(now.today.low.t));

    const words = [];
    if (c.dew != null) words.push(dewWords(c.dew));
    if (c.wind != null) words.push(windWords(c.wind).toLowerCase());
    if (c.rainRate > 0) words.push(c.precipType === 2 ? "hail" : c.precipType === 3 ? "rain and hail" : "raining");
    const s = words.join(", ");
    text("summary", s ? s[0].toUpperCase() + s.slice(1) : "–");

    renderWind();

    text("humidity", c.humidity == null ? "–" : Math.round(c.humidity) + "%");
    text("dew", fmt("temp", c.dew));
    text("dew-words", c.dew == null ? "" : dewWords(c.dew));

    text("pressure", fmt("press", c.pressure));
    const tr = c.pressureTrend;
    if (tr == null) text("pressure-trend", "Trend not available yet");
    else {
      const amount = fmt("press", Math.abs(tr));
      text("pressure-trend", tr >= 1 ? "↑ Rising " + amount : tr <= -1 ? "↓ Falling " + amount : "→ Steady");
    }

    const r = now.rain;
    text("rain-today", fmt("rain", r.today));
    text("rain-rate", c.rainRate > 0 ? "Falling at " + fmt("rate", c.rainRate) : r.started && isToday(r.started) ? "Started " + at(r.started) : "Not raining");
    text("rain-hour", fmt("rain", r.hour));
    text("rain-day", fmt("rain", r.day));
    text("rain-yesterday", fmt("rain", r.yesterday));
    text("rain-month", fmt("rain", r.month));
    text("rain-year", fmt("rain", r.year));

    text("uv", c.uv == null ? "–" : numText("uv", c.uv));
    text("uv-words", uvWords(c.uv));
    text("solar", fmt("solar", c.solar));
    text("lux", c.illuminance == null ? "–" : Math.round(c.illuminance).toLocaleString() + " lx");

    const l = now.lightning;
    text("strikes", String(l.count3h));
    text("strike-last", l.last ? "Last one " + fmt("dist", l.last.km) + " away, " + ago(l.last.t) : "None nearby");

    const rec = now.records;
    text("records-since", rec.since ? "since " + fDate.format(d(rec.since)) : "");
    recordText("rec-high", fmt("temp", rec.high.v), rec.high.t);
    recordText("rec-low", fmt("temp", rec.low.v), rec.low.t);
    recordText("rec-gust", fmt("speed", rec.gust.v), rec.gust.t);
    recordText("rec-wet", fmt("rain", rec.wettest.v), rec.wettest.t);

    const h = now.health;
    const parts = ["Station heard " + ago(h.lastPacket)];
    if (h.battery != null) parts.push("battery " + h.battery.toFixed(2) + " V");
    if (h.rssi) parts.push("signal " + h.rssi + " dBm");
    if (h.hubRssi) parts.push("hub Wi-Fi " + h.hubRssi + " dBm");
    if (h.archive) parts.push("archive " + ago(h.archive));
    text("health", parts.join(" · "));

    const stale = !h.lastPacket || Date.now() / 1000 - h.lastPacket > 180;
    if (now.errors && now.errors.includes("archive")) showBanner("The weather archive can't be read right now, so history and totals are missing.");
    else if (stale) showBanner(now.time ? "No broadcasts from the station since " + (h.lastPacket ? at(h.lastPacket) : "this page loaded") + ". Showing the last saved reading, from " + ago(now.time) + "." : "Waiting for the station's first broadcast.");
    else showBanner("");
    renderUpdated();
  }

  function recordText(id, value, t) {
    const el = $(id);
    el.textContent = value;
    if (t) {
      const w = document.createElement("span");
      w.className = "when";
      w.textContent = fDate.format(d(t));
      el.append(w);
    }
  }

  // ---- Wind ------------------------------------------------------------------

  let needleAngle = null;
  let needleAnim = 0;
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

  function setNeedle(target) {
    const g = $("compass-needle");
    if (needleAngle == null || reduceMotion.matches) {
      needleAngle = target;
      g.setAttribute("transform", "rotate(" + target.toFixed(1) + ")");
      return;
    }
    // Turn the short way round: 350° to 10° is +20°, not −340°.
    const from = needleAngle;
    const delta = ((target - from + 540) % 360) - 180;
    const start = performance.now();
    cancelAnimationFrame(needleAnim);
    const step = (ts) => {
      const k = Math.min(1, (ts - start) / 900);
      const e = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2;
      needleAngle = (from + delta * e + 360) % 360;
      g.setAttribute("transform", "rotate(" + needleAngle.toFixed(1) + ")");
      if (k < 1) needleAnim = requestAnimationFrame(step);
    };
    needleAnim = requestAnimationFrame(step);
  }

  function drawCompassTicks() {
    const g = $("compass-ticks");
    for (let a = 0; a < 360; a += 10) {
      const major = a % 90 === 0;
      const r1 = major ? 42 : 46;
      const rad = (a * Math.PI) / 180;
      const l = el("line", { x1: Math.sin(rad) * r1, y1: -Math.cos(rad) * r1, x2: Math.sin(rad) * 50, y2: -Math.cos(rad) * 50 });
      if (major) l.setAttribute("class", "major");
      g.append(l);
    }
  }

  function renderWind() {
    if (!now) return;
    const c = now.current;
    const latest = windRecent.length ? windRecent[windRecent.length - 1] : null;
    const speed = latest ? latest.s : c.wind;
    const dir = latest && latest.s > 0 ? latest.d : c.windDir;
    text("wind-now", fmt("speed", speed));
    text("wind-from", speed == null ? "–" : speed < 0.5 ? "Calm" : dir == null ? windWords(speed) : "From the " + compassPoint(dir) + " (" + Math.round(dir) + "°) · " + windWords(speed).toLowerCase());
    text("wind-gust", fmt("speed", c.gust));
    text("wind-lull", fmt("speed", c.lull));
    text("wind-today", fmt("speed", now.today.gust.v));
    if (dir != null) setNeedle(dir);

    // Where the wind has come from over the last ten minutes, faint dots on
    // the rim, the recent ones darker.
    const trail = $("compass-trail");
    trail.replaceChildren();
    const n = windRecent.length;
    windRecent.forEach((w, i) => {
      if (!(w.s > 0) || w.d == null) return;
      const rad = (w.d * Math.PI) / 180;
      const dot = el("circle", { cx: Math.sin(rad) * 38, cy: -Math.cos(rad) * 38, r: 2.2, class: "trail" });
      dot.setAttribute("fill-opacity", (0.08 + 0.5 * (i / Math.max(1, n - 1))).toFixed(2));
      trail.append(dot);
    });

    drawSpark();
  }

  function drawSpark() {
    const svg = $("wind-spark");
    const W = svg.clientWidth || 300;
    const H = 44;
    svg.setAttribute("viewBox", "0 0 " + W + " " + H);
    svg.replaceChildren();
    if (windRecent.length < 2) return;
    const t1 = windRecent[windRecent.length - 1].t;
    const t0 = t1 - 600;
    const max = Math.max(1, ...windRecent.map((w) => w.s || 0)) * 1.1;
    const x = (t) => ((t - t0) / 600) * W;
    const y = (v) => H - 2 - (v / max) * (H - 4);
    let p = "";
    windRecent.forEach((w, i) => { p += (i ? "L" : "M") + x(w.t).toFixed(1) + " " + y(w.s || 0).toFixed(1); });
    svg.append(el("path", { d: p + "L" + x(t1).toFixed(1) + " " + H + "L" + x(windRecent[0].t).toFixed(1) + " " + H + "Z", class: "area" }));
    svg.append(el("path", { d: p, class: "line" }));
  }

  // ---- Charts ------------------------------------------------------------

  const CHARTS = [
    { id: "temp", title: "Temperature", kind: "temp", series: [{ key: "temp", name: "Temperature" }, { key: "dew", name: "Dew point" }] },
    { id: "wind", title: "Wind", kind: "speed", zero: true, series: [{ key: "wind", name: "Average" }, { key: "gust", name: "Gust" }] },
    { id: "pressure", title: "Pressure at sea level", kind: "press", series: [{ key: "pressure", name: "Pressure" }] },
    { id: "rain", title: "Rain", kind: "rain", zero: true, columns: true, series: [{ key: "rain", name: "Rain" }] },
    { id: "humidity", title: "Humidity", kind: "pct", fixed: [0, 100], series: [{ key: "humidity", name: "Humidity" }] },
    { id: "solar", title: "Solar radiation", kind: "solar", zero: true, wash: true, series: [{ key: "solar", name: "Solar radiation" }] },
  ];
  const charts = []; // { spec, svg, x(i), y(v), vals, plot }
  let hover = null;  // index into hist.t under the pointer, shared by every chart

  function el(name, attrs) {
    const e = document.createElementNS(SVG, name);
    for (const k in attrs) e.setAttribute(k, attrs[k]);
    return e;
  }

  function buildCharts() {
    const host = $("charts");
    for (const spec of CHARTS) {
      const card = document.createElement("figure");
      card.className = "chart";
      const head = document.createElement("figcaption");
      head.className = "chart-head";
      const h = document.createElement("h3");
      h.textContent = spec.title + " ";
      const sub = document.createElement("span");
      sub.className = "when";
      h.append(sub);
      head.append(h);
      if (spec.series.length > 1) {
        const lg = document.createElement("span");
        lg.className = "legend";
        spec.series.forEach((s, i) => {
          const item = document.createElement("span");
          const key = document.createElement("i");
          key.className = "key bg-" + (i + 1);
          item.append(key, document.createTextNode(s.name));
          lg.append(item);
        });
        head.append(lg);
      }
      const svg = el("svg", { role: "img", tabindex: "0", "aria-label": spec.title + " chart. Use the arrow keys to read values." });
      card.append(head, svg);
      host.append(card);
      const c = { spec, svg, sub };
      charts.push(c);
      svg.addEventListener("pointermove", (e) => { setHover(nearest(c, e), c, e); });
      svg.addEventListener("pointerleave", () => setHover(null));
      svg.addEventListener("keydown", (e) => keyHover(c, e));
      svg.addEventListener("blur", () => setHover(null));
    }
  }

  function niceTicks(lo, hi, count) {
    if (lo === hi) { lo -= 1; hi += 1; }
    const raw = (hi - lo) / count;
    const mag = Math.pow(10, Math.floor(Math.log10(raw)));
    const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => raw <= s);
    const start = Math.floor(lo / step) * step;
    const ticks = [];
    for (let v = start; v <= hi + step * 1e-9; v += step) ticks.push(+v.toFixed(10));
    if (ticks[ticks.length - 1] < hi) ticks.push(ticks[ticks.length - 1] + step);
    return { ticks, step };
  }

  function timeTicks(from, to, width) {
    const out = [];
    const t = new Date(from * 1000);
    t.setMinutes(0, 0, 0);
    if (range === "day") {
      const every = width < 420 ? 6 : 3;
      while (t.getTime() / 1000 <= to) {
        if (t.getTime() / 1000 >= from && t.getHours() % every === 0) out.push([t.getTime() / 1000, fHour.format(t)]);
        t.setHours(t.getHours() + 1);
      }
    } else if (range === "week") {
      t.setHours(0);
      while (t.getTime() / 1000 <= to) {
        if (t.getTime() / 1000 >= from) out.push([t.getTime() / 1000, fWeekday.format(t)]);
        t.setDate(t.getDate() + 1);
      }
    } else if (range === "month") {
      t.setHours(0);
      const every = width < 420 ? 10 : 5;
      while (t.getTime() / 1000 <= to) {
        if (t.getTime() / 1000 >= from && (t.getDate() - 1) % every === 0 && t.getDate() < 30) out.push([t.getTime() / 1000, fDay.format(t)]);
        t.setDate(t.getDate() + 1);
      }
    } else {
      t.setHours(0);
      t.setDate(1);
      const every = width < 420 ? 3 : width < 700 ? 2 : 1;
      while (t.getTime() / 1000 <= to) {
        if (t.getTime() / 1000 >= from && t.getMonth() % every === 0) out.push([t.getTime() / 1000, fMonth.format(t)]);
        t.setMonth(t.getMonth() + 1);
      }
    }
    return out;
  }

  function renderCharts() {
    if (!hist) return;
    for (const c of charts) drawChart(c);
    if (hover != null) setHover(hover);
    const tv = document.querySelector(".table-view");
    if (tv.open) renderTable();
  }

  function drawChart(c) {
    const { spec, svg } = c;
    const W = svg.clientWidth || 600;
    const H = 200;
    const M = { l: 44, r: 52, t: 10, b: 24 };
    svg.setAttribute("viewBox", "0 0 " + W + " " + H);
    svg.replaceChildren();

    const vals = spec.series.map((s) => hist[s.key].map((v) => conv(spec.kind, v)));
    c.vals = vals;
    const all = vals.flat().filter((v) => v != null);
    const pw = W - M.l - M.r;
    const ph = H - M.t - M.b;
    const from = hist.from;
    const to = hist.to;
    const half = spec.columns ? hist.width / 2 : 0;
    const x = (i) => M.l + ((hist.t[i] + half - from) / (to - from)) * pw;
    c.x = x;

    // The unit names the axis, in the heading where it cannot collide with a
    // tick label.
    c.sub.textContent = unitOf(spec.kind);
    if (spec.id === "rain" && all.length) {
      const total = vals[0].reduce((a, v) => a + (v || 0), 0);
      c.sub.textContent = unitOf("rain") + " · total " + numText("rain", total) + " " + unitOf("rain");
    }

    if (!all.length) {
      svg.append(el("text", { x: W / 2, y: H / 2, class: "empty" })).textContent = "No data for this range yet";
      c.y = null;
      return;
    }

    let lo = Math.min(...all);
    let hi = Math.max(...all);
    if (spec.fixed) [lo, hi] = spec.fixed;
    if (spec.zero) lo = Math.min(0, lo);
    if (spec.zero && hi <= 0) hi = spec.kind === "rain" ? (units === "us" ? 0.1 : 2) : 1;
    const { ticks } = niceTicks(lo, hi, 4);
    const y0 = spec.fixed ? spec.fixed[0] : ticks[0];
    const y1 = spec.fixed ? spec.fixed[1] : ticks[ticks.length - 1];
    const y = (v) => M.t + ph - ((v - y0) / (y1 - y0)) * ph;
    c.y = y;

    const grid = el("g", { class: "grid" });
    for (const tv of ticks) {
      if (tv < y0 || tv > y1) continue;
      grid.append(el("line", { x1: M.l, x2: M.l + pw, y1: y(tv), y2: y(tv) }));
      const lbl = el("text", { x: M.l - 8, y: y(tv), class: "tick", "text-anchor": "end", "dominant-baseline": "middle" });
      lbl.textContent = tv.toLocaleString(undefined, { maximumFractionDigits: 2 });
      svg.append(lbl);
    }
    svg.prepend(grid);
    svg.append(el("line", { x1: M.l, x2: M.l + pw, y1: M.t + ph, y2: M.t + ph, class: "baseline" }));
    for (const [t, label] of timeTicks(from, to, W)) {
      const tx = M.l + ((t - from) / (to - from)) * pw;
      const lbl = el("text", { x: tx, y: H - 6, class: "tick", "text-anchor": "middle" });
      lbl.textContent = label;
      svg.append(lbl);
    }

    if (spec.columns) {
      // Columns from one baseline, rounded at the data end only, with air
      // between them.
      const slot = pw / hist.t.length;
      const bw = Math.max(1, Math.min(24, slot - 2));
      const g = el("g", { class: "series-1" });
      vals[0].forEach((v, i) => {
        if (!v) return;
        const cx = x(i);
        const top = y(v);
        const base = y(0);
        const h = Math.max(1, base - top);
        const r = Math.min(4, bw / 2, h);
        const l = cx - bw / 2;
        g.append(el("path", {
          class: "col",
          d: "M" + l + " " + base + "V" + (base - h + r) + "Q" + l + " " + (base - h) + " " + (l + r) + " " + (base - h) +
            "H" + (l + bw - r) + "Q" + (l + bw) + " " + (base - h) + " " + (l + bw) + " " + (base - h + r) + "V" + base + "Z",
        }));
      });
      svg.append(g);
    } else {
      vals.forEach((vs, si) => {
        let d = "";
        let pen = false;
        let first = null;
        let last = null;
        vs.forEach((v, i) => {
          if (v == null) { pen = false; return; }
          d += (pen ? "L" : "M") + x(i).toFixed(1) + " " + y(v).toFixed(1);
          pen = true;
          if (first == null) first = i;
          last = i;
        });
        if (spec.wash && si === 0 && first != null) {
          svg.append(el("path", { d: d + "L" + x(last).toFixed(1) + " " + y(Math.max(y0, 0)) + "L" + x(first).toFixed(1) + " " + y(Math.max(y0, 0)) + "Z", class: "wash series-" + (si + 1) }));
        }
        svg.append(el("path", { d, class: "line series-" + (si + 1) }));
      });
      // The newest value of each line, at its end. When two would collide
      // only the first is labeled; the legend and tooltip carry the other.
      const placed = [];
      vals.forEach((vs, si) => {
        let i = vs.length - 1;
        while (i >= 0 && vs[i] == null) i--;
        if (i < 0) return;
        const ly = y(vs[i]);
        if (placed.some((p) => Math.abs(p - ly) < 13)) return;
        placed.push(ly);
        const t = el("text", { x: x(i) + 6, y: ly, class: "end-label", "dominant-baseline": "middle" });
        t.textContent = numText(spec.kind, vs[i]);
        svg.append(t);
        svg.append(el("circle", { cx: x(i), cy: ly, r: 4, class: "dot series-" + (si + 1) }));
      });
    }
    c.overlay = el("g", {});
    svg.append(c.overlay);
    c.plot = { M, pw, ph, H };
  }

  function nearest(c, e) {
    if (!hist || !c.plot) return null;
    const box = c.svg.getBoundingClientRect();
    const px = ((e.clientX - box.left) / box.width) * (c.svg.viewBox.baseVal.width || box.width);
    let best = null;
    let bestD = Infinity;
    for (let i = 0; i < hist.t.length; i++) {
      const dx = Math.abs(c.x(i) - px);
      if (dx < bestD) { bestD = dx; best = i; }
    }
    return best;
  }

  function keyHover(c, e) {
    if (!hist) return;
    const n = hist.t.length;
    let i = hover == null ? n - 1 : hover;
    if (e.key === "ArrowLeft") i = Math.max(0, i - 1);
    else if (e.key === "ArrowRight") i = Math.min(n - 1, i + 1);
    else if (e.key === "Home") i = 0;
    else if (e.key === "End") i = n - 1;
    else if (e.key === "Escape") { setHover(null); return; }
    else return;
    e.preventDefault();
    setHover(i, c);
  }

  // setHover draws the crosshair at index i on every chart, and the tooltip
  // beside the chart the reader is pointing at.
  function setHover(i, active, ev) {
    hover = i;
    const tip = $("tooltip");
    for (const c of charts) {
      if (!c.overlay) continue;
      c.overlay.replaceChildren();
      if (i == null || !c.y) continue;
      const cx = c.x(i);
      const { M, ph } = c.plot;
      c.overlay.append(el("line", { x1: cx, x2: cx, y1: M.t, y2: M.t + ph, class: "cross" }));
      if (!c.spec.columns) {
        c.vals.forEach((vs, si) => {
          if (vs[i] != null) c.overlay.append(el("circle", { cx, cy: c.y(vs[i]), r: 4, class: "dot series-" + (si + 1) }));
        });
      }
    }
    if (i == null || !active) { tip.hidden = true; return; }

    tip.replaceChildren();
    const head = document.createElement("div");
    head.className = "tt-time";
    head.textContent = bucketLabel(i);
    tip.append(head);
    active.spec.series.forEach((s, si) => {
      const row = document.createElement("div");
      row.className = "tt-row";
      const key = document.createElement("i");
      key.className = "key bg-" + (si + 1);
      const v = document.createElement("strong");
      v.textContent = fmt(active.spec.kind, hist[s.key][i]);
      const name = document.createElement("span");
      name.textContent = s.name;
      row.append(key, v, name);
      tip.append(row);
    });
    if (active.spec.id === "wind" && hist.windDir[i] != null) {
      const row = document.createElement("div");
      row.className = "tt-row";
      row.textContent = "From the " + compassPoint(hist.windDir[i]);
      tip.append(row);
    }
    tip.hidden = false;
    const box = active.svg.getBoundingClientRect();
    const scale = box.width / (active.svg.viewBox.baseVal.width || box.width);
    let left = box.left + active.x(i) * scale + 14;
    const top = ev ? ev.clientY - 20 : box.top + 20;
    if (left + tip.offsetWidth > window.innerWidth - 8) left = box.left + active.x(i) * scale - tip.offsetWidth - 14;
    tip.style.left = Math.max(8, left) + "px";
    tip.style.top = Math.max(8, Math.min(top, window.innerHeight - tip.offsetHeight - 8)) + "px";
  }

  function bucketLabel(i) {
    const t = hist.t[i];
    const w = hist.width;
    if (w >= 86400) return fDate.format(d(t));
    const end = t + w;
    if (w >= 3600) return fDay.format(d(t)) + ", " + fTime.format(d(t)) + "–" + fTime.format(d(end));
    return fTime.format(d(t)) + "–" + fTime.format(d(end));
  }

  function renderTable() {
    const wrap = $("table-wrap");
    if (!hist) return;
    const cols = [["temp", "Temperature", "temp"], ["dew", "Dew point", "temp"], ["humidity", "Humidity", "pct"], ["pressure", "Pressure", "press"],
      ["wind", "Wind", "speed"], ["gust", "Gust", "speed"], ["rain", "Rain", "rain"], ["solar", "Solar", "solar"], ["uv", "UV", "uv"]];
    const table = document.createElement("table");
    const thead = table.createTHead().insertRow();
    const th0 = document.createElement("th");
    th0.textContent = "Time";
    thead.append(th0);
    for (const [, name, kind] of cols) {
      const th = document.createElement("th");
      th.textContent = name + (unitOf(kind) ? " (" + unitOf(kind) + ")" : "");
      thead.append(th);
    }
    const body = table.createTBody();
    for (let i = hist.t.length - 1; i >= 0; i--) {
      const row = body.insertRow();
      row.insertCell().textContent = bucketLabel(i);
      for (const [key, , kind] of cols) row.insertCell().textContent = fmt(kind, hist[key][i], { unit: false });
    }
    wrap.replaceChildren(table);
  }

  // ---- Controls --------------------------------------------------------------

  function setUnits(u) {
    units = u;
    store.set("units", u);
    for (const b of document.querySelectorAll("[data-set-units]")) b.setAttribute("aria-pressed", String(b.dataset.setUnits === u));
    if (now) renderNow();
    renderCharts();
  }

  function setRange(r) {
    range = r;
    store.set("range", r);
    for (const b of document.querySelectorAll("[data-range]")) b.setAttribute("aria-pressed", String(b.dataset.range === r));
    setHover(null);
    fetchHistory();
  }

  function start() {
    drawCompassTicks();
    buildCharts();
    for (const b of document.querySelectorAll("[data-set-units]")) b.addEventListener("click", () => setUnits(b.dataset.setUnits));
    for (const b of document.querySelectorAll("[data-range]")) b.addEventListener("click", () => setRange(b.dataset.range));
    document.querySelector(".table-view").addEventListener("toggle", (e) => { if (e.target.open) renderTable(); });
    setUnits(units);
    setRange(range);
    fetchNow();
    // Only after the load event: a stream opened before it is a request that
    // never finishes, so the page would never finish loading and the tab's
    // spinner would turn forever.
    if (document.readyState === "complete") connectStream();
    else window.addEventListener("load", connectStream, { once: true });

    // Redraw on a change of width only. Drawing can change the container's
    // height, and redrawing on that as well would loop for as long as the
    // page is open.
    let raf = 0;
    let lastWidth = 0;
    new ResizeObserver((entries) => {
      const w = Math.round(entries[0].contentRect.width);
      if (w === lastWidth) return;
      lastWidth = w;
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => { renderCharts(); drawSpark(); });
    }).observe($("charts"));

    // The stream drives updates; these are the backstop for a stream that
    // has quietly died, and for the archive totals that change without a
    // broadcast.
    setInterval(fetchNow, 60 * 1000);
    setInterval(fetchHistory, 5 * 60 * 1000);
    setInterval(renderUpdated, 1000);
    document.addEventListener("visibilitychange", () => { if (!document.hidden) { fetchNow(); fetchHistory(); } });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
