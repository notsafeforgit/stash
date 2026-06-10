import { useEffect, useState } from "react";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { useLazyQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import {
  type EntityOption,
  EntitySingleSelect,
} from "src/components/forms/async-entity-select";

export interface GroupEntry {
  group_id: string;
  group_name: string;
  scene_index: number | null;
}

interface GroupsFieldProps {
  value: GroupEntry[];
  onChange: (groups: GroupEntry[]) => void;
  disabled?: boolean;
}

export function GroupsField({
  value,
  onChange,
  disabled = false,
}: GroupsFieldProps) {
  const [groupOptions, setGroupOptions] = useState<EntityOption[]>([]);
  const [searchGroups, { data: groupData, loading: groupLoading }] =
    useLazyQuery(GQL.FindGroupsDocument);

  useEffect(() => {
    if (groupData) {
      setGroupOptions(
        groupData.findGroups.groups.map((g) => ({ id: g.id, name: g.name })),
      );
    }
  }, [groupData]);

  // New-row state
  const [pendingGroup, setPendingGroup] = useState<EntityOption | null>(null);

  function handleAdd() {
    if (!pendingGroup) return;
    onChange([
      ...value,
      {
        group_id: pendingGroup.id,
        group_name: pendingGroup.name,
        scene_index: null,
      },
    ]);
    setPendingGroup(null);
  }

  function updateSceneIndex(index: number, raw: string) {
    const next = [...value];
    next[index] = {
      ...next[index],
      scene_index: raw === "" ? null : Number(raw),
    };
    onChange(next);
  }

  function remove(index: number) {
    onChange(value.filter((_, i) => i !== index));
  }

  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 && (
        <table className="w-full text-sm border-collapse">
          <thead>
            <tr className="text-left text-muted-foreground text-xs">
              <th className="pb-1 pr-2 font-medium">Group</th>
              <th className="pb-1 pr-2 font-medium w-20">Scene #</th>
              <th className="pb-1 w-8" />
            </tr>
          </thead>
          <tbody>
            {value.map((entry, i) => (
              <tr key={entry.group_id} className="border-t border-border">
                <td className="py-1 pr-2">{entry.group_name}</td>
                <td className="py-1 pr-2">
                  <Input
                    type="number"
                    min={0}
                    className="h-7 text-sm"
                    value={entry.scene_index ?? ""}
                    disabled={disabled}
                    onChange={(e) => updateSceneIndex(i, e.target.value)}
                  />
                </td>
                <td className="py-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    disabled={disabled}
                    aria-label="Remove group"
                    onClick={() => remove(i)}
                  >
                    <Trash2Icon className="pointer-events-none size-3.5" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div className="flex items-center gap-2">
        <div className="flex-1">
          <EntitySingleSelect
            value={pendingGroup}
            onChange={setPendingGroup}
            options={groupOptions.filter(
              (o) => !value.some((v) => v.group_id === o.id),
            )}
            onSearch={(q) =>
              searchGroups({ variables: { filter: { q, per_page: 20 } } })
            }
            loading={groupLoading}
            placeholder="Search groups…"
            disabled={disabled}
          />
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled || !pendingGroup}
          onClick={handleAdd}
        >
          <PlusIcon className="size-3.5" />
          Add
        </Button>
      </div>
    </div>
  );
}
