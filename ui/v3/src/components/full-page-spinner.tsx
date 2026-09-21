import { Spinner } from "@/components/ui/spinner";

export function FullPageSpinner() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
      <Spinner className="size-10 text-muted-foreground" />
    </div>
  );
}
