import React, { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import {
  StudioDataFragment,
  FindStudiosForSelectDocument,
  FindStudiosForSelectQuery,
  FindStudiosForSelectQueryVariables,
} from "src/core/generated-graphql";
import { HierarchicalObjectsFilter } from "./SelectableFilter";
import { StudiosCriterion } from "src/models/list-filter/criteria/studios";
import { sortByRelevance } from "src/utils/query";

interface StudiosFilterProps {
  criterion: StudiosCriterion;
  setCriterion: (c: StudiosCriterion) => void;
}

function sortResults(
  query: string,
  studios: Pick<StudioDataFragment, "id" | "name" | "aliases">[],
) {
  return sortByRelevance(
    query,
    studios ?? [],
    (s) => s.name,
    (s) => s.aliases,
  ).map((p) => ({
    id: p.id,
    label: p.name,
  }));
}

function useStudioQuery(query: string, skip?: boolean) {
  const { data, loading } = useQuery<
    FindStudiosForSelectQuery,
    FindStudiosForSelectQueryVariables
  >(FindStudiosForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
    skip: !!skip,
  });

  const results = useMemo(
    () => sortResults(query, data?.findStudios.studios ?? []),
    [data?.findStudios.studios, query],
  );

  return { results, loading };
}

const StudiosFilter: React.FC<StudiosFilterProps> = ({
  criterion,
  setCriterion,
}) => {
  return (
    <HierarchicalObjectsFilter
      criterion={criterion}
      setCriterion={setCriterion}
      useResults={useStudioQuery}
      singleValue
    />
  );
};

export default StudiosFilter;
