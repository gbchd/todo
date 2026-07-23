"use strict";

const state = {
  tasks: [],
  view: "list",
  detailTaskId: null,
  detailEditing: false,
};

const el = (id) => document.getElementById(id);

async function api(path, options) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (res.status === 204) return null;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(body.error || `request failed (${res.status})`);
  }
  return body;
}

async function fetchTasks() {
  state.tasks = await api("/api/tasks");
  render();
}

function statusLabel(s) {
  return { open: "Open", "in-progress": "In Progress", done: "Done" }[s] || s;
}

function render() {
  el("list-view").hidden = state.view !== "list";
  el("board-view").hidden = state.view !== "board";
  document.querySelectorAll(".view-toggle button").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.view === state.view);
  });
  if (state.view === "list") renderList();
  else renderBoard();
}

function renderList() {
  const container = el("list-view");
  if (state.tasks.length === 0) {
    container.innerHTML = `<div class="empty-state">No tasks yet — add one to get started.</div>`;
    return;
  }
  const rows = state.tasks
    .map(
      (t) => `
      <tr data-id="${t.id}">
        <td>#${t.id}</td>
        <td>${statusLabel(t.status)}</td>
        <td class="priority-${t.priority}">${t.priority}</td>
        <td>${escapeHtml(t.title)}</td>
        <td>${t.due_date || "-"}</td>
      </tr>`
    )
    .join("");
  container.innerHTML = `
    <table>
      <thead><tr><th>ID</th><th>Status</th><th>Priority</th><th>Title</th><th>Due</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>`;
  container.querySelectorAll("tbody tr").forEach((row) => {
    row.addEventListener("click", () => openDetail(Number(row.dataset.id)));
  });
}

function renderBoard() {
  const columns = { open: [], "in-progress": [], done: [] };
  for (const t of state.tasks) {
    (columns[t.status] || columns.open).push(t);
  }
  for (const status of Object.keys(columns)) {
    const target = document.querySelector(`.cards[data-status="${status}"]`);
    target.innerHTML = columns[status]
      .map(
        (t) => `
        <div class="card" draggable="true" data-id="${t.id}">
          <div class="card-title">#${t.id} ${escapeHtml(t.title)}</div>
          <div class="card-meta priority-${t.priority}">${t.priority} ${t.due_date ? "· due " + t.due_date : ""}</div>
        </div>`
      )
      .join("");
    target.querySelectorAll(".card").forEach((card) => {
      card.addEventListener("click", () => openDetail(Number(card.dataset.id)));
      card.addEventListener("dragstart", (e) => {
        e.dataTransfer.setData("text/plain", card.dataset.id);
      });
    });
  }
}

document.querySelectorAll(".column").forEach((column) => {
  column.addEventListener("dragover", (e) => {
    e.preventDefault();
    column.classList.add("drag-over");
  });
  column.addEventListener("dragleave", () => column.classList.remove("drag-over"));
  column.addEventListener("drop", async (e) => {
    e.preventDefault();
    column.classList.remove("drag-over");
    const id = e.dataTransfer.getData("text/plain");
    const status = column.dataset.status;
    await api(`/api/tasks/${id}`, { method: "PATCH", body: JSON.stringify({ status }) });
    await fetchTasks();
  });
});

document.querySelectorAll(".view-toggle button").forEach((btn) => {
  btn.addEventListener("click", () => {
    state.view = btn.dataset.view;
    render();
  });
});

// --- Add task modal ---

el("add-task-btn").addEventListener("click", () => {
  el("add-form").reset();
  el("add-error").hidden = true;
  el("add-backdrop").hidden = false;
});
el("add-cancel").addEventListener("click", () => {
  el("add-backdrop").hidden = true;
});
el("add-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const form = new FormData(e.target);
  const payload = {
    title: form.get("title"),
    description: form.get("description") || "",
    priority: form.get("priority") || "none",
    due_date: form.get("due_date") || null,
  };
  try {
    await api("/api/tasks", { method: "POST", body: JSON.stringify(payload) });
    el("add-backdrop").hidden = true;
    await fetchTasks();
  } catch (err) {
    el("add-error").textContent = err.message;
    el("add-error").hidden = false;
  }
});

