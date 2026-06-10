import React, { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import {
  CriterionModifier,
  TagDataFragment,
  FindTagsForSelectDocument,
  FindTagsForSelectQuery,
  FindTagsForSelectQueryVariables,
} from "src/core/generated-graphql";
import { HierarchicalObjectsFilter } from "./SelectableFilter";
import { sortByRelevance } from "src/utils/query";
import {
  StudioTagsCriterion,
  TagsCriterion,
} from "src/models/list-filter/criteria/tags";
import { useIntl } from "react-intl";
import { PinnableComboBox } from "src/components/ui/pinnable-combo-box";

type HierarchyMode =
  | "exact"
  | "ancestors"
  | "descendants"
  | "ancestors_descendants";

interface TagsFilterProps {
  criterion: TagsCriterion;
  setCriterion: (c: TagsCriterion) => void;
  renderHierarchyModeSelect?: (
    value: HierarchyMode,
    onChange: (mode: HierarchyMode) => void,
  ) => React.ReactNode;
}

function sortResults(
  query: string,
  tags: Pick<TagDataFragment, "id" | "name" | "aliases">[],
) {
  return sortByRelevance(
    query,
    tags ?? [],
    (t) => t.name,
    (t) => t.aliases,
  ).map((p) => {
    return {
      id: p.id,
      label: p.name,
    };
  });
}

function useTagQuery(query: string, skip?: boolean) {
  const { data, loading } = useQuery<
    FindTagsForSelectQuery,
    FindTagsForSelectQueryVariables
  >(FindTagsForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
    skip: !!skip,
  });

  const results = useMemo(
    () => sortResults(query, data?.findTags.tags ?? []),
    [data, query],
  );

  return { results, loading };
}

const StudioTagHierarchySelector: React.FC<{
  criterion: StudioTagsCriterion;
  setCriterion: (c: StudioTagsCriterion) => void;
  renderHierarchyModeSelect?: TagsFilterProps["renderHierarchyModeSelect"];
}> = ({ criterion, setCriterion, renderHierarchyModeSelect }) => {
  const intl = useIntl();

  if (
    criterion.modifier === CriterionModifier.IsNull ||
    criterion.modifier === CriterionModifier.NotNull
  ) {
    return null;
  }

  function onModeChange(mode: HierarchyMode) {
    const nextCriterion = criterion.clone() as StudioTagsCriterion;
    nextCriterion.value.hierarchyMode = mode;
    setCriterion(nextCriterion);
  }

  const currentMode = (criterion.value.hierarchyMode ??
    "exact") as HierarchyMode;

  if (renderHierarchyModeSelect) {
    return <>{renderHierarchyModeSelect(currentMode, onModeChange)}</>;
  }

  const hierarchyModeOptions = [
    {
      value: "exact",
      label: intl.formatMessage({ id: "studio_tag_hierarchy_mode.exact" }),
    },
    {
      value: "ancestors",
      label: intl.formatMessage({ id: "studio_tag_hierarchy_mode.ancestors" }),
    },
    {
      value: "descendants",
      label: intl.formatMessage({
        id: "studio_tag_hierarchy_mode.descendants",
      }),
    },
    {
      value: "ancestors_descendants",
      label: intl.formatMessage({
        id: "studio_tag_hierarchy_mode.ancestors_descendants",
      }),
    },
  ];

  return (
    <div className="mb-2">
      <label>{intl.formatMessage({ id: "studio_tag_hierarchy_mode" })}</label>
      <PinnableComboBox
        currentLabel={
          hierarchyModeOptions.find((o) => o.value === currentMode)?.label ??
          currentMode
        }
        options={hierarchyModeOptions}
        selectedValue={currentMode}
        onSelect={(v) => onModeChange(v as HierarchyMode)}
      />
    </div>
  );
};

const TagsFilter: React.FC<TagsFilterProps> = ({
  criterion,
  setCriterion,
  renderHierarchyModeSelect,
}) => {
  const hierarchySelector =
    criterion instanceof StudioTagsCriterion ? (
      <StudioTagHierarchySelector
        criterion={criterion}
        setCriterion={(c) => setCriterion(c)}
        renderHierarchyModeSelect={renderHierarchyModeSelect}
      />
    ) : null;

  return (
    <>
      {hierarchySelector}
      <HierarchicalObjectsFilter
        criterion={criterion}
        setCriterion={setCriterion}
        useResults={useTagQuery}
      />
    </>
  );
};

export default TagsFilter;
