import type { Priority, Status } from "./types";

export function statusLabel(s: Status): string {
  return { open: "Open", "in-progress": "In Progress", done: "Done" }[s];
}

export function priorityClass(p: Priority): string {
  switch (p) {
    case "high":
      return "text-red-600 dark:text-red-400";
    case "medium":
      return "text-amber-600 dark:text-amber-400";
    default:
      return "text-muted-foreground";
  }
}
