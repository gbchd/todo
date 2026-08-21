import { useState, type FormEvent } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { TaskFormFields } from "@/components/TaskFormFields";
import { ErrorMessage } from "@/components/ErrorMessage";
import { api } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import type { Priority } from "@/lib/types";

interface AddTaskModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}

export function AddTaskModal({ open, onOpenChange, onCreated }: AddTaskModalProps) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState<Priority>("none");
  const [dueDate, setDueDate] = useState("");
  const [error, setError] = useState("");

  function reset() {
    setTitle("");
    setDescription("");
    setPriority("none");
    setDueDate("");
    setError("");
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    try {
      await api.createTask({ title, description, priority, due_date: dueDate || null });
      reset();
      onOpenChange(false);
      onCreated();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) reset();
        onOpenChange(o);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add task</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <TaskFormFields
            idPrefix="add"
            title={title}
            onTitleChange={setTitle}
            titleRequired
            description={description}
            onDescriptionChange={setDescription}
            priority={priority}
            onPriorityChange={setPriority}
            dueDate={dueDate}
            onDueDateChange={setDueDate}
          />
          <ErrorMessage message={error || null} />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit">Add</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
