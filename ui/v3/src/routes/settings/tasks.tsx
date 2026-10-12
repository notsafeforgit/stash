import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { JobTable } from "src/components/settings/tasks/job-table";
import { LibraryTasks } from "src/components/settings/tasks/library-tasks";
import { DataManagementTasks } from "src/components/settings/tasks/data-management-tasks";
import { PluginTasks } from "src/components/settings/tasks/plugin-tasks";
import { ScrapeTasks } from "@/components/settings/tasks/scrape-tasks";

function TasksPage() {
  const intl = useIntl();
  return (
    <div className="flex max-w-3xl flex-col gap-6 p-6">
      <section className="flex flex-col gap-3">
        <h2 className="text-base font-medium">
          {intl.formatMessage({
            id: "config.tasks.job_queue",
            defaultMessage: "Library task queue",
          })}
        </h2>
        <JobTable />
      </section>

      <ScrapeTasks />

      <LibraryTasks />
      <DataManagementTasks />
      <PluginTasks />
    </div>
  );
}

export const Route = createFileRoute("/settings/tasks")({
  component: TasksPage,
});
