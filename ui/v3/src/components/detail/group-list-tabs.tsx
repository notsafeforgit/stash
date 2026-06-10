/**
 * Group detail embedded list tab panels.
 */

import { useMemo, useState } from "react";
import type * as GQL from "src/core/generated-graphql";
import {
  FilterMode,
  FilterGroupOperator,
  CriterionModifier,
} from "src/core/generated-graphql";
import { EntityListPage } from "src/components/list";
import { View } from "src/components/list/views";
import {
  useSceneListConfig,
  usePerformerListConfig,
} from "src/components/list/entity-list-configs";
import { ListFilterModel } from "src/models/list-filter/filter";
import {
  GroupsCriterion,
  GroupsCriterionOption,
} from "src/models/list-filter/criteria/groups";
import {
  createASTConditionFromCriterion,
  createASTGroup,
} from "src/models/list-filter/filter-ast";
import { useConfigurationContextOptional } from "src/hooks/config";
import { SceneEditSheet } from "./scene-edit-sheet";
import { PerformerEditSheet } from "./performer-edit-sheet";

// ── Helpers ────────────────────────────────────────────────────────────────────

type GroupData = NonNullable<GQL.FindGroupQuery["findGroup"]>;

function makeGroupFilter(
  mode: FilterMode,
  groupId: string,
  groupName: string,
  config: GQL.ConfigDataFragment | undefined,
): ListFilterModel {
  const filter = new ListFilterModel(mode, config);

  const criterion = new GroupsCriterion(GroupsCriterionOption);
  // `Includes` (vs `IncludesAll`) — for a single-group lock the two
  // are semantically identical ("entity is associated with this
  // group"), and `Includes` is the modifier the backend's
  // performer-filter SQL handler actually implements. The handler at
  // `pkg/sqlite/performer_filter.go:groupsCriterionHandler` falls
  // through `default: return` for `IncludesAll`, silently emitting no
  // WHERE clause and leaving the Performers tab listing every
  // performer in the library. The scenes handler does support
  // `IncludesAll`, but using `Includes` here covers both tabs.
  criterion.modifier = CriterionModifier.Includes;
  criterion.value = {
    items: [{ id: groupId, label: groupName }],
    excluded: [],
    depth: 0,
  };

  const conditionNode = createASTConditionFromCriterion(mode, criterion);
  filter.lockedFilterAst = createASTGroup(mode, FilterGroupOperator.And, [
    conditionNode,
  ]);

  return filter;
}

// ── Scenes tab ─────────────────────────────────────────────────────────────────

export function GroupScenesTab({ group }: { group: GroupData }) {
  const ctx = useConfigurationContextOptional();
  const { id: groupId, name: groupName } = group;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeGroupFilter(FilterMode.Scenes, groupId, groupName, gqlConfig),
    [groupId, groupName, gqlConfig],
  );
  const { config, lightboxElement, lightboxOpen } =
    useSceneListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={groupId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.GroupScenes}
        mobileChromeFixed
        keyboardShortcutsDisabled={lightboxOpen}
      />
      <SceneEditSheet id={editingId} onClose={() => setEditingId(null)} />
      {lightboxElement}
    </>
  );
}

// ── Performers tab ─────────────────────────────────────────────────────────────

export function GroupPerformersTab({ group }: { group: GroupData }) {
  const ctx = useConfigurationContextOptional();
  const { id: groupId, name: groupName } = group;
  const gqlConfig = ctx?.configuration;
  const [editingId, setEditingId] = useState<string | null>(null);
  const defaultFilter = useMemo(
    () => makeGroupFilter(FilterMode.Performers, groupId, groupName, gqlConfig),
    [groupId, groupName, gqlConfig],
  );
  const config = usePerformerListConfig(setEditingId);
  return (
    <>
      <EntityListPage
        key={groupId}
        config={config}
        defaultFilter={defaultFilter}
        view={View.GroupPerformers}
        mobileChromeFixed
      />
      <PerformerEditSheet id={editingId} onClose={() => setEditingId(null)} />
    </>
  );
}
