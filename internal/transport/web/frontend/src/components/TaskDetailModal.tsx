import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { TaskFormFields } from "@/components/TaskFormFields";
import { ErrorMessage } from "@/components/ErrorMessage";
import { SubtaskSection } from "@/components/SubtaskSection";
import { api } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import type { Priority, Task } from "@/lib/types";
import { cn } from "@/lib/utils";
import { priorityBadgeClass, statusBadgeClass, statusLabel } from "@/lib/format";

interface TaskDetailModalProps {
  task: Task | null;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
}

// Jira-style: read view first; Edit switches this same modal into a form;
// Cancel reverts to the read view without closing.
export function TaskDetailModal({ task, onOpenChange, onChanged }: TaskDetailModalProps) {
  return (
    <Dialog open={!!task} onOpenChange={onOpenChange}>
      <DialogContent>
        {task && (
          <TaskDetailBody
            key={task.id}
            task={task}
            onOpenChange={onOpenChange}
            onChanged={onChanged}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

interface TaskDetailBodyProps {
  task: Task;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
}

// Mounted fresh (via the parent's key={task.id}) each time the selected task
// changes, so all form state below can simply be initialised from props —
// no effect is needed to resync it when `task` changes.
function TaskDetailBody({ task, onOpenChange, onChanged }: TaskDetailBodyProps) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description);
  const [priority, setPriority] = useState<Priority>(task.priority);
  const [dueDate, setDueDate] = useState(task.due_date ?? "");
  const [error, setError] = useState("");

  async function handleSave() {
    try {
      await api.patchTask(task.id, { title, description, priority, due_date: dueDate || null });
      setEditing(false);
      onChanged();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  async function handleDelete() {
    if (!confirm(`Delete task #${task.id} "${task.title}"?`)) return;
    try {
      await api.deleteTask(task.id);
      onOpenChange(false);
      onChanged();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  if (editing) {
    return (
      <>
        <DialogHeader>
          <DialogTitle>Edit #{task.id}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <TaskFormFields
            idPrefix="edit"
            title={title}
            onTitleChange={setTitle}
            description={description}
            onDescriptionChange={setDescription}
            priority={priority}
            onPriorityChange={setPriority}
            dueDate={dueDate}
            onDueDateChange={setDueDate}
          />
          <ErrorMessage message={error || null} />
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => setEditing(false)}>
            Cancel
          </Button>
          <Button type="button" onClick={handleSave}>
            Save
          </Button>
        </DialogFooter>
      </>
    );
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>
          #{task.id} {task.title}
        </DialogTitle>
      </DialogHeader>
      <div className="space-y-3 text-sm">
        <div className="flex items-center gap-2">
          <Badge className={cn("border-transparent font-medium", statusBadgeClass(task.status))}>
            {statusLabel(task.status)}
          </Badge>
          {task.priority !== "none" && (
            <Badge className={cn("border-transparent capitalize", priorityBadgeClass(task.priority))}>
              {task.priority}
            </Badge>
          )}
        </div>
        <p>
          <span className="text-muted-foreground">Due: </span>
          {task.due_date ?? "–"}
        </p>
        {task.parent_id !== null && (
          <p className="text-muted-foreground">Subtask of #{task.parent_id}</p>
        )}
        <p className="whitespace-pre-wrap border-t pt-3">
          {task.description || "(no description)"}
        </p>
        {/* A subtask is one level deep, so it never gets a subtask list
            of its own. */}
        {task.parent_id === null && <SubtaskSection parent={task} onChanged={onChanged} />}
        <ErrorMessage message={error || null} />
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          Close
        </Button>
        <Button type="button" variant="destructive" onClick={handleDelete}>
          Delete
        </Button>
        <Button type="button" onClick={() => setEditing(true)}>
          Edit
        </Button>
      </DialogFooter>
    </>
  );
}
