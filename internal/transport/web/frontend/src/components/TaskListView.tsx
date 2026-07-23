import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Task } from "@/lib/types";
import { priorityClass, statusLabel } from "@/lib/format";

interface TaskListViewProps {
  tasks: Task[];
  onSelect: (id: number) => void;
}

export function TaskListView({ tasks, onSelect }: TaskListViewProps) {
  if (tasks.length === 0) {
    return (
      <p className="py-12 text-center text-muted-foreground">
        No tasks yet — add one to get started.
      </p>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>ID</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Priority</TableHead>
          <TableHead>Title</TableHead>
          <TableHead>Due</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {tasks.map((t) => (
          <TableRow key={t.id} className="cursor-pointer" onClick={() => onSelect(t.id)}>
            <TableCell>#{t.id}</TableCell>
            <TableCell>{statusLabel(t.status)}</TableCell>
            <TableCell className={priorityClass(t.priority)}>{t.priority}</TableCell>
            <TableCell>{t.title}</TableCell>
            <TableCell>{t.due_date ?? "-"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
