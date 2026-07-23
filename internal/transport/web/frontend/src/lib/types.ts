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
  created_at: string;
  updated_at: string;
  completed_at: string | null;
}

// Mirrors internal/transport/web/dto.go's createRequest.
export interface CreateTaskInput {
  title: string;
  description: string;
  priority: Priority;
  due_date: string | null;
}

// Mirrors the partial-patch body internal/transport/web/handlers.go's
// patchTask accepts: any subset present, absent = untouched.
export interface TaskPatch {
  title?: string;
  description?: string;
  priority?: Priority;
  due_date?: string | null;
  status?: Status;
}
