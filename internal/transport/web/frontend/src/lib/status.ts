import type { Status } from "./types";

// The next status in the open → in-progress → done → open cycle, matching the
// TUI's space bar and keeping one status model across all three interfaces.
export const NEXT_STATUS: Record<Status, Status> = {
  open: "in-progress",
  "in-progress": "done",
  done: "open",
};
