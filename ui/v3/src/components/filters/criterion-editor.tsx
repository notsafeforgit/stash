import type React from "react";
import { useCallback, useMemo } from "react";
import { CriterionModifier } from "src/core/generated-graphql";
import {
  DurationCriterion,
  type CriterionValue,
  ModifierCriterion,
  IHierarchicalLabeledIdCriterion,
  NumberCriterion,
  ILabeledIdCriterion,
  DateCriterion,
  TimestampCriterion,
  BooleanCriterion,
  type Criterion,
} from "src/models/list-filter/criteria/criterion";
import {
  type IHierarchicalLabelValue,
  criterionIsHierarchicalLabelValue,
  criterionIsNumberValue,
  criterionIsStashIDValue,
  criterionIsDateValue,
  criterionIsTimestampValue,
} from "src/models/list-filter/types";
import { DurationFilter } from "./filters/duration-filter";
import { NumberFilter } from "./filters/number-filter";
import { LabeledIdFilter } from "./filters/labeled-id-filter";
import { HierarchicalLabelValueFilter } from "./filters/hierarchical-label-value-filter";
import { InputFilter } from "./filters/input-filter";
import { DateFilter } from "./filters/date-filter";
import { TimestampFilter } from "./filters/timestamp-filter";
import { CountryCriterion } from "src/models/list-filter/criteria/country";
import { CountrySelect } from "src/components/shared/country-select";
import { StashIDCriterion } from "src/models/list-filter/criteria/stash-ids";
import { StashIDFilter } from "./filters/stash-id-filter";
import { RatingCriterion } from "../../models/list-filter/criteria/rating";
import { RatingFilter } from "./filters/rating-filter";
import { BooleanFilter } from "./filters/boolean-filter";
import { OptionFilter, OptionListFilter } from "./filters/option-filter";
import { PathFilter } from "./filters/path-filter";
import { PerformersCriterion } from "src/models/list-filter/criteria/performers";
import PerformersFilter from "./filters/performers-filter";
import { StudiosCriterion } from "src/models/list-filter/criteria/studios";
import StudiosFilter from "./filters/studios-filter";
import { TagsCriterion } from "src/models/list-filter/criteria/tags";
import TagsFilter from "./filters/tags-filter";
import {
  PhashCriterion,
  DuplicatedCriterion,
} from "src/models/list-filter/criteria/phash";
import { PhashFilter } from "./filters/phash-filter";
import { DuplicatedFilter } from "./filters/duplicate-filter";
import { PathCriterion } from "src/models/list-filter/criteria/path";
import { ModifierSelect } from "./modifier-select";
import {
  CustomFieldsCriterion,
  SingleCustomFieldCriterion,
} from "src/models/list-filter/criteria/custom-fields";
import type { FilterMode } from "src/core/generated-graphql";
import { CustomFieldsFilter } from "./filters/custom-fields-filter";
import { SingleCustomFieldFilter } from "./filters/single-custom-field-filter";
import { FolderFilter } from "./filters/folder-filter";
import {
  FolderCriterion,
  ParentFolderCriterion,
} from "src/models/list-filter/criteria/folder";
import { GroupsCriterion } from "src/models/list-filter/criteria/groups";
import GroupsFilter from "./filters/groups-filter";
import { GalleriesCriterion } from "src/models/list-filter/criteria/galleries";
import GalleriesFilter from "./filters/galleries-filter";

type HierarchyMode = NonNullable<IHierarchicalLabelValue["hierarchyMode"]>;

export type RenderSelectFn = (
  options: Array<{ value: string; label: string }>,
  currentValue: string,
  onChange: (value: string) => void,
) => React.ReactNode;

interface GenericCriterionEditor {
  criterion: ModifierCriterion<CriterionValue>;
  setCriterion: (c: ModifierCriterion<CriterionValue>) => void;
  renderModifierSelect?: (
    options: CriterionModifier[],
    value: CriterionModifier,
    onChanged: (m: CriterionModifier) => void,
  ) => React.ReactNode;
  renderHierarchyModeSelect?: (
    value: HierarchyMode,
    onChange: (mode: HierarchyMode) => void,
  ) => React.ReactNode;
  renderSimpleSelect?: RenderSelectFn;
  renderNumberInput?: (
    value: number,
    onChange: (v: number) => void,
    min?: number,
  ) => React.ReactNode;
}

