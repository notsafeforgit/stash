import { FormattedMessage, useIntl } from "react-intl";
import { useMutation, useQuery } from "@apollo/client/react";
import { ChevronDown } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "src/components/ui/collapsible";
import { useToast } from "src/hooks/toast";
import { TaskGroup, TaskSectionHeading } from "./task-section";

export function PluginTasks() {
  const intl = useIntl();
  const toast = useToast();
  const { data } = useQuery(GQL.PluginsDocument);
  const [run] = useMutation(GQL.RunPluginTaskDocument);

  const taskPlugins = (data?.plugins ?? []).filter(
    (p) => p.enabled && p.tasks && p.tasks.length > 0,
  );

  if (taskPlugins.length === 0) return null;

  async function onRun(pluginId: string, taskName: string) {
    try {
      await run({
        variables: { plugin_id: pluginId, task_name: taskName },
      });
      toast.success(
        intl.formatMessage(
          {
            id: "config.tasks.added_job_to_queue",
            defaultMessage: "Added {operation_name} job to queue.",
          },
          { operation_name: taskName },
        ),
      );
    } catch (e) {
      toast.error(e);
    }
  }

  return (
    <TaskGroup
      title={intl.formatMessage({
        id: "config.tasks.plugin_tasks",
        defaultMessage: "Plugin Tasks",
      })}
    >
      {taskPlugins.map((plugin) => (
        // One <Collapsible> per plugin so users with chatty plugins can
        // fold them out of view. Default-open: most users have a
        // handful of plugins and want to see them straight away — but
        // the affordance is right there for the noisy-plugin case.
        <Collapsible
          key={plugin.id}
          defaultOpen
          className="border-b last:border-b-0"
        >
          <CollapsibleTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                className="group/plugin h-auto w-full justify-between rounded-none px-0 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground hover:bg-transparent hover:text-foreground"
              />
            }
          >
            <span>{plugin.name}</span>
            <ChevronDown className="size-3 transition-transform group-data-[panel-open]/plugin:rotate-180" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            {(plugin.tasks ?? []).map((task) => (
              <TaskSectionHeading
                key={`${plugin.id}-${task.name}`}
                title={task.name}
                description={task.description ?? undefined}
                actions={
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={() => void onRun(plugin.id, task.name)}
                  >
                    <FormattedMessage id="actions.run" defaultMessage="Run" />
                  </Button>
                }
              />
            ))}
          </CollapsibleContent>
        </Collapsible>
      ))}
    </TaskGroup>
  );
}
