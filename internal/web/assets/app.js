// The token arrives in the URL on the first visit and is stripped from the
// address bar at once, so it never reaches history, a bookmark or a screenshot.
// The server also set a cookie while serving this page, which is what makes a
// reload work. Requests below still send the token in a header, and the thumbnail
// sends it in its URL, because the cookie alone cannot tell one local port from
// another.
const token = new URLSearchParams(location.search).get("t") || "";
history.replaceState(null, "", location.pathname);

const el = (id) => document.getElementById(id);
const selected = new Set();
let files = [];
let target = null;
let formatsRendered = false;

async function api(path, options = {}) {
  const res = await fetch(path, {
    ...options,
    headers: { ...(options.headers || {}), "X-Imgconv-Token": token },
  });
  if (!res.ok) throw new Error((await res.text()).trim() || `HTTP ${res.status}`);
  return res;
}

// The three states a data panel always has. Leaving any of them unhandled is how
// "it is still loading" and "there is nothing here" become the same blank box.
function showState(which, message) {
  for (const id of ["state-loading", "state-empty", "state-error"]) {
    el(id).hidden = id !== which;
  }
  el("files").hidden = which !== null;
  if (which === "state-error") el("state-error").textContent = message;
}

function humanSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function renderFiles(list) {
  const ul = el("files");
  ul.replaceChildren();

  for (const f of list) {
    const li = document.createElement("li");
    const wrap = document.createElement("div");
    wrap.className = "rowwrap";

    const row = document.createElement("button");
    row.type = "button";
    row.className = "row";
    row.setAttribute("aria-pressed", String(selected.has(f.id)));

    const img = document.createElement("img");
    img.className = "thumb";
    img.loading = "lazy";
    img.alt = "";
    // The token IS in this URL, on purpose. An <img> cannot set a header, and the
    // cookie alone was not enough: a cookie belongs to a host and every port on
    // 127.0.0.1 shares it, so a page from another local server could embed this
    // same URL and be served. Sec-Fetch-Site closes that too; this is the half
    // that does not depend on the browser sending a header.
    img.src = `/api/thumb?f=${encodeURIComponent(f.id)}&t=${encodeURIComponent(token)}`;

    const label = document.createElement("span");
    label.className = "label";

    const name = document.createElement("span");
    name.className = "name";
    name.textContent = f.name;

    // Format first, because it is the one that can contradict the file name: this
    // program decides by bytes, so a .jpeg holding a PNG says PNG here and that
    // is the only place the operator sees it before anything is written.
    const meta = document.createElement("span");
    meta.className = "meta";
    const bits = [];
    if (f.format) bits.push(f.format.toUpperCase());
    if (f.width && f.height) bits.push(`${f.width} × ${f.height}`);
    bits.push(humanSize(f.size));
    if (f.width && f.height) {
      const mp = (f.width * f.height) / 1e6;
      if (mp >= 0.1) bits.push(`${mp.toFixed(1)} MP`);
    }
    meta.textContent = bits.join("  ·  ");
    if (!f.format) {
      meta.classList.add("meta-unknown");
      meta.textContent = `nao reconhecido  ·  ${humanSize(f.size)}`;
      meta.title = "os bytes deste arquivo nao sao de uma imagem que este build conhece";
    }

    label.append(name, meta);

    const tick = document.createElement("span");
    tick.className = "tick";
    tick.setAttribute("aria-hidden", "true");
    tick.textContent = "\u2713";

    row.append(img, label, tick);
    row.addEventListener("click", () => toggle(f.id, row));

    const drop = document.createElement("button");
    drop.type = "button";
    drop.className = "drop";
    drop.title = `tirar ${f.name} da lista`;
    drop.setAttribute("aria-label", drop.title);
    drop.textContent = "\u00D7";
    drop.addEventListener("click", () => remove(f.id));

    wrap.append(row, drop);
    li.append(wrap);
    ul.append(li);
  }

  el("select-all").disabled = list.length === 0;
}

function toggle(id, row) {
  const on = !selected.has(id);
  on ? selected.add(id) : selected.delete(id);
  row.setAttribute("aria-pressed", String(on));
  syncSelection();
}

// Taking a file off the list is not deleting it. The server forgets it; the disk
// is untouched, and saying so in the label is cheaper than reassuring afterwards.
async function remove(id) {
  await api("/api/remove", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id }),
  });
  selected.delete(id);
  await load();
  syncSelection();
}

function syncSelection() {
  const n = selected.size;
  el("selection").textContent =
    n === 0 ? "Nenhum arquivo selecionado."
    : n === 1 ? "1 arquivo selecionado."
    : `${n} arquivos selecionados.`;
  el("convert").disabled = n === 0 || target === null;
  el("select-all").textContent = n === files.length && n > 0 ? "Limpar selecao" : "Selecionar todas";
}

function selectAll() {
  const rows = [...document.querySelectorAll(".row")];
  const clearing = selected.size === files.length && files.length > 0;
  selected.clear();
  rows.forEach((row, i) => {
    if (!clearing) selected.add(files[i].id);
    row.setAttribute("aria-pressed", String(!clearing));
  });
  syncSelection();
}

