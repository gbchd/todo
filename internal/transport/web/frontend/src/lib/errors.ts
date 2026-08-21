// Shared shape for turning a caught value (typically from a failed fetch)
// into a message fit for the inline error banners across the app.
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
