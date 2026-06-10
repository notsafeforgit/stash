import React, { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import {
  FindGroupsForSelectDocument,
  FindGroupsForSelectQuery,
  FindGroupsForSelectQueryVariables,
  SelectGroupDataFragment,
} from "src/core/generated-graphql";
import { ObjectsFilter } from "./SelectableFilter";
import { GroupsCriterion } from "src/models/list-filter/criteria/groups";
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

function useGroupQuery(query: string, skip?: boolean) {
  const { data, loading } = useQuery<
    FindGroupsForSelectQuery,
    FindGroupsForSelectQueryVariables
  >(FindGroupsForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
    skip: !!skip,
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
    <ObjectsFilter
      criterion={criterion}
      setCriterion={setCriterion}
      useResults={useGroupQuery}
    />
  );
};

export default GroupsFilter;
