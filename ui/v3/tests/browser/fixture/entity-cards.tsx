import { useState } from "react";
import { EntityCard } from "@/components/cards/entity-card";
import { EntityContextMenuContent } from "@/components/cards/entity-context-menu-content";
import { ContextMenuItem } from "@/components/ui/context-menu";
import { useMediaQuery } from "@/utils/screen";

const title = `A full entity title ${"with-more-detail_".repeat(16)}`;

export function EntityCardsFixture() {
  const [selected, setSelected] = useState(false);
  const [action, setAction] = useState("");
  const mobile = useMediaQuery("(max-width: 767px)");
  return (
    <div data-selecting className="p-3">
      <div className="w-36">
        <EntityCard
          id="long-title"
          label={title}
          destination={{
            to: "/scenes/$sceneId",
            params: { sceneId: "long-title" },
            search: undefined,
          }}
          isMobile={mobile}
          selected={selected}
          onSelectedChanged={setSelected}
          contextMenu={
            <EntityContextMenuContent title={title}>
              <ContextMenuItem onClick={() => setAction("edit")}>
                Edit
              </ContextMenuItem>
            </EntityContextMenuContent>
          }
        >
          <EntityCard.Body>
            <EntityCard.Title>{title}</EntityCard.Title>
            <EntityCard.Subtitle>
              A subtitle that also needs more than one line
            </EntityCard.Subtitle>
          </EntityCard.Body>
        </EntityCard>
      </div>
      <output data-testid="card-action">{action}</output>
    </div>
  );
}
