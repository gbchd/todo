import type { CreateTaskInput, Task, TaskPatch } from "./types";

class ApiError extends Error {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (res.status === 204) return undefined as T;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(body.error || `request failed (${res.status})`);
  }
  return body as T;
}

export const api = {
  // parent: "none" for top-level tasks only, an id for that task's subtasks,
  // omitted for everything at once.
  listTasks: (parent?: number | "none") =>
    request<Task[]>(parent === undefined ? "/api/tasks" : `/api/tasks?parent=${parent}`),

  createTask: (input: CreateTaskInput) =>
    request<Task>("/api/tasks", { method: "POST", body: JSON.stringify(input) }),

  patchTask: (id: number, patch: TaskPatch) =>
    request<Task>(`/api/tasks/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),

  deleteTask: (id: number) => request<void>(`/api/tasks/${id}`, { method: "DELETE" }),
};
