"use strict";

const $ = (id) => document.getElementById(id);
const number = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 0 });
const money = new Intl.NumberFormat("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 3 });
const decimals = new Intl.NumberFormat("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const clock = new Intl.DateTimeFormat("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const minutes = new Intl.DateTimeFormat("ru-RU", { hour: "2-digit", minute: "2-digit" });
let state = null;
let received = 0;
let transport = false;
let connectionFailed = false;
let range = 300;

function text(id, value) { $(id).textContent = value; }
function signed(value, digits = 2) { return `${value > 0 ? "+" : ""}${digits === 0 ? Math.round(value) : decimals.format(value)}`; }
function tone(value) { return value > 0 ? "positive" : value < 0 ? "negative" : "muted"; }
function element(tag, value, className = "") {
  const node = document.createElement(tag);
  if (value !== undefined) node.textContent = value;
  if (className) node.className = className;
  return node;
}
function serverNow() { return state ? Date.parse(state.server_time) + (performance.now() - received) : Date.now(); }
function freshBook() {
  return transport && performance.now() - received < 4000 && state?.status === "connected" && state.book?.valid
    && serverNow() - Date.parse(state.book.time) <= 3000;
}

// Keep the book's ten rows stable as prices and volumes change.
const bookRows = Array.from({ length: 10 }, () => {
  const row = element("tr");
  const cells = Array.from({ length: 4 }, () => element("td"));
  [0, 3].forEach((index) => cells[index].append(element("i", undefined, "volume-bar"), element("span", "—", "volume-number")));
  cells[1].className = "positive"; cells[2].className = "negative";
  cells[1].textContent = cells[2].textContent = "—";
  row.append(...cells); $("book-rows").append(row);
  return cells;
});

function renderConnection() {
  const fresh = freshBook();
  const linked = transport && performance.now() - received < 4000;
  let label = "Соединяемся…", message = "Подключение к потоку. Первые данные появятся автоматически.", dot = "";
  if (!state && connectionFailed) {
    label = "Нет связи с приложением"; dot = "offline";
    message = "Не удалось подключиться к приложению. Убедитесь, что наблюдатель запущен. Повторяем подключение автоматически.";
  }
  if (state) {
    text("environment", state.environment === "sandbox" ? "Песочница" : "Production");
    if (!linked) {
      label = "Связь потеряна"; dot = "offline";
      message = "Нет связи с приложением. Переподключаемся автоматически. Если приложение остановлено, запустите его снова.";
    } else if (state.status !== "connected") {
      label = state.status === "connecting" ? "Подключение к API" : "Переподключение к API";
      message = state.message || "Ожидаем поток данных SBER.";
    } else if (!fresh) {
      label = "Нет свежего стакана";
      message = "Поток подключён, но свежего согласованного стакана пока нет. Сигнал приостановлен; в неактивные часы это нормально.";
    } else {
      label = "Прямой поток"; dot = "connected"; message = "";
    }
  }
  text("connection-status", label); $("status-dot").className = `status-dot ${dot}`;
  $("notice").hidden = !message; text("notice", message);
  $("book-content").classList.toggle("stale", !fresh);
  if (state?.book?.time) {
    const age = Math.max(0, (serverNow() - Date.parse(state.book.time)) / 1000);
    text("book-age", fresh ? `${decimals.format(age)} с назад` : "Нет свежих данных");
  } else { text("book-age", "Ожидание"); }
  renderSignal();
}

function factor(name, value, available) {
  text(`factor-${name}`, available ? signed(value) : "—");
  const bar = $(`factor-${name}-bar`);
  const v = available ? Math.max(-1, Math.min(1, value)) : 0;
  bar.style.left = `${50 + Math.min(0, v * 50)}%`;
  bar.style.width = `${Math.abs(v) * 50}%`;
  bar.style.backgroundColor = v < 0 ? "var(--red)" : "var(--green)";
  $(`factor-${name}`).className = tone(v);
}

function renderSignal() {
  const s = state?.signal;
  const available = freshBook() && s && serverNow() - Date.parse(s.time) <= (state.signal_interval_seconds + 3) * 1000;
  const ready = available && s.ready;
  const direction = ready ? s.direction : "Неопределённо";
  const score = available ? s.score * 100 : 0;
  const color = ready && direction === "вверх" ? "positive" : ready && direction === "вниз" ? "negative" : "muted";
  text("signal-direction", direction.charAt(0).toUpperCase() + direction.slice(1));
  $("signal-direction").className = color;
  const scoreNode = $("signal-score");
  scoreNode.replaceChildren(document.createTextNode(available ? signed(score, 0) : "—"), element("small", " / 100"));
  $("pressure-marker").style.left = `${50 + score / 2}%`;
  text("signal-reason", available ? s.reason : "Ожидаем свежие данные и следующий расчёт сигнала.");
  factor("book", s?.book_imbalance ?? 0, available);
  factor("flow", s?.order_flow ?? 0, available);
  factor("trades", s?.trade_imbalance ?? 0, available);
}

function renderBook() {
  const b = state.book;
  const bids = b?.bids ?? [], asks = b?.asks ?? [];
  const valid = b?.valid && bids.length && asks.length;
  text("mid", valid ? money.format(b.mid) : "—");
  text("best-bid", valid ? `${money.format(bids[0].price)} ₽` : "—");
  text("best-ask", valid ? `${money.format(asks[0].price)} ₽` : "—");
  text("spread", valid ? `${money.format(b.spread)} ₽ · ${decimals.format(b.spread / b.mid * 10000)} б.п.` : "—");
  document.title = valid ? `${money.format(b.mid)} ₽ · SBER · TradeGoPilot` : "SBER · TradeGoPilot";
  if (!valid) text("price-change", "Середина спреда · ожидание котировок");
  let bidTotal = 0, askTotal = 0;
  const maxVolume = Math.max(1, ...bids.map((l) => Number(l.quantity)), ...asks.map((l) => Number(l.quantity)));
  bookRows.forEach((cells, i) => {
    const bid = bids[i], ask = asks[i];
    cells[1].textContent = bid ? money.format(bid.price) : "—";
    cells[2].textContent = ask ? money.format(ask.price) : "—";
    [[0, bid], [3, ask]].forEach(([index, level]) => {
      cells[index].querySelector("span").textContent = level ? number.format(level.quantity) : "—";
      cells[index].querySelector("i").style.width = `${level ? Number(level.quantity) / maxVolume * 100 : 0}%`;
    });
    bidTotal += Number(bid?.quantity ?? 0); askTotal += Number(ask?.quantity ?? 0);
  });
  text("bid-total", `${number.format(bidTotal)} покупка`);
  text("ask-total", `${number.format(askTotal)} продажа`);
  $("balance-bid").style.width = `${bidTotal + askTotal ? bidTotal / (bidTotal + askTotal) * 100 : 50}%`;
}

function svg(tag, attributes, value) {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  Object.entries(attributes).forEach(([name, value]) => node.setAttribute(name, String(value)));
  if (value !== undefined) node.textContent = value;
  return node;
}

function renderChart() {
  const now = serverNow(), from = now - range * 1000;
  const points = (state?.points ?? []).filter((p) => Date.parse(p.time) >= from);
  const chart = $("chart"); chart.replaceChildren();
  $("chart-empty").hidden = points.length > 0;
  if (!points.length) { chart.setAttribute("aria-label", "За выбранный период нет котировок"); text("chart-count", "История текущего запуска"); return; }
  const left = 12, right = 704, top = 22, bottom = 246;
  const values = points.map((p) => p.mid);
  const lo = Math.min(...values), hi = Math.max(...values), pad = Math.max((hi - lo) * .15, .015);
  const min = lo - pad, max = hi + pad;
  const x = (time) => left + (time - from) / (range * 1000) * (right - left);
  const y = (price) => bottom - (price - min) / (max - min) * (bottom - top);
  for (let i = 0; i <= 4; i++) {
    const price = min + (max - min) * i / 4, cy = y(price);
    chart.append(svg("line", { x1: left, y1: cy, x2: right, y2: cy, stroke: "#243143", "stroke-dasharray": "3 5" }));
    chart.append(svg("text", { x: right + 12, y: cy + 5, fill: "#93a3b8", "font-size": 14 }, money.format(price)));
  }
  for (let i = 0; i <= 3; i++) {
    const at = from + range * 1000 * i / 3;
    chart.append(svg("text", { x: x(at), y: 273, fill: "#93a3b8", "font-size": 14, "text-anchor": i === 0 ? "start" : i === 3 ? "end" : "middle" }, minutes.format(at)));
  }
  let path = "", previous = 0;
  points.forEach((p) => {
    const at = Date.parse(p.time);
    path += `${previous && at - previous <= 3000 ? "L" : "M"}${x(at).toFixed(2)},${y(p.mid).toFixed(2)} `;
    previous = at;
  });
  chart.append(svg("path", { d: path, fill: "none", stroke: "#42d8aa", "stroke-width": 2, "stroke-linejoin": "round", "stroke-linecap": "round", "vector-effect": "non-scaling-stroke" }));
  const last = points[points.length - 1];
  text("price-change", `${signed(last.mid - points[0].mid)} ₽ за ${range / 60} мин · mid`);
  chart.append(svg("circle", { cx: x(Date.parse(last.time)), cy: y(last.mid), r: 3.5, fill: "#42d8aa" }));
  chart.setAttribute("aria-label", `Цена SBER за ${range / 60} минут: от ${money.format(lo)} до ${money.format(hi)} рублей; последняя ${money.format(last.mid)} рубля.`);
  text("chart-count", `${points.length} отсчётов · разрывы = нет данных`);
}

function renderTrades() {
  text("trade-count", `${number.format(state.trade_count)} за сеанс`);
  const trades = state.trades ?? [];
  if (!trades.length) return;
  const fragment = document.createDocumentFragment();
  trades.slice().reverse().forEach((t) => {
    const row = element("tr");
    const color = t.direction === "buy" ? "positive" : t.direction === "sell" ? "negative" : "muted";
    row.append(element("td", clock.format(Date.parse(t.time)), "muted"), element("td", money.format(t.price), color), element("td", number.format(t.quantity)), element("td", t.direction === "buy" ? "↗ Покупка" : t.direction === "sell" ? "↘ Продажа" : "Не определено", color));
    fragment.append(row);
  });
  $("trade-rows").replaceChildren(fragment);
}

function renderPredictions() {
  $("accuracy-strip").replaceChildren(...state.stats.map((s) => {
    const item = element("div");
    item.append(element("span", `${s.horizon} секунд`), element("strong", s.measured ? `${Math.round(s.correct / s.measured * 100)}%` : "—"), element("small", `${s.correct} из ${s.measured} верно · ${s.unavailable} без данных`));
    return item;
  }));
  const predictions = state.predictions ?? [];
  if (!predictions.length) return;
  const fragment = document.createDocumentFragment();
  predictions.slice().reverse().forEach((p) => {
    const row = element("tr"), s = p.signal;
    row.append(element("td", clock.format(Date.parse(s.time)), "muted"), element("td", s.direction === "вверх" ? "↗ Вверх" : "↘ Вниз", s.direction === "вверх" ? "positive" : "negative"), element("td", signed(s.score * 100, 0)), element("td", money.format(s.mid)));
    [10, 30, 60].forEach((horizon, i) => {
      const result = p.results[i], cell = element("td");
      if (!result) {
        const seconds = Math.max(0, Math.ceil((Date.parse(s.time) + horizon * 1000 - serverNow()) / 1000));
        cell.append(element("span", transport ? seconds ? `Через ${seconds} с` : "Ждём котировку" : "Нет связи", "result waiting"));
      } else if (result.status === "unavailable") {
        cell.append(element("span", "Нет данных", "result missing")); cell.title = result.reason || "Результат недоступен";
      } else {
        cell.append(element("span", `${result.correct ? "✓" : "×"} ${signed(result.move_bps ?? 0)} б.п.`, `result ${result.correct ? "positive" : "negative"}`));
        cell.title = `${result.correct ? "Направление подтвердилось" : "Направление не подтвердилось"}. Цена ${money.format(result.end_mid)} ₽ в ${clock.format(Date.parse(result.time))}`;
      }
      row.append(cell);
    });
    fragment.append(row);
  });
  $("prediction-rows").replaceChildren(fragment);
}

document.querySelectorAll("[data-range]").forEach((button) => button.addEventListener("click", () => {
  range = Number(button.dataset.range);
  document.querySelectorAll("[data-range]").forEach((b) => b.setAttribute("aria-pressed", String(b === button)));
  renderChart();
}));

const source = new EventSource("/api/events");
source.addEventListener("state", (event) => {
  try { state = JSON.parse(event.data); } catch { return; }
  received = performance.now(); transport = true; connectionFailed = false;
  renderBook(); renderChart(); renderTrades(); renderPredictions(); renderConnection();
  text("session-info", `Сеанс с ${clock.format(Date.parse(state.started))} · ${number.format(state.book_count)} стаканов`);
});
source.onerror = () => { transport = false; connectionFailed = true; renderConnection(); };
setInterval(() => { renderConnection(); if (state) renderPredictions(); }, 1000);
