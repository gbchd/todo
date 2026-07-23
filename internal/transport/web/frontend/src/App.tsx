import { useCallback, useEffect, useState } from "react";
import { Navbar, type View } from "@/components/Navbar";
import { TaskListView } from "@/components/TaskListView";
import { TaskBoardView } from "@/components/TaskBoardView";
import { AddTaskModal } from "@/components/AddTaskModal";
import { TaskDetailModal } from "@/components/TaskDetailModal";
import { api } from "@/lib/api";
import type { Task } from "@/lib/types";

export default function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [view, setView] = useState<View>("list");
  const [addOpen, setAddOpen] = useState(false);
  const [detailId, setDetailId] = useState<number | null>(null);

  const refresh = useCallback(() => {
    api.listTasks().then(setTasks);
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const detailTask = tasks.find((t) => t.id === detailId) ?? null;

  return (
    <div className="min-h-screen">
      <Navbar view={view} onViewChange={setView} onAddClick={() => setAddOpen(true)} />

      <main className="mx-auto max-w-7xl p-6">
        {view === "list" ? (
          <TaskListView tasks={tasks} onSelect={setDetailId} onMoved={refresh} />
        ) : (
          <TaskBoardView tasks={tasks} onSelect={setDetailId} onMoved={refresh} />
        )}
      </main>

      <AddTaskModal open={addOpen} onOpenChange={setAddOpen} onCreated={refresh} />
      <TaskDetailModal
        task={detailTask}
        onOpenChange={(open) => !open && setDetailId(null)}
        onChanged={refresh}
      />
    </div>
  );
}
