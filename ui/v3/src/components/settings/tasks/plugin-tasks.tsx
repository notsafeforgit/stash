import { FormattedMessage, useIntl } from "react-intl";
import { useMutation, useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
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
        <section key={plugin.id} className="border-b pt-4 last:border-b-0">
          <h3 className="text-xs font-medium text-muted-foreground">
            {plugin.name}
          </h3>
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
        </section>
      ))}
    </TaskGroup>
  );
}