const GenericCriterionEditor: React.FC<GenericCriterionEditor> = ({
  criterion,
  setCriterion,
  renderModifierSelect,
  renderHierarchyModeSelect,
  renderSimpleSelect,
  renderNumberInput,
}) => {
  const { options, modifierOptions } = criterion.modifierCriterionOption();

  const showModifierSelector = useMemo(() => {
    if (
      criterion instanceof PerformersCriterion ||
      criterion instanceof StudiosCriterion ||
      criterion instanceof TagsCriterion ||
      criterion instanceof GroupsCriterion ||
      criterion instanceof GalleriesCriterion ||
      criterion instanceof FolderCriterion ||
      criterion instanceof ParentFolderCriterion
    ) {
      return false;
    }

    return modifierOptions && modifierOptions.length > 1;
  }, [criterion, modifierOptions]);

  const alwaysShowFilter = useMemo(() => {
    return (
      criterion instanceof StashIDCriterion ||
      criterion instanceof PerformersCriterion ||
      criterion instanceof StudiosCriterion ||
      criterion instanceof TagsCriterion ||
      criterion instanceof GroupsCriterion ||
      criterion instanceof GalleriesCriterion
    );
  }, [criterion]);

  const onChangedModifierSelect = useCallback(
    (m: CriterionModifier) => {
      const newCriterion =
        criterion.clone() as ModifierCriterion<CriterionValue>;
      newCriterion.modifier = m;
      if (
        newCriterion instanceof NumberCriterion &&
        (m === CriterionModifier.Between || m === CriterionModifier.NotBetween)
      ) {
        const lower = newCriterion.value?.value ?? 0;
        const upper = newCriterion.value?.value2 ?? 0;
        if (upper <= lower) {
          newCriterion.value = {
            ...newCriterion.value,
            value: lower,
            value2: lower + 1,
          };
        }
      }
      setCriterion(newCriterion);
    },
    [criterion, setCriterion],
  );

  const modifierSelector = useMemo(() => {
    if (!showModifierSelector) {
      return;
    }

    if (renderModifierSelect) {
      return renderModifierSelect(
        modifierOptions,
        criterion.modifier,
        onChangedModifierSelect,
      );
    }

    return (
      <ModifierSelect
        options={modifierOptions}
        value={criterion.modifier}
        onChanged={onChangedModifierSelect}
      />
    );
  }, [
    showModifierSelector,
    modifierOptions,
    onChangedModifierSelect,
    criterion.modifier,
    renderModifierSelect,
  ]);

  const usesStackedSelectorLayout = useMemo(
    () => criterion.modifierCriterionOption().type === "groups",
    [criterion],
  );

  const valueControl = useMemo(() => {
    function onValueChanged(value: CriterionValue) {
      const newCriterion =
        criterion.clone() as ModifierCriterion<CriterionValue>;
      newCriterion.value = value;
      setCriterion(newCriterion);
    }

    // always show stashID filter
    if (criterion instanceof StashIDCriterion) {
      return (
        <StashIDFilter criterion={criterion} onValueChanged={onValueChanged} />
      );
    }

    // Hide the value select if the modifier is "IsNull" or "NotNull"
    if (
      !alwaysShowFilter &&
      (criterion.modifier === CriterionModifier.IsNull ||
        criterion.modifier === CriterionModifier.NotNull)
    ) {
      return;
    }

    if (criterion instanceof PerformersCriterion) {
      return (
        <PerformersFilter
          criterion={criterion}
          setCriterion={(c) => setCriterion(c)}
        />
      );
    }

    if (criterion instanceof StudiosCriterion) {
      return (
        <StudiosFilter
          criterion={criterion}
          setCriterion={(c) => setCriterion(c)}
        />
      );
    }

    if (criterion instanceof TagsCriterion) {
      return (
        <TagsFilter
          criterion={criterion}
          setCriterion={(c) => setCriterion(c)}
          renderHierarchyModeSelect={renderHierarchyModeSelect}
        />
      );
    }

    if (
      criterion instanceof FolderCriterion ||
      criterion instanceof ParentFolderCriterion
    ) {
      return (
        <FolderFilter
          criterion={criterion}
          setCriterion={(c) => setCriterion(c)}
        />
      );
    }

    if (criterion instanceof ILabeledIdCriterion) {
      return (
        <LabeledIdFilter
          criterion={criterion}
          onValueChanged={onValueChanged}
        />
      );
    }
    if (criterion instanceof GroupsCriterion) {
      return (
        <GroupsFilter
          criterion={criterion}
          setCriterion={(c) => setCriterion(c)}
        />
      );
    }
    if (criterion instanceof GalleriesCriterion) {
      return (
        <GalleriesFilter
          criterion={criterion}
          setCriterion={(c) => setCriterion(c)}
        />
      );
    }
    if (criterion instanceof IHierarchicalLabeledIdCriterion) {
      return (
        <HierarchicalLabelValueFilter
          criterion={criterion}
          onValueChanged={onValueChanged}
          mode={usesStackedSelectorLayout ? "select-only" : "full"}
        />
      );
    }
    if (
      options &&
      !criterionIsHierarchicalLabelValue(criterion.value) &&
      !criterionIsNumberValue(criterion.value) &&
      !criterionIsStashIDValue(criterion.value) &&
      !criterionIsDateValue(criterion.value) &&
      !criterionIsTimestampValue(criterion.value)
    ) {
      if (!Array.isArray(criterion.value)) {
        return (
          <OptionFilter
            criterion={criterion}
            setCriterion={setCriterion}
            renderSelect={renderSimpleSelect}
          />
        );
      } else {
        return (
          <OptionListFilter criterion={criterion} setCriterion={setCriterion} />
        );
      }
    }
    if (criterion instanceof PathCriterion) {
      return (
        <PathFilter criterion={criterion} onValueChanged={onValueChanged} />
      );
    }
    if (criterion instanceof DurationCriterion) {
      return (
        <DurationFilter criterion={criterion} onValueChanged={onValueChanged} />
      );
    }
    if (criterion instanceof DateCriterion) {
      return (
        <DateFilter criterion={criterion} onValueChanged={onValueChanged} />
      );
    }
    if (criterion instanceof TimestampCriterion) {
      return (
        <TimestampFilter
          criterion={criterion}
          onValueChanged={onValueChanged}
        />
      );
    }
    if (criterion instanceof NumberCriterion) {
      return (
        <NumberFilter
          criterion={criterion}
          onValueChanged={onValueChanged}
          renderNumberInput={renderNumberInput}
        />
      );
    }
    if (criterion instanceof RatingCriterion) {
      return (
        <RatingFilter criterion={criterion} onValueChanged={onValueChanged} />
      );
    }
    if (criterion instanceof PhashCriterion) {
      return (
        <PhashFilter criterion={criterion} onValueChanged={onValueChanged} />
      );
    }
    if (
      criterion instanceof CountryCriterion &&
      (criterion.modifier === CriterionModifier.Equals ||
        criterion.modifier === CriterionModifier.NotEquals)
    ) {
      return (
        <CountrySelect
          value={criterion.value}
          onChange={(v) => onValueChanged(v)}
          menuPortalTarget={document.body}
        />
      );
    }
    return (
      <InputFilter criterion={criterion} onValueChanged={onValueChanged} />
    );
  }, [
    criterion,
    setCriterion,
    options,
    alwaysShowFilter,
    usesStackedSelectorLayout,
    renderHierarchyModeSelect,
    renderSimpleSelect,
    renderNumberInput,
  ]);

  if (usesStackedSelectorLayout) {
    return (
      <div className="criterion-editor-group-stacked flex flex-col gap-2">
        <HierarchicalLabelValueFilter
          criterion={criterion as ModifierCriterion<IHierarchicalLabelValue>}
          onValueChanged={(value) => {
            const newCriterion =
              criterion.clone() as ModifierCriterion<CriterionValue>;
            newCriterion.value = value;
            setCriterion(newCriterion);
          }}
          mode="toggle-only"
        />
        {modifierSelector}
        {valueControl}
      </div>
    );
  }

  return (
    <div
      className={
        showModifierSelector
          ? "flex flex-col gap-2"
          : "criterion-editor-no-modifier flex flex-col gap-2"
      }
    >
      {modifierSelector}
      {valueControl}
    </div>
  );
};