// The format list comes from the server, which reads the registry. A list written
// out here would be a second source, and it would go stale the first time a row
// is added there. Decode-only formats never arrive, so they can never be offered.
function renderFormats(formats) {
  const box = el("formats");
  box.replaceChildren();
  formats.forEach((f, i) => {
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = "chip";
    chip.setAttribute("role", "radio");
    chip.setAttribute("aria-checked", "false");
    // The chip shows the EXTENSION, not the format's internal name: somebody
    // looking for "jpg" should not have to know that the format is called jpeg.
    chip.textContent = f.ext.replace(".", "").toUpperCase();
    chip.title = `${f.name} — escreve ${f.ext}`;
    chip.addEventListener("click", () => pickFormat(f.name, chip));
    box.append(chip);
    if (i === 0) pickFormat(f.name, chip);
  });
}

function pickFormat(name, chip) {
  target = name;
  for (const c of document.querySelectorAll(".chip")) c.setAttribute("aria-checked", "false");
  chip.setAttribute("aria-checked", "true");
  // Quality is a JPEG encoder setting and nothing else. Showing it for another
  // target would offer a control that does nothing, which the CLI treats as a
  // usage error rather than a silent no-op.
  el("quality-wrap").hidden = name !== "jpeg";
  syncSelection();
}

async function load() {
  showState("state-loading");
  try {
    const data = await (await api("/api/files")).json();
    // Only the folder's own name. The absolute path tells the operator nothing
    // they do not already know and reads as the tool rummaging through the disk.
    if (!formatsRendered) { renderFormats(data.formats); formatsRendered = true; }
    files = data.files;
    renderFiles(files);
    showState(files.length ? null : "state-empty");
    // The button's job changes once there is a list: the second press is "add
    // more", which is how a batch actually gets built.
    el("pick-label").textContent = files.length ? "Adicionar arquivos" : "Selecionar arquivos";
  } catch (err) {
    showState("state-error", `Nao consegui ler a pasta: ${err.message}`);
  }
}

async function convert() {
  const button = el("convert");
  button.disabled = true;
  const label = button.textContent;
  button.textContent = "Convertendo...";
  try {
    const data = await (await api("/api/convert", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        files: [...selected],
        format: target,
        quality: target === "jpeg" ? Number(el("quality").value) : 0,
        width: Number(el("width").value) || 0,
        height: Number(el("height").value) || 0,
        force: el("force").checked,
      }),
    })).json();
    renderResults(data.results);
    selected.clear();
    await load();
    syncSelection();
  } catch (err) {
    renderResults([{ input: "a requisicao", error: err.message }]);
  } finally {
    button.textContent = label;
    syncSelection();
  }
}

// Every input is accounted for, including the ones that failed. A result list
// that quietly omits a failure is the defect this whole program is built against.
function renderResults(results) {
  const ul = el("results");
  ul.replaceChildren();
  for (const r of results) {
    const li = document.createElement("li");
    const head = document.createElement("span");
    const detail = document.createElement("span");
    detail.className = "r-detail";

    if (r.error) {
      li.className = "r-err";
      head.textContent = r.input;
      // The server reports WHAT failed; naming the control that fixes it is this
      // page's job, because the control only exists here.
      detail.textContent = r.exists
        ? `${r.error} — marque "Substituir arquivo existente" e converta de novo`
        : r.error;
    } else {
      li.className = r.warning ? "r-warn" : "r-ok";
      head.textContent = `${r.input} → ${r.output}`;
      detail.textContent = r.warning || "";
    }
    li.append(head);
    if (detail.textContent) li.append(detail);
    ul.append(li);
  }
  el("results-section").hidden = false;
}

el("quality").addEventListener("input", (e) => { el("quality-out").textContent = e.target.value; });
el("convert").addEventListener("click", convert);
// Clearing the results is a screen action and nothing more: no file is touched,
// which is why it does not ask for confirmation.
// Stopping from the page, because closing the window only works when there IS a
// window: started from a file manager there is no terminal to close, and without
// this the server would keep listening with nothing on screen to stop it.
el("quit").addEventListener("click", async () => {
  try {
    await api("/api/quit", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
  } catch {
    // The connection dropping IS the server stopping. Either way it is gone.
  }
  el("stopped").hidden = false;
});

el("clear-results").addEventListener("click", () => {
  el("results").replaceChildren();
  el("results-section").hidden = true;
});
el("select-all").addEventListener("click", selectAll);
// The dialog is the operating system's, opened by the server. The browser's own
// file input would hand us bytes and hide the path, which is the one thing this
// tool cannot work without.
async function pick() {
  const buttons = [el("pick-files")];
  buttons.forEach((b) => (b.disabled = true));
  const note = el("pick-note");
  note.hidden = true;
  try {
    const data = await (await api("/api/pick", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    })).json();
    if (!data.changed) return; // cancelled; say nothing, because nothing happened
    await load();
    // Everything chosen starts ticked: choosing a file in a dialog IS choosing it.
    for (const f of files) selected.add(f.id);
    for (const row of document.querySelectorAll(".row")) row.setAttribute("aria-pressed", "true");
    syncSelection();
  } catch (err) {
    note.textContent = err.message;
    note.hidden = false;
  } finally {
    buttons.forEach((b) => (b.disabled = false));
  }
}

el("pick-files").addEventListener("click", pick);
load();
