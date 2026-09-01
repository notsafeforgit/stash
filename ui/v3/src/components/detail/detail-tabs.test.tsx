import type { PropsWithChildren } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { useListActivity } from "src/components/list/list-activity-context";

vi.mock("src/components/ui/tabs", () => ({
  Tabs: ({ children }: PropsWithChildren) => <div>{children}</div>,
  TabsContent: ({ children, value }: PropsWithChildren<{ value: string }>) => (
    <section data-tab={value}>{children}</section>
  ),
}));

vi.mock("src/components/detail/detail-tab-strip", () => ({
  DetailTabStrip: () => null,
}));

vi.mock("src/hooks/use-tab-state", () => ({
  useTabState: () => ({
    activeTab: "images",
    selectTab: vi.fn(),
    isMounted: () => true,
  }),
}));

import { DetailTabs } from "./detail-tabs";

function ListActivity({ id }: { id: string }) {
  return (
    <span data-list={id}>{useListActivity() ? "active" : "inactive"}</span>
  );
}

describe("DetailTabs", () => {
  it("keeps mounted hidden lists passive", () => {
    const markup = renderToStaticMarkup(
      <DetailTabs
        activeTab="images"
        onTabChange={() => {}}
        tabs={[
          {
            id: "scenes",
            label: "Scenes",
            content: <ListActivity id="scenes" />,
          },
          {
            id: "images",
            label: "Images",
            content: <ListActivity id="images" />,
          },
        ]}
      />,
    );

    expect(markup).toContain('data-list="scenes">inactive');
    expect(markup).toContain('data-list="images">active');
  });
});
