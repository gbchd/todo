export type Status = "open" | "in-progress" | "done";
export type Priority = "none" | "low" | "medium" | "high";

// Mirrors internal/transport/web/dto.go's taskDTO.
export interface Task {
  id: number;
  title: string;
  description: string;
  status: Status;
  priority: Priority;
  due_date: string | null;
  parent_id: number | null;
  created_at: string;
  updated_at: string;
  completed_at: string | null;

  // Derived, read-only: a parent task's rolled-up view of its subtasks.
  child_count: number;
  done_child_count: number;
  any_child_overdue: boolean;
}

// Mirrors internal/transport/web/dto.go's createRequest.
export interface CreateTaskInput {
  title: string;
  description: string;
  priority: Priority;
  due_date: string | null;
  parent_id?: number;
}

// Mirrors the partial-patch body internal/transport/web/handlers.go's
// patchTask accepts: any subset present, absent = untouched.
export interface TaskPatch {
  title?: string;
  description?: string;
  priority?: Priority;
  due_date?: string | null;
  // null promotes a subtask back to top level; absent leaves it untouched.
  parent_id?: number | null;
  status?: Status;
}
