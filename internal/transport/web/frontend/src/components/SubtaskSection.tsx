import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/lib/api";
import type { Status, Task } from "@/lib/types";
import { statusBadgeClass, statusLabel } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Plus } from "lucide-react";

interface SubtaskSectionProps {
  parent: Task;
  onChanged: () => void;
}

// The next status in the open → in-progress → done → open cycle, matching the
// TUI's space bar and keeping one status model across all three interfaces.
const NEXT_STATUS: Record<Status, Status> = {
  open: "in-progress",
  "in-progress": "done",
  done: "open",
};

// SubtaskSection is the only place the web UI creates a subtask: you make one
// from inside its parent, so there is never a parent picker to get wrong.
export function SubtaskSection({ parent, onChanged }: SubtaskSectionProps) {
  const [children, setChildren] = useState<Task[]>([]);
  const [title, setTitle] = useState("");
  const [error, setError] = useState("");

  // Children are fetched through the same ?parent= call path the CLI's
  // --parent uses, rather than a bespoke "task with children" payload.
  const load = useCallback(() => {
    api.listTasks(parent.id).then(setChildren).catch(() => setChildren([]));
  }, [parent.id]);

  useEffect(() => {
    load();
  }, [load]);

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    if (!title.trim()) return;
    try {
      await api.createTask({
        title,
        description: "",
        priority: "none",
        due_date: null,
        parent_id: parent.id,
      });
      setTitle("");
      setError("");
      load();
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function handleAdvance(child: Task) {
    await api.patchTask(child.id, { status: NEXT_STATUS[child.status] });
    load();
    onChanged();
  }

  return (
    <div className="space-y-2 border-t pt-3">
      <p className="text-sm font-medium">
        Subtasks{" "}
        <span className="text-muted-foreground">
          {parent.done_child_count}/{parent.child_count}
        </span>
      </p>

      <ul className="space-y-1">
        {children.map((c) => (
          <li key={c.id} className="flex items-center gap-2 text-sm">
            <button
              type="button"
              onClick={() => handleAdvance(c)}
              title="Advance status"
              className={cn(
                "rounded-full px-2 py-0.5 text-xs font-medium",
                statusBadgeClass(c.status)
              )}
            >
              {statusLabel(c.status)}
            </button>
            <span
              className={cn(
                "min-w-0 flex-1 truncate",
                c.status === "done" && "text-muted-foreground line-through"
              )}
            >
              {c.title}
            </span>
            {c.due_date && <span className="text-xs text-muted-foreground">{c.due_date}</span>}
          </li>
        ))}
      </ul>

      <form onSubmit={handleAdd} className="flex items-center gap-2">
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Add a subtask"
          aria-label="New subtask title"
        />
        <Button type="submit" variant="outline" size="icon" title="Add subtask">
          <Plus />
        </Button>
      </form>

      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  );
}
