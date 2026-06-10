import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { JobTable } from "src/components/settings/tasks/job-table";
import { LibraryTasks } from "src/components/settings/tasks/library-tasks";
import { DataManagementTasks } from "src/components/settings/tasks/data-management-tasks";
import { PluginTasks } from "src/components/settings/tasks/plugin-tasks";

function TasksPage() {
  const intl = useIntl();
  return (
    <div className="max-w-3xl space-y-6 p-6">
      <section className="space-y-3">
        <h2 className="text-base font-medium">
          {intl.formatMessage({
            id: "config.tasks.job_queue",
            defaultMessage: "Task Queue",
          })}
        </h2>
        <JobTable />
      </section>

      <LibraryTasks />
      <DataManagementTasks />
      <PluginTasks />
    </div>
  );
}

export const Route = createFileRoute("/settings/tasks")({
  component: TasksPage,
});
