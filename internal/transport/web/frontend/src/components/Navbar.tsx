import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";

export type View = "list" | "board";

interface NavbarProps {
  view: View;
  onViewChange: (view: View) => void;
  onAddClick: () => void;
}

export function Navbar({ view, onViewChange, onAddClick }: NavbarProps) {
  return (
    <nav className="flex items-center gap-4 border-b px-5 py-3">
      <span className="mr-auto text-lg font-semibold">todo</span>

      <div className="inline-flex rounded-md border" role="tablist">
        <Button
          type="button"
          variant={view === "list" ? "default" : "ghost"}
          className="rounded-r-none"
          onClick={() => onViewChange("list")}
        >
          List
        </Button>
        <Button
          type="button"
          variant={view === "board" ? "default" : "ghost"}
          className="rounded-l-none border-l"
          onClick={() => onViewChange("board")}
        >
          Board
        </Button>
      </div>

      <Button type="button" onClick={onAddClick}>
        <Plus /> Add task
      </Button>
    </nav>
  );
}
