import { useCallback, useEffect, useState } from "react";
import { Navbar, type View } from "@/components/Navbar";
import { TaskListView } from "@/components/TaskListView";
import { TaskBoardView } from "@/components/TaskBoardView";
import { AddTaskModal } from "@/components/AddTaskModal";
import { TaskDetailModal } from "@/components/TaskDetailModal";
import { ErrorMessage } from "@/components/ErrorMessage";
import { api } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import type { Task } from "@/lib/types";

export default function App() {
  // null = the list has never successfully loaded yet, which is distinct
  // from a load having completed with zero tasks — see `loadError` below.
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [view, setView] = useState<View>("list");
  const [addOpen, setAddOpen] = useState(false);
  const [detailId, setDetailId] = useState<number | null>(null);
  // Subtasks are hidden by default in all three interfaces; this is the web's
  // reveal toggle. Hidden subtasks still reach their parent's row as a
  // rolled-up count, which is why it filters server-side rather than here.
  const [showSubtasks, setShowSubtasks] = useState(false);

  const refresh = useCallback(() => {
    api
      .listTasks(showSubtasks ? undefined : "none")
      .then((ts) => {
        setTasks(ts);
        setLoadError(null);
      })
      .catch((err) => setLoadError(errorMessage(err)));
  }, [showSubtasks]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const detailTask = tasks?.find((t) => t.id === detailId) ?? null;

  return (
    <div className="min-h-screen">
      <Navbar
        view={view}
        onViewChange={setView}
        onAddClick={() => setAddOpen(true)}
        showSubtasks={showSubtasks}
        onShowSubtasksChange={setShowSubtasks}
      />

      <main className="mx-auto max-w-7xl space-y-4 p-6">
        <ErrorMessage message={loadError} />
        {view === "list" ? (
          <TaskListView
            tasks={tasks ?? []}
            loaded={tasks !== null}
            onSelect={setDetailId}
            onMoved={refresh}
          />
        ) : (
          <TaskBoardView tasks={tasks ?? []} onSelect={setDetailId} onMoved={refresh} />
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