// --- Detail / edit modal (Jira-style: read view, Edit toggles to a form) ---

function findTask(id) {
  return state.tasks.find((t) => t.id === id);
}

function openDetail(id) {
  state.detailTaskId = id;
  state.detailEditing = false;
  el("detail-backdrop").hidden = false;
  renderDetail();
}

function closeDetail() {
  el("detail-backdrop").hidden = true;
  state.detailTaskId = null;
  state.detailEditing = false;
}

function renderDetail() {
  const t = findTask(state.detailTaskId);
  const modal = el("detail-modal");
  if (!t) {
    closeDetail();
    return;
  }
  modal.innerHTML = state.detailEditing ? detailEditHTML(t) : detailReadHTML(t);
  wireDetailEvents(t);
}

function detailReadHTML(t) {
  return `
    <h2>#${t.id} ${escapeHtml(t.title)}</h2>
    <div class="detail-row"><span class="label">Status</span><div>${statusLabel(t.status)}</div></div>
    <div class="detail-row"><span class="label">Priority</span><div class="priority-${t.priority}">${t.priority}</div></div>
    <div class="detail-row"><span class="label">Due</span><div>${t.due_date || "-"}</div></div>
    <div class="detail-desc">${t.description ? escapeHtml(t.description) : "(no description)"}</div>
    <div class="modal-actions">
      <button type="button" data-action="close">Close</button>
      <button type="button" class="danger" data-action="delete">Delete</button>
      <button type="button" class="primary" data-action="edit">Edit</button>
    </div>`;
}

function detailEditHTML(t) {
  const opt = (v, label) => `<option value="${v}" ${t.priority === v ? "selected" : ""}>${label}</option>`;
  return `
    <h2>Edit #${t.id}</h2>
    <div class="field"><label>Title</label><input name="title" value="${escapeAttr(t.title)}"></div>
    <div class="field"><label>Description</label><textarea name="description">${escapeHtml(t.description || "")}</textarea></div>
    <div class="field"><label>Priority</label>
      <select name="priority">
        ${opt("none", "None")}${opt("low", "Low")}${opt("medium", "Medium")}${opt("high", "High")}
      </select>
    </div>
    <div class="field"><label>Due date</label><input type="date" name="due_date" value="${t.due_date || ""}"></div>
    <div class="error-msg" data-role="error" hidden></div>
    <div class="modal-actions">
      <button type="button" data-action="cancel">Cancel</button>
      <button type="button" class="primary" data-action="save">Save</button>
    </div>`;
}

function wireDetailEvents(t) {
  const modal = el("detail-modal");
  modal.querySelector('[data-action="close"]')?.addEventListener("click", closeDetail);
  modal.querySelector('[data-action="edit"]')?.addEventListener("click", () => {
    state.detailEditing = true;
    renderDetail();
  });
  modal.querySelector('[data-action="cancel"]')?.addEventListener("click", () => {
    state.detailEditing = false;
    renderDetail();
  });
  modal.querySelector('[data-action="delete"]')?.addEventListener("click", async () => {
    if (!confirm(`Delete task #${t.id} "${t.title}"?`)) return;
    await api(`/api/tasks/${t.id}`, { method: "DELETE" });
    closeDetail();
    await fetchTasks();
  });
  modal.querySelector('[data-action="save"]')?.addEventListener("click", async () => {
    const title = modal.querySelector('[name="title"]').value;
    const description = modal.querySelector('[name="description"]').value;
    const priority = modal.querySelector('[name="priority"]').value;
    const due = modal.querySelector('[name="due_date"]').value;
    try {
      await api(`/api/tasks/${t.id}`, {
        method: "PATCH",
        body: JSON.stringify({ title, description, priority, due_date: due || null }),
      });
      state.detailEditing = false;
      await fetchTasks();
      renderDetail();
    } catch (err) {
      const errEl = modal.querySelector('[data-role="error"]');
      errEl.textContent = err.message;
      errEl.hidden = false;
    }
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
function escapeAttr(s) {
  return escapeHtml(s);
}

fetchTasks();
