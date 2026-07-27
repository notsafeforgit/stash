import { Component, type ErrorInfo, type PropsWithChildren } from "react";
import type { ListFilterModel } from "src/models/list-filter/filter";
import { getRegisteredFilterExtras } from "./registry";

class PluginFilterExtrasBoundary extends Component<
  PropsWithChildren<{ pluginId: string }>,
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(
      `[stash-plugin:${this.props.pluginId}] filter extras render failed`,
      error,
      info,
    );
  }

  render() {
    return this.state.failed ? null : this.props.children;
  }
}

export function PluginFilterExtras({
  filter,
  view,
}: {
  filter: ListFilterModel;
  view?: string;
}) {
  const extensions = getRegisteredFilterExtras();
  if (extensions.length === 0) return null;

  return (
    <div className="plugin-filter-extras flex flex-wrap items-center gap-2 border-b border-border bg-background/90 px-3 py-1.5">
      {extensions.map(({ pluginId, component: Extras }, index) => (
        <PluginFilterExtrasBoundary
          key={`${pluginId}-${index}`}
          pluginId={pluginId}
        >
          <Extras filter={filter} searchTerm={filter.searchTerm} view={view} />
        </PluginFilterExtrasBoundary>
      ))}
    </div>
  );
}
