import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ErrorMessage } from "@/components/ErrorMessage";
import { api } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { NEXT_STATUS } from "@/lib/status";
import type { Task } from "@/lib/types";
import { statusBadgeClass, statusLabel } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Plus } from "lucide-react";

interface SubtaskSectionProps {
  parent: Task;
  onChanged: () => void;
}

// SubtaskSection is the only place the web UI creates a subtask: you make one
// from inside its parent, so there is never a parent picker to get wrong.
export function SubtaskSection({ parent, onChanged }: SubtaskSectionProps) {
  const [children, setChildren] = useState<Task[]>([]);
  const [title, setTitle] = useState("");
  const [error, setError] = useState("");

  // Children are fetched through the same ?parent= call path the CLI's
  // --parent uses, rather than a bespoke "task with children" payload. This
  // stays a local fetch (rather than reading off App's own `tasks`) because
  // when the App-level "show subtasks" toggle is off, App's `tasks` never
  // contains the children at all.
  const load = useCallback(() => {
    api.listTasks(parent.id).then(setChildren).catch((err) => setError(errorMessage(err)));
  }, [parent.id]);

  useEffect(() => {
    load();
  }, [load]);

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    if (!title.trim()) return;
    try {
      const created = await api.createTask({
        title,
        description: "",
        priority: "none",
        due_date: null,
        parent_id: parent.id,
      });
      setChildren((cs) => [...cs, created]);
      setTitle("");
      setError("");
      onChanged();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  // Uses the patch response to update the local list in place instead of
  // re-fetching it, so one click only triggers one refetch: onChanged()'s,
  // which keeps the parent's rolled-up counts (shown here and in the list/
  // board views) in sync.
  async function handleAdvance(child: Task) {
    try {
      const updated = await api.patchTask(child.id, { status: NEXT_STATUS[child.status] });
      setChildren((cs) => cs.map((c) => (c.id === updated.id ? updated : c)));
      setError("");
      onChanged();
    } catch (err) {
      setError(errorMessage(err));
    }
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

      <ErrorMessage message={error || null} />
    </div>
  );
}
