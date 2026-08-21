import { useState, type DragEvent } from "react";
import type { Status, Task } from "@/lib/types";
import { api } from "@/lib/api";
import { priorityBadgeClass, subtaskProgress } from "@/lib/format";
import { cn } from "@/lib/utils";

const COLUMNS: { status: Status; label: string }[] = [
  { status: "open", label: "Open" },
  { status: "in-progress", label: "In Progress" },
  { status: "done", label: "Done" },
];

const PRIORITY_BAR_CLASS: Record<Task["priority"], string> = {
  high: "bg-red-500",
  medium: "bg-amber-500",
  low: "bg-sky-500",
  none: "bg-transparent",
};

interface TaskBoardViewProps {
  tasks: Task[];
  onSelect: (id: number) => void;
  onMoved: () => void;
}

export function TaskBoardView({ tasks, onSelect, onMoved }: TaskBoardViewProps) {
  const [dragOver, setDragOver] = useState<Status | null>(null);

  async function handleDrop(status: Status, e: DragEvent) {
    e.preventDefault();
    setDragOver(null);
    const id = Number(e.dataTransfer.getData("text/plain"));
    if (!id) return;
    await api.patchTask(id, { status });
    onMoved();
  }

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
      {COLUMNS.map(({ status, label }) => {
        const columnTasks = tasks.filter((t) => t.status === status);
        return (
          <div
            key={status}
            className={cn(
              "min-h-48 rounded-lg border bg-muted/40 p-3 transition-colors",
              dragOver === status && "border-primary/50 bg-primary/5 outline-2 outline-dashed outline-primary/40"
            )}
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(status);
            }}
            onDragLeave={() => setDragOver(null)}
            onDrop={(e) => handleDrop(status, e)}
          >
            <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-foreground">
              {label}
              <span className="rounded-full bg-background px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
                {columnTasks.length}
              </span>
            </h2>
            <div className="space-y-2">
              {columnTasks.map((t) => (
                <div
                  key={t.id}
                  draggable
                  onDragStart={(e) => e.dataTransfer.setData("text/plain", String(t.id))}
                  onClick={() => onSelect(t.id)}
                  className="flex cursor-grab overflow-hidden rounded-md border bg-card shadow-sm transition-shadow hover:shadow-md active:cursor-grabbing"
                >
                  <div className={cn("w-1 shrink-0", PRIORITY_BAR_CLASS[t.priority])} />
                  <div className="min-w-0 flex-1 p-2.5">
                    <div
                      className={cn(
                        "font-medium",
                        status === "done" && "text-muted-foreground line-through"
                      )}
                    >
                      <span className="text-muted-foreground">#{t.id}</span> {t.title}
                    </div>
                    {(t.priority !== "none" || t.due_date || subtaskProgress(t)) && (
                      <div className="mt-1.5 flex items-center gap-2 text-xs">
                        {t.priority !== "none" && (
                          <span className={cn("rounded-full px-1.5 py-0.5 capitalize", priorityBadgeClass(t.priority))}>
                            {t.priority}
                          </span>
                        )}
                        {t.due_date && <span className="text-muted-foreground">due {t.due_date}</span>}
                        {subtaskProgress(t) && (
                          <span
                            className={cn(
                              "rounded-full bg-muted px-1.5 py-0.5 text-muted-foreground",
                              t.any_child_overdue && "text-red-600 dark:text-red-400"
                            )}
                          >
                            {subtaskProgress(t)}
                            {t.any_child_overdue && " !"}
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}
