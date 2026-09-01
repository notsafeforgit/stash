import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ListActivityContext, useListActivity } from "./list-activity-context";

function ActivityState() {
  return <span>{useListActivity() ? "active" : "inactive"}</span>;
}

describe("list activity context", () => {
  it("keeps standalone lists active", () => {
    expect(renderToStaticMarkup(<ActivityState />)).toContain("active");
  });

  it("makes a keep-mounted hidden tab passive", () => {
    const markup = renderToStaticMarkup(
      <ListActivityContext value={false}>
        <ActivityState />
      </ListActivityContext>,
    );

    expect(markup).toContain("inactive");
  });
});
