import type React from "react";
import { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import {
  FindGalleriesForSelectDocument,
  type FindGalleriesForSelectQuery,
  type FindGalleriesForSelectQueryVariables,
  type SelectGalleryDataFragment,
} from "src/core/generated-graphql";
import { MultiSelectFilter } from "./selectable-filter";
import type { GalleriesCriterion } from "src/models/list-filter/criteria/galleries";
import { galleryLabel } from "src/lib/gallery-utils";
import { sortByRelevance } from "src/utils/query";

interface GalleriesFilterProps {
  criterion: GalleriesCriterion;
  setCriterion: (c: GalleriesCriterion) => void;
}

function sortResults(query: string, galleries: SelectGalleryDataFragment[]) {
  return sortByRelevance(
    query,
    galleries ?? [],
    (g) => galleryLabel(g),
    () => [],
  ).map((g) => ({
    id: g.id,
    label: galleryLabel(g),
  }));
}

function useGalleryQuery(query: string) {
  const { data, loading } = useQuery<
    FindGalleriesForSelectQuery,
    FindGalleriesForSelectQueryVariables
  >(FindGalleriesForSelectDocument, {
    variables: { filter: { q: query, per_page: 200 } },
  });

  const results = useMemo(
    () => sortResults(query, data?.findGalleries.galleries ?? []),
    [data?.findGalleries.galleries, query],
  );

  return { results, loading };
}

const GalleriesFilter: React.FC<GalleriesFilterProps> = ({
  criterion,
  setCriterion,
}) => {
  return (
    <MultiSelectFilter
      criterion={criterion}
      setCriterion={setCriterion}
      useResults={useGalleryQuery}
    />
  );
};

export default GalleriesFilter;
