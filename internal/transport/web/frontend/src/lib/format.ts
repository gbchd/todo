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
    case "low":
      return "text-sky-600 dark:text-sky-400";
    default:
      return "text-muted-foreground";
  }
}

export function priorityBadgeClass(p: Priority): string {
  switch (p) {
    case "high":
      return "bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-400";
    case "medium":
      return "bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-400";
    case "low":
      return "bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-400";
    default:
      return "bg-muted text-muted-foreground";
  }
}

export function statusBadgeClass(s: Status): string {
  switch (s) {
    case "in-progress":
      return "bg-primary/10 text-primary dark:bg-primary/20";
    case "done":
      return "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-400";
    default:
      return "bg-muted text-muted-foreground";
  }
}
