import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@apollo/client/react";
import {
  CriterionModifier,
  FilterMode,
  type FolderDataFragment,
  type MultiCriterionInput,
  FindFolderHierarchyForIDsDocument,
  type FindFolderHierarchyForIDsQuery,
  type FindFolderHierarchyForIDsQueryVariables,
  FindFoldersForQueryDocument,
  type FindFoldersForQueryQuery,
  type FindFoldersForQueryQueryVariables,
  FindRootFoldersForSelectDocument,
  type FindRootFoldersForSelectQuery,
  type FindRootFoldersForSelectQueryVariables,
} from "src/core/generated-graphql";
import { ChevronDown, ChevronRight, Minus, X } from "lucide-react";
import { Button } from "src/components/ui/button";
import { ExpandCollapseButton } from "src/components/ui/collapse-button";
import { cn } from "src/lib/utils";
import { queryFindSubFolders } from "src/core/folders";
import { keyboardClickHandler } from "src/utils/keyboard";
import type { FolderCriterion } from "src/models/list-filter/criteria/folder";
import { FormattedMessage, type MessageDescriptor, useIntl } from "react-intl";
import { DepthSelector } from "./selectable-filter";
import ClearableInput from "src/components/ui/clearable-input";
import { useDebouncedState } from "src/hooks/debounce";

interface FolderItem {
  id: string;
  label: string;
}

interface Folder extends FolderDataFragment {
  children?: Folder[];
  expanded: boolean;
}

