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
import type { Priority } from "@/lib/types";

interface TaskFormFieldsProps {
  idPrefix: string;
  title: string;
  onTitleChange: (title: string) => void;
  titleRequired?: boolean;
  description: string;
  onDescriptionChange: (description: string) => void;
  priority: Priority;
  onPriorityChange: (priority: Priority) => void;
  dueDate: string;
  onDueDateChange: (dueDate: string) => void;
}

// The Title / Description / Priority / Due date fields shared by AddTaskModal
// and TaskDetailModal's edit form. Purely presentational: the caller owns the
// values, the change handlers, and how (or whether) the fields are wrapped in
// a <form>.
export function TaskFormFields({
  idPrefix,
  title,
  onTitleChange,
  titleRequired,
  description,
  onDescriptionChange,
  priority,
  onPriorityChange,
  dueDate,
  onDueDateChange,
}: TaskFormFieldsProps) {
  return (
    <>
      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-title`}>Title</Label>
        <Input
          id={`${idPrefix}-title`}
          value={title}
          onChange={(e) => onTitleChange(e.target.value)}
          required={titleRequired}
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-description`}>Description</Label>
        <Textarea
          id={`${idPrefix}-description`}
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
        />
      </div>
      <div className="space-y-1.5">
        <Label>Priority</Label>
        <Select value={priority} onValueChange={(v) => onPriorityChange(v as Priority)}>
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
        <Label htmlFor={`${idPrefix}-due`}>Due date</Label>
        <Input
          id={`${idPrefix}-due`}
          type="date"
          value={dueDate}
          onChange={(e) => onDueDateChange(e.target.value)}
        />
      </div>
    </>
  );
}
