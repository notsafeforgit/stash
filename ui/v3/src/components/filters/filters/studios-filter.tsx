import type React from "react";
import { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import {
  type StudioDataFragment,
  FindStudiosForSelectDocument,
  type FindStudiosForSelectQuery,
  type FindStudiosForSelectQueryVariables,
} from "src/core/generated-graphql";
import { HierarchicalObjectsFilter } from "./selectable-filter";
import type { StudiosCriterion } from "src/models/list-filter/criteria/studios";
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

function useStudioQuery(query: string) {
  const { data, loading } = useQuery<
    FindStudiosForSelectQuery,
    FindStudiosForSelectQueryVariables
  >(FindStudiosForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
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
