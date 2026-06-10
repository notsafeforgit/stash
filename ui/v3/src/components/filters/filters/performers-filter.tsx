import type React from "react";
import { useMemo } from "react";
import type { PerformersCriterion } from "src/models/list-filter/criteria/performers";
import { useQuery } from "@apollo/client/react";
import {
  type PerformerDataFragment,
  FindPerformersForSelectDocument,
  type FindPerformersForSelectQuery,
  type FindPerformersForSelectQueryVariables,
} from "src/core/generated-graphql";
import { MultiSelectFilter } from "./selectable-filter";
import { sortByRelevance } from "src/utils/query";

interface PerformersFilterProps {
  criterion: PerformersCriterion;
  setCriterion: (c: PerformersCriterion) => void;
}

function sortResults(
  query: string,
  performers?: Pick<PerformerDataFragment, "name" | "aliases" | "id">[],
) {
  return sortByRelevance(
    query,
    performers ?? [],
    (p) => p.name,
    (p) => p.aliases.map((a) => a.alias),
  ).map((p) => ({
    id: p.id,
    label: p.name,
  }));
}

function usePerformerQuery(query: string) {
  const { data, loading } = useQuery<
    FindPerformersForSelectQuery,
    FindPerformersForSelectQueryVariables
  >(FindPerformersForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
  });

  const results = useMemo(
    () => sortResults(query, data?.findPerformers.performers),
    [data, query],
  );

  return { results, loading };
}

const PerformersFilter: React.FC<PerformersFilterProps> = ({
  criterion,
  setCriterion,
}) => {
  return (
    <MultiSelectFilter
      criterion={criterion}
      setCriterion={setCriterion}
      useResults={usePerformerQuery}
    />
  );
};

export default PerformersFilter;
