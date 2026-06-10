import React, { useMemo } from "react";
import { PerformersCriterion } from "src/models/list-filter/criteria/performers";
import { useQuery } from "@apollo/client/react";
import {
  PerformerDataFragment,
  FindPerformersForSelectDocument,
  FindPerformersForSelectQuery,
  FindPerformersForSelectQueryVariables,
} from "src/core/generated-graphql";
import { ObjectsFilter } from "./SelectableFilter";
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

function usePerformerQuery(query: string, skip?: boolean) {
  const { data, loading } = useQuery<
    FindPerformersForSelectQuery,
    FindPerformersForSelectQueryVariables
  >(FindPerformersForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
    skip: !!skip,
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
    <ObjectsFilter
      criterion={criterion}
      setCriterion={setCriterion}
      useResults={usePerformerQuery}
    />
  );
};

export default PerformersFilter;