const SelectedList: React.FC<{
  items: FolderItem[];
  excluded?: boolean;
  onUnselect: (item: FolderItem) => void;
}> = ({ items, excluded, onUnselect }) => {
  if (!items.length) return null;

  return (
    <ul className={`selected-list${excluded ? " excluded-list" : ""}`}>
      {items.map((item) => (
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
  );
};

const FolderRow: React.FC<{
  folder: Folder;
  level?: number;
  canExclude?: boolean;
  toggleExpanded: (folder: Folder) => void;
  onSelect: (folder: Folder, exclude?: boolean) => void;
}> = ({ folder, level, toggleExpanded, onSelect, canExclude }) => {
  return (
    <>
      <li
        className="folder-row unselected-object"
        style={{ paddingLeft: (level ?? 0) * 5 }}
      >
        <a
          onClick={() => onSelect(folder)}
          onKeyDown={keyboardClickHandler(() => onSelect(folder))}
          tabIndex={0}
        >
          <span>
            <span
              className={cn({
                empty: folder.children && folder.children.length === 0,
              })}
            >
              <ExpandCollapseButton
                collapsed={!folder.expanded}
                setCollapsed={() => toggleExpanded(folder)}
                collapsedIcon={ChevronRight}
                notCollapsedIcon={ChevronDown}
              />
            </span>
            {folder.basename}
          </span>
          {canExclude && (
            <Button
              variant="ghost"
              size="sm"
              className="exclude-button"
              onClick={(e) => {
                e.stopPropagation();
                onSelect(folder, true);
              }}
              onKeyDown={(e) => e.stopPropagation()}
            >
              <span className="exclude-button-text">
                <FormattedMessage id="actions.exclude_lowercase" />
              </span>
              <Minus className="exclude-icon" size={16} />
            </Button>
          )}
        </a>
      </li>
      {folder.expanded &&
        folder.children?.map((child) => (
          <FolderRow
            key={child.id}
            folder={child}
            level={(level ?? 0) + 1}
            toggleExpanded={toggleExpanded}
            onSelect={onSelect}
            canExclude={canExclude}
          />
        ))}
    </>
  );
};

function toggleExpandedFn(object: Folder): (f: Folder) => Folder {
  return (f: Folder) => {
    if (f.id === object.id) {
      return { ...f, expanded: !f.expanded };
    }

    if (f.children) {
      return {
        ...f,
        children: f.children.map(toggleExpandedFn(object)),
      };
    }

    return f;
  };
}

function replaceFolder(folder: Folder): (f: Folder) => Folder {
  return (f: Folder) => {
    if (f.id === folder.id) {
      return folder;
    }

    if (f.children) {
      return {
        ...f,
        children: f.children.map(replaceFolder(folder)),
      };
    }

    return f;
  };
}

function mergeFolderMaps(base: Folder[], update: Folder[]): Folder[] {
  const ret = [...base];

  update.forEach((updateFolder) => {
    const existingIndex = ret.findIndex((f) => f.id === updateFolder.id);
    if (existingIndex === -1) {
      ret.push(updateFolder);
    } else {
      ret[existingIndex] = updateFolder;
    }
  });

  return ret;
}

export function useFolderMap(props: {
  query: string;
  skip?: boolean;
  initialSelected?: string[];
  mode?: FilterMode;
}) {
  const { query, skip = false, initialSelected, mode } = props;

  const [cachedInitialSelected] = useState<string[]>(initialSelected ?? []);

  const excludeZipFolders =
    mode === FilterMode.Scenes || mode === FilterMode.Galleries;

  const zipFileFilter: MultiCriterionInput | undefined = useMemo(
    () =>
      excludeZipFolders
        ? {
            modifier: CriterionModifier.IsNull,
          }
        : undefined,
    [excludeZipFolders],
  );

  const folderFilterForQuery = useMemo(
    () => (zipFileFilter ? { zip_file: zipFileFilter } : undefined),
    [zipFileFilter],
  );

  const { data: rootFoldersResult } = useQuery<
    FindRootFoldersForSelectQuery,
    FindRootFoldersForSelectQueryVariables
  >(FindRootFoldersForSelectDocument, {
    skip,
    variables: {
      zip_file_filter: zipFileFilter,
    },
  });

  const { data: queryFoldersResult } = useQuery<
    FindFoldersForQueryQuery,
    FindFoldersForQueryQueryVariables
  >(FindFoldersForQueryDocument, {
    skip: !query,
    variables: {
      filter: { q: query, per_page: 200 },
      folder_filter: folderFilterForQuery,
    },
  });

  const { data: initialSelectedResult } = useQuery<
    FindFolderHierarchyForIDsQuery,
    FindFolderHierarchyForIDsQueryVariables
  >(FindFolderHierarchyForIDsDocument, {
    skip: !initialSelected || cachedInitialSelected.length === 0,
    variables: {
      ids: cachedInitialSelected ?? [],
    },
  });

  const rootFolders: Folder[] = useMemo(() => {
    const ret = rootFoldersResult?.findFolders.folders ?? [];
    return ret.map((f) => ({ ...f, expanded: false, children: undefined }));
  }, [rootFoldersResult]);

  const initialSelectedFolders: Folder[] = useMemo(() => {
    const ret: Folder[] = [];
    (initialSelectedResult?.findFolders.folders ?? []).forEach((folder) => {
      if (!folder.parent_folders.length) {
        if (!ret.find((f) => f.id === folder.id)) {
          ret.push({ ...folder, expanded: true, children: [] });
        }
        return;
      }

      let currentParent: Folder | undefined;

      for (let i = folder.parent_folders.length - 1; i >= 0; i--) {
        const thisFolder = folder.parent_folders[i];
        let existing: Folder | undefined;

        if (i === folder.parent_folders.length - 1) {
          existing = ret.find((f) => f.id === thisFolder.id);
          if (!existing) {
            existing = {
              ...folder.parent_folders[i],
              expanded: true,
              children: folder.parent_folders[i].sub_folders
                .filter((f) => f.zip_file === null || !excludeZipFolders)
                .map((f) => ({
                  ...f,
                  expanded: false,
                  children: undefined,
                })),
            };
            ret.push(existing);
          }
          currentParent = existing;
          continue;
        }

        const existingIndex =
          currentParent!.children?.findIndex((f) => f.id === thisFolder.id) ??
          -1;
        if (existingIndex === -1) {
          throw new Error(
            `Parent folder ${thisFolder.id} not found in children of ${
              currentParent!.id
            }`,
          );
        }

        existing = currentParent!.children![existingIndex];

        existing = {
          ...existing,
          expanded: true,
          children: thisFolder.sub_folders
            .filter((f) => f.zip_file === null || !excludeZipFolders)
            .map((f) => ({
              ...f,
              expanded: false,
              children: undefined,
            })),
        };

        currentParent!.children![existingIndex] = existing;
        currentParent = existing;
      }
    });
    return ret;
  }, [initialSelectedResult, excludeZipFolders]);

  const mergedRootFolders = useMemo(() => {
    if (query) {
      return rootFolders;
    }

    return mergeFolderMaps(rootFolders, initialSelectedFolders);
  }, [rootFolders, initialSelectedFolders, query]);

  const queryFolders: Folder[] = useMemo(() => {
    const ret: Folder[] = [];

    (queryFoldersResult?.findFolders.folders ?? []).forEach((folder) => {
      if (!folder.parent_folders.length) {
        if (!ret.find((f) => f.id === folder.id)) {
          ret.push({ ...folder, expanded: true, children: [] });
        }
        return;
      }

      let currentParent: Folder | undefined;
      for (let i = folder.parent_folders.length - 1; i >= 0; i--) {
        const thisFolder = folder.parent_folders[i];
        let existing: Folder | undefined;

        if (i === folder.parent_folders.length - 1) {
          existing = ret.find((f) => f.id === thisFolder.id);
          if (!existing) {
            existing = {
              ...folder.parent_folders[i],
              expanded: true,
              children: [],
            };
            ret.push(existing);
          }
          currentParent = existing;
          continue;
        }

        existing = currentParent!.children?.find((f) => f.id === thisFolder.id);
        if (!existing) {
          existing = {
            ...thisFolder,
            expanded: true,
            children: [],
          };
          currentParent!.children!.push(existing);
        }
        currentParent = existing;
      }

      if (!currentParent) {
        return;
      }

      if (!currentParent.children) {
        currentParent.children = [];
      }

      currentParent!.children!.push({
        ...folder,
        expanded: false,
        children: undefined,
      });
    });
    return ret;
  }, [queryFoldersResult]);

  const [folderMap, setFolderMap] = React.useState<Folder[]>([]);

  useEffect(() => {
    if (!query) {
      setFolderMap(mergedRootFolders);
    } else {
      setFolderMap(queryFolders);
    }
  }, [query, mergedRootFolders, queryFolders]);

  async function onToggleExpanded(folder: Folder) {
    setFolderMap(folderMap.map(toggleExpandedFn(folder)));

    if (folder.children === undefined) {
      const subFolderResult = await queryFindSubFolders(
        folder.id,
        excludeZipFolders,
      );
      setFolderMap((current) =>
        current.map(
          replaceFolder({
            ...folder,
            expanded: true,
            children: (subFolderResult.data?.findFolders.folders ?? []).map(
              (f) => ({
                ...f,
                expanded: false,
              }),
            ),
          }),
        ),
      );
    }
  }

  return { folderMap, onToggleExpanded };
}

function getMatchingFolders(folders: Folder[], query: string): Folder[] {
  let matches: Folder[] = [];

  const queryLower = query.toLowerCase();

  folders.forEach((folder) => {
    if (
      folder.basename.toLowerCase().includes(queryLower) ||
      folder.path.toLowerCase() === queryLower
    ) {
      matches.push(folder);
    }

    if (folder.children) {
      matches = matches.concat(getMatchingFolders(folder.children, query));
    }
  });

  return matches;
}

export const FolderSelector: React.FC<{
  onSelect: (folder: Folder, exclude?: boolean) => void;
  canExclude?: boolean;
  preListContent?: React.ReactNode;
  folderMap: Folder[];
  onToggleExpanded: (folder: Folder) => void;
}> = ({
  onSelect,
  preListContent,
  canExclude = false,
  folderMap,
  onToggleExpanded,
}) => {
  return (
    <ul className="selectable-list">
      {preListContent}
      {folderMap.map((folder) => (
        <FolderRow
          key={folder.id}
          folder={folder}
          onSelect={(f, exclude) => onSelect(f, exclude)}
          toggleExpanded={onToggleExpanded}
          canExclude={canExclude}
        />
      ))}
    </ul>
  );
};

interface FolderFilterProps {
  criterion: FolderCriterion;
  setCriterion: (c: FolderCriterion) => void;
  mode?: FilterMode;
}

export const FolderFilter: React.FC<FolderFilterProps> = ({
  criterion,
  setCriterion,
  mode,
}) => {
  const intl = useIntl();
  const [query, setQuery] = useState("");
  const [displayQuery, onQueryChange] = useDebouncedState(query, setQuery, 250);

  const { folderMap, onToggleExpanded } = useFolderMap({ query, mode });

  function criterionOptionTypeToIncludeID(): string {
    return "include-sub-folders";
  }

  function criterionOptionTypeToIncludeUIString(): MessageDescriptor {
    const optionType = "include_sub_folders";

    return {
      id: optionType,
    };
  }

  function onDepthChanged(depth: number) {
    const newValue = criterion.clone() as FolderCriterion;
    newValue.value.depth = depth;
    setCriterion(newValue);
  }

  function onSelect(folder: Folder, exclude: boolean = false) {
    const newValue = criterion.clone() as FolderCriterion;

    if (!exclude) {
      if (newValue.value.items.find((i) => i.id === folder.id)) {
        return;
      }

      newValue.value.items.push({ id: folder.id, label: folder.path });
    } else {
      if (newValue.value.excluded.find((i) => i.id === folder.id)) {
        return;
      }

      newValue.value.excluded.push({ id: folder.id, label: folder.path });
    }

    setCriterion(newValue);
  }

  const onUnselect = useCallback(
    (i: FolderItem, excluded?: boolean) => {
      const newValue = criterion.clone() as FolderCriterion;

      if (!excluded) {
        newValue.value.items = newValue.value.items.filter(
          (item) => item.id !== i.id,
        );
      } else {
        newValue.value.excluded = newValue.value.excluded.filter(
          (item) => item.id !== i.id,
        );
      }
      setCriterion(newValue);
    },
    [criterion, setCriterion],
  );

  function onEnter() {
    if (!query) return;

    const matchingFolders = getMatchingFolders(folderMap, query);
    if (matchingFolders.length === 1) {
      onSelect(matchingFolders[0]);
    }
  }

  const selectedList = useMemo(() => {
    const selected: FolderItem[] =
      criterion.value?.items.map((item) => ({
        id: item.id,
        label: item.label,
      })) ?? [];

    return <SelectedList items={selected} onUnselect={onUnselect} />;
  }, [criterion, onUnselect]);

  const excludedList = useMemo(() => {
    const selected: FolderItem[] =
      criterion.value?.excluded.map((item) => ({
        id: item.id,
        label: item.label,
      })) ?? [];

    return (
      <SelectedList
        excluded
        items={selected}
        onUnselect={(i) => onUnselect(i, true)}
      />
    );
  }, [criterion, onUnselect]);

  return (
    <div className="folder-filter">
      <DepthSelector
        depth={criterion.value.depth}
        onDepthChanged={onDepthChanged}
        id={criterionOptionTypeToIncludeID()}
        label={intl.formatMessage(criterionOptionTypeToIncludeUIString())}
      />

      <div>
        {selectedList}
        {excludedList}
        <ClearableInput
          value={displayQuery}
          setValue={(v) => onQueryChange(v)}
          placeholder={`${intl.formatMessage({ id: "actions.search" })}…`}
          onEnter={onEnter}
        />
        <FolderSelector
          folderMap={folderMap}
          onToggleExpanded={onToggleExpanded}
          onSelect={onSelect}
          canExclude
        />
      </div>
    </div>
  );
};
