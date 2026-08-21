import { Button } from "@/components/ui/button";
import { ThemeToggle } from "@/components/ThemeToggle";
import { CheckCircle2, ListTree, Plus } from "lucide-react";

export type View = "list" | "board";

interface NavbarProps {
  view: View;
  onViewChange: (view: View) => void;
  onAddClick: () => void;
  showSubtasks: boolean;
  onShowSubtasksChange: (show: boolean) => void;
}

export function Navbar({
  view,
  onViewChange,
  onAddClick,
  showSubtasks,
  onShowSubtasksChange,
}: NavbarProps) {
  return (
    <nav className="sticky top-0 z-10 flex items-center gap-4 border-b bg-background/80 px-5 py-3 backdrop-blur-sm">
      <span className="mr-auto flex items-center gap-1.5 text-lg font-semibold">
        <CheckCircle2 className="size-5 text-primary" />
        todo
      </span>

      <div className="inline-flex rounded-md border p-0.5" role="tablist">
        <Button
          type="button"
          variant={view === "list" ? "default" : "ghost"}
          size="sm"
          className="rounded-sm"
          onClick={() => onViewChange("list")}
        >
          List
        </Button>
        <Button
          type="button"
          variant={view === "board" ? "default" : "ghost"}
          size="sm"
          className="rounded-sm"
          onClick={() => onViewChange("board")}
        >
          Board
        </Button>
      </div>

      <Button
        type="button"
        variant={showSubtasks ? "default" : "ghost"}
        size="sm"
        aria-pressed={showSubtasks}
        title={showSubtasks ? "Hide subtasks" : "Show subtasks"}
        onClick={() => onShowSubtasksChange(!showSubtasks)}
      >
        <ListTree /> Subtasks
      </Button>

      <ThemeToggle />

      <Button type="button" onClick={onAddClick}>
        <Plus /> Add task
      </Button>
    </nav>
  );
}
