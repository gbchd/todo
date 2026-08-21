import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api } from "@/lib/api";
import type { Status, Task } from "@/lib/types";
import { priorityBadgeClass, statusBadgeClass, statusLabel, subtaskProgress } from "@/lib/format";
import { cn } from "@/lib/utils";
import { AlertTriangle, CornerDownRight } from "lucide-react";

const STATUSES: Status[] = ["open", "in-progress", "done"];

interface TaskListViewProps {
  tasks: Task[];
  onSelect: (id: number) => void;
  onMoved: () => void;
}

export function TaskListView({ tasks, onSelect, onMoved }: TaskListViewProps) {
  async function handleStatusChange(id: number, status: Status) {
    await api.patchTask(id, { status });
    onMoved();
  }

  if (tasks.length === 0) {
    return (
      <p className="py-12 text-center text-muted-foreground">
        No tasks yet — add one to get started.
      </p>
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-card shadow-sm">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="w-16 text-muted-foreground">ID</TableHead>
            <TableHead className="w-32 text-muted-foreground">Status</TableHead>
            <TableHead className="w-24 text-muted-foreground">Priority</TableHead>
            <TableHead className="text-muted-foreground">Title</TableHead>
            <TableHead className="w-28 text-muted-foreground">Due</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {tasks.map((t) => (
            <TableRow key={t.id} className="cursor-pointer" onClick={() => onSelect(t.id)}>
              <TableCell className="text-muted-foreground">#{t.id}</TableCell>
              <TableCell onClick={(e) => e.stopPropagation()}>
                <Select value={t.status} onValueChange={(v) => handleStatusChange(t.id, v as Status)}>
                  <SelectTrigger
                    size="sm"
                    className={cn(
                      "h-auto w-fit gap-1 rounded-full border-transparent px-2 py-0.5 text-xs font-medium shadow-none",
                      statusBadgeClass(t.status)
                    )}
                  >
                    <SelectValue>{statusLabel(t.status)}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {STATUSES.map((s) => (
                      <SelectItem key={s} value={s}>
                        {statusLabel(s)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </TableCell>
              <TableCell>
                {t.priority === "none" ? (
                  <span className="text-muted-foreground">–</span>
                ) : (
                  <Badge className={cn("border-transparent capitalize", priorityBadgeClass(t.priority))}>
                    {t.priority}
                  </Badge>
                )}
              </TableCell>
              <TableCell className="font-medium">
                <span className="flex items-center gap-1.5">
                  {t.parent_id !== null && (
                    <CornerDownRight className="size-3.5 shrink-0 text-muted-foreground" />
                  )}
                  <span className={cn(t.status === "done" && "text-muted-foreground line-through")}>
                    {t.title}
                  </span>
                  {subtaskProgress(t) && (
                    <span className="flex items-center gap-1 rounded-full bg-muted px-1.5 py-0.5 text-xs font-normal text-muted-foreground">
                      {subtaskProgress(t)}
                      {/* Subtasks carry their own due dates, so a parent has to
                          surface an overdue one that is hidden beneath it. */}
                      {t.any_child_overdue && (
                        <AlertTriangle className="size-3 text-red-600 dark:text-red-400" />
                      )}
                    </span>
                  )}
                </span>
              </TableCell>
              <TableCell className="text-muted-foreground">{t.due_date ?? "–"}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
