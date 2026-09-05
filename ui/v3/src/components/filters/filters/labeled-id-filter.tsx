import type React from "react";
import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@apollo/client/react";
import type { ModifierCriterion } from "src/models/list-filter/criteria/criterion";
import {
  CriterionModifier,
  FindScenesForSelectDocument,
  type FindScenesForSelectQuery,
  type FindScenesForSelectQueryVariables,
  FindGalleriesForSelectDocument,
  type FindGalleriesForSelectQuery,
  type FindGalleriesForSelectQueryVariables,
  FindStudiosForSelectDocument,
  type FindStudiosForSelectQuery,
  type FindStudiosForSelectQueryVariables,
} from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
import { objectTitle } from "src/core/files";
import type { ILabeledId } from "src/models/list-filter/types";
import { ClearableInput } from "src/components/ui/clearable-input";
import { Button } from "src/components/ui/button";
import { useDebounce } from "src/hooks/debounce";
import { X, Plus } from "lucide-react";

interface LabeledIdFilterProps {
  criterion: ModifierCriterion<ILabeledId[]>;
  onValueChanged: (value: ILabeledId[]) => void;
}

export const LabeledIdFilter: React.FC<LabeledIdFilterProps> = ({
  criterion,
  onValueChanged,
}) => {
  const criterionOption = criterion.modifierCriterionOption();
  const { inputType } = criterionOption;

  const [query, setQuery] = useState("");
  const [displayQuery, setDisplayQuery] = useState("");

  const debouncedSetQuery = useDebounce(setQuery, 250);
  const onQueryChange = useCallback(
    (input: string) => {
      setDisplayQuery(input);
      debouncedSetQuery(input);
    },
    [debouncedSetQuery],
  );

  // All three hooks always called — skipped based on inputType
  const { data: scenesData } = useQuery<
    FindScenesForSelectQuery,
    FindScenesForSelectQueryVariables
  >(FindScenesForSelectDocument, {
    variables: { filter: { q: query, per_page: 50 } },
    skip: inputType !== "scenes",
  });

  const { data: galleriesData } = useQuery<
    FindGalleriesForSelectQuery,
    FindGalleriesForSelectQueryVariables
  >(FindGalleriesForSelectDocument, {
    variables: { filter: { q: query, per_page: 50 } },
    skip: inputType !== "galleries",
  });

  const { data: studiosData } = useQuery<
    FindStudiosForSelectQuery,
    FindStudiosForSelectQueryVariables
  >(FindStudiosForSelectDocument, {
    variables: { filter: { q: query, per_page: 50 } },
    skip: inputType !== "studios",
  });

  const queryResults: ILabeledId[] = useMemo(() => {
    if (inputType === "scenes") {
      return (scenesData?.findScenes.scenes ?? []).map((s) => ({
        id: s.id,
        label: objectTitle(s) || s.id,
      }));
    }
    if (inputType === "galleries") {
      return (galleriesData?.findGalleries.galleries ?? []).map((g) => ({
        id: g.id,
        label: galleryLabel(g),
      }));
    }
    if (inputType === "studios") {
      return (studiosData?.findStudios.studios ?? []).map((s) => ({
        id: s.id,
        label: s.name,
      }));
    }
    return [];
  }, [inputType, scenesData, galleriesData, studiosData]);

  const selected = criterion.value;

  const isNullModifier =
    criterion.modifier === CriterionModifier.IsNull ||
    criterion.modifier === CriterionModifier.NotNull;

  const unselectedResults = useMemo(
    () => queryResults.filter((r) => !selected.find((s) => s.id === r.id)),
    [queryResults, selected],
  );

  function onSelect(item: ILabeledId) {
    onValueChanged([...selected, item]);
    debouncedSetQuery.cancel();
    setQuery("");
    setDisplayQuery("");
  }

  function onUnselect(item: ILabeledId) {
    onValueChanged(selected.filter((s) => s.id !== item.id));
  }

  if (
    inputType !== "performers" &&
    inputType !== "studios" &&
    inputType !== "scene_tags" &&
    inputType !== "performer_tags" &&
    inputType !== "studio_tags" &&
    inputType !== "tags" &&
    inputType !== "scenes" &&
    inputType !== "groups" &&
    inputType !== "galleries"
  ) {
    return null;
  }

  return (
    <div className="labeled-id-filter">
      {!isNullModifier && (
        <>
          <ul className="selected-list">
            {selected.map((item) => (
              <li key={item.id} className="selected-object">
                <span>{item.label}</span>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={() => onUnselect(item)}
                >
                  <X size={16} />
                </Button>
              </li>
            ))}
          </ul>
          <ClearableInput value={displayQuery} setValue={onQueryChange} />
          <ul className="selectable-list">
            {unselectedResults.map((item) => (
              <li key={item.id} className="unselected-object">
                <Button
                  variant="ghost"
                  className="w-full justify-start"
                  onClick={() => onSelect(item)}
                >
                  <Plus className="icon" size={16} />
                  <span>{item.label}</span>
                </Button>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
};

export type ModifierValue = "any" | "none" | "any_of" | "only" | "include_subs";

export function getModifierCandidates(props: {
  modifier: CriterionModifier;
  defaultModifier: CriterionModifier;
  hasSelected?: boolean;
  hasExcluded?: boolean;
  singleValue?: boolean;
  hierarchical?: boolean;
}): ModifierValue[] {
  const {
    modifier,
    defaultModifier,
    hasSelected,
    hasExcluded,
    singleValue,
    hierarchical,
  } = props;
  const ret: ModifierValue[] = [];

  if (modifier === defaultModifier && !hasSelected && !hasExcluded) {
    ret.push("any");
  }
  if (modifier === defaultModifier && !hasSelected && !hasExcluded) {
    ret.push("none");
  }
  if (!singleValue && modifier === defaultModifier && hasSelected) {
    ret.push("any_of");
  }
  if (
    hierarchical &&
    modifier === defaultModifier &&
    (hasSelected || hasExcluded)
  ) {
    ret.push("include_subs");
  }
  if (
    !singleValue &&
    modifier === defaultModifier &&
    hasSelected &&
    !hasExcluded
  ) {
    ret.push("only");
  }
  return ret;
}

export function modifierValueToModifier(key: ModifierValue): CriterionModifier {
  switch (key) {
    case "any":
      return CriterionModifier.NotNull;
    case "none":
      return CriterionModifier.IsNull;
    case "any_of":
      return CriterionModifier.Includes;
    case "only":
      return CriterionModifier.Equals;
  }

  throw new Error("Invalid modifier value");
}
