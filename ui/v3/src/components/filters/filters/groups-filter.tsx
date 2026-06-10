import type React from "react";
import { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import {
  FindGroupsForSelectDocument,
  type FindGroupsForSelectQuery,
  type FindGroupsForSelectQueryVariables,
  type SelectGroupDataFragment,
} from "src/core/generated-graphql";
import { MultiSelectFilter } from "./selectable-filter";
import type { GroupsCriterion } from "src/models/list-filter/criteria/groups";
import { sortByRelevance } from "src/utils/query";

interface GroupsFilterProps {
  criterion: GroupsCriterion;
  setCriterion: (c: GroupsCriterion) => void;
}

function sortResults(
  query: string,
  groups: Pick<SelectGroupDataFragment, "id" | "name" | "aliases">[],
) {
  return sortByRelevance(
    query,
    groups ?? [],
    (g) => g.name,
    (g) => (g.aliases ? [g.aliases] : []),
  ).map((g) => ({
    id: g.id,
    label: g.name,
  }));
}

function useGroupQuery(query: string) {
  const { data, loading } = useQuery<
    FindGroupsForSelectQuery,
    FindGroupsForSelectQueryVariables
  >(FindGroupsForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
  });

  const results = useMemo(
    () => sortResults(query, data?.findGroups.groups ?? []),
    [data?.findGroups.groups, query],
  );

  return { results, loading };
}

const GroupsFilter: React.FC<GroupsFilterProps> = ({
  criterion,
  setCriterion,
}) => {
  return (
    <MultiSelectFilter
      criterion={criterion}
      setCriterion={setCriterion}
      useResults={useGroupQuery}
    />
  );
};

export default GroupsFilter;
