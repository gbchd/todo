interface ErrorMessageProps {
  message: string | null;
}

// One shared inline error treatment, reused wherever a mutation or fetch
// fails, instead of every call site inventing its own.
export function ErrorMessage({ message }: ErrorMessageProps) {
  if (!message) return null;
  return <p className="text-sm text-destructive">{message}</p>;
}
