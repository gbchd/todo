import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api } from "@/lib/api";
import type { Priority, Task } from "@/lib/types";
import { priorityClass, statusLabel } from "@/lib/format";

interface TaskDetailModalProps {
  task: Task | null;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
}

// Jira-style: read view first; Edit switches this same modal into a form;
// Cancel reverts to the read view without closing.
export function TaskDetailModal({ task, onOpenChange, onChanged }: TaskDetailModalProps) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState<Priority>("none");
  const [dueDate, setDueDate] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!task) return;
    setEditing(false);
    setTitle(task.title);
    setDescription(task.description);
    setPriority(task.priority);
    setDueDate(task.due_date ?? "");
    setError("");
  }, [task]);

  if (!task) return null;

  async function handleSave() {
    try {
      await api.patchTask(task!.id, { title, description, priority, due_date: dueDate || null });
      setEditing(false);
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function handleDelete() {
    if (!confirm(`Delete task #${task!.id} "${task!.title}"?`)) return;
    await api.deleteTask(task!.id);
    onOpenChange(false);
    onChanged();
  }

  return (
    <Dialog open={!!task} onOpenChange={onOpenChange}>
      <DialogContent>
        {editing ? (
          <>
            <DialogHeader>
              <DialogTitle>Edit #{task.id}</DialogTitle>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="edit-title">Title</Label>
                <Input id="edit-title" value={title} onChange={(e) => setTitle(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-description">Description</Label>
                <Textarea
                  id="edit-description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label>Priority</Label>
                <Select value={priority} onValueChange={(v) => setPriority(v as Priority)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">None</SelectItem>
                    <SelectItem value="low">Low</SelectItem>
                    <SelectItem value="medium">Medium</SelectItem>
                    <SelectItem value="high">High</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-due">Due date</Label>
                <Input
                  id="edit-due"
                  type="date"
                  value={dueDate}
                  onChange={(e) => setDueDate(e.target.value)}
                />
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
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
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>
                #{task.id} {task.title}
              </DialogTitle>
            </DialogHeader>
            <div className="space-y-2 text-sm">
              <p>
                <span className="text-muted-foreground">Status: </span>
                {statusLabel(task.status)}
              </p>
              <p>
                <span className="text-muted-foreground">Priority: </span>
                <span className={priorityClass(task.priority)}>{task.priority}</span>
              </p>
              <p>
                <span className="text-muted-foreground">Due: </span>
                {task.due_date ?? "-"}
              </p>
              <p className="whitespace-pre-wrap pt-2">{task.description || "(no description)"}</p>
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
        )}
      </DialogContent>
    </Dialog>
  );
}
