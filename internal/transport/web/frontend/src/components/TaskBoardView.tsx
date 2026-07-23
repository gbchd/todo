import { useState, type DragEvent } from "react";
import type { Status, Task } from "@/lib/types";
import { api } from "@/lib/api";
import { priorityClass } from "@/lib/format";
import { cn } from "@/lib/utils";

const COLUMNS: { status: Status; label: string }[] = [
  { status: "open", label: "Open" },
  { status: "in-progress", label: "In Progress" },
  { status: "done", label: "Done" },
];

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
              "min-h-48 rounded-lg border bg-card p-3 transition-colors",
              dragOver === status && "outline-2 outline-dashed outline-ring"
            )}
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(status);
            }}
            onDragLeave={() => setDragOver(null)}
            onDrop={(e) => handleDrop(status, e)}
          >
            <h2 className="mb-2 text-sm font-medium text-muted-foreground">
              {label} ({columnTasks.length})
            </h2>
            {columnTasks.map((t) => (
              <div
                key={t.id}
                draggable
                onDragStart={(e) => e.dataTransfer.setData("text/plain", String(t.id))}
                onClick={() => onSelect(t.id)}
                className="mb-2 cursor-grab rounded-md border bg-background p-2 active:cursor-grabbing"
              >
                <div className="font-medium">
                  #{t.id} {t.title}
                </div>
                <div className={cn("text-xs", priorityClass(t.priority))}>
                  {t.priority}
                  {t.due_date ? ` · due ${t.due_date}` : ""}
                </div>
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}