interface CriterionEditor {
  criterion: Criterion;
  setCriterion: (c: Criterion) => void;
  // FilterMode for the enclosing list — required for criteria whose editor
  // needs to query mode-specific data (e.g. custom field names).
  mode?: FilterMode;
  renderModifierSelect?: GenericCriterionEditor["renderModifierSelect"];
  renderHierarchyModeSelect?: GenericCriterionEditor["renderHierarchyModeSelect"];
  renderSimpleSelect?: RenderSelectFn;
  renderNumberInput?: GenericCriterionEditor["renderNumberInput"];
}

export const CriterionEditor: React.FC<CriterionEditor> = ({
  criterion,
  setCriterion,
  mode,
  renderModifierSelect,
  renderHierarchyModeSelect,
  renderSimpleSelect,
  renderNumberInput,
}) => {
  const filterControl = useMemo(() => {
    if (criterion instanceof BooleanCriterion) {
      return (
        <BooleanFilter
          criterion={criterion}
          setCriterion={setCriterion}
          renderSelect={renderSimpleSelect}
        />
      );
    }

    if (criterion instanceof DuplicatedCriterion) {
      return (
        <DuplicatedFilter criterion={criterion} setCriterion={setCriterion} />
      );
    }

    if (criterion instanceof SingleCustomFieldCriterion) {
      if (!mode) {
        // Defensive: a single-custom-field criterion can only be edited if we
        // know the entity type to query field names against. Render nothing
        // rather than a broken editor.
        return null;
      }
      return (
        <SingleCustomFieldFilter
          criterion={criterion}
          setCriterion={setCriterion}
          mode={mode}
        />
      );
    }

    if (criterion instanceof CustomFieldsCriterion) {
      return (
        <CustomFieldsFilter criterion={criterion} setCriterion={setCriterion} />
      );
    }

    if (criterion instanceof ModifierCriterion) {
      return (
        <GenericCriterionEditor
          criterion={criterion}
          setCriterion={setCriterion}
          renderModifierSelect={renderModifierSelect}
          renderHierarchyModeSelect={renderHierarchyModeSelect}
          renderSimpleSelect={renderSimpleSelect}
          renderNumberInput={renderNumberInput}
        />
      );
    }

    return null;
  }, [
    criterion,
    setCriterion,
    mode,
    renderModifierSelect,
    renderHierarchyModeSelect,
    renderSimpleSelect,
    renderNumberInput,
  ]);

  return <div className="criterion-editor">{filterControl}</div>;
};
