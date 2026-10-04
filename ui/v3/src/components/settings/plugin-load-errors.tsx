import type { PluginLoadErrorsQuery } from "@/core/generated-graphql";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useMsg } from "@/hooks/message";

export function PluginLoadErrors({
  errors,
}: {
  errors: PluginLoadErrorsQuery["pluginLoadErrorsV3"];
}) {
  const msg = useMsg();
  if (!errors.length) return null;

  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("config.plugins.load_errors", "Some plugins could not be loaded")}
      </AlertTitle>
      <AlertDescription>
        <ul className="flex flex-col gap-3">
          {errors.map((error) => (
            <li key={error.path}>
              <code className="break-all">{error.path}</code>
              <div
                data-selectable-text
                className="whitespace-pre-wrap break-words"
              >
                {error.message}
              </div>
            </li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  );
}
