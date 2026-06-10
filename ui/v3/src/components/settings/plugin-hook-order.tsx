import type React from "react";
import { useMemo } from "react";
import { useIntl } from "react-intl";
import { useMutation, useQuery } from "@apollo/client/react";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, RotateCcw, PlugIcon } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
} from "src/components/ui/empty";

interface PluginRow {
  id: string;
  name: string;
}

interface HookSection {
  hook: string;
  isOverride: boolean;
  plugins: PluginRow[];
}

function buildSections(
  plugins: GQL.PluginsQuery["plugins"],
  configured: GQL.PluginHookOrderQuery["pluginHookOrder"],
): HookSection[] {
  if (!plugins) return [];

  const enabledById = new Map<string, PluginRow>();
  for (const p of plugins) {
    if (p.enabled) enabledById.set(p.id, { id: p.id, name: p.name });
  }

  const subscribers = new Map<string, PluginRow[]>();
  for (const p of plugins) {
    if (!p.enabled) continue;
    for (const h of p.hooks ?? []) {
      for (const event of h.hooks ?? []) {
        const existing = subscribers.get(event) ?? [];
        if (!existing.find((e) => e.id === p.id)) {
          existing.push({ id: p.id, name: p.name });
        }
        subscribers.set(event, existing);
      }
    }
  }

  const overrides = new Map<string, string[]>();
  for (const o of configured) {
    overrides.set(o.hook, o.plugin_ids);
  }

  const sections: HookSection[] = [];
  for (const [hook, subs] of subscribers) {
    if (subs.length < 2) continue;

    const order = overrides.get(hook);
    if (!order) {
      const sorted = [...subs].sort((a, b) => a.id.localeCompare(b.id));
      sections.push({ hook, isOverride: false, plugins: sorted });
      continue;
    }

    const seen = new Set<string>();
    const ordered: PluginRow[] = [];
    for (const id of order) {
      const p = enabledById.get(id);
      if (p && !seen.has(id)) {
        ordered.push(p);
        seen.add(id);
      }
    }
    const remainder = subs
      .filter((p) => !seen.has(p.id))
      .sort((a, b) => a.id.localeCompare(b.id));
    sections.push({
      hook,
      isOverride: true,
      plugins: [...ordered, ...remainder],
    });
  }

  sections.sort((a, b) => a.hook.localeCompare(b.hook));
  return sections;
}

interface SortableRowProps {
  plugin: PluginRow;
}

function SortableRow({ plugin }: SortableRowProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: plugin.id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className="flex items-center gap-2 bg-card border border-border rounded-md px-2 py-2"
    >
      <Button
        variant="ghost"
        size="icon-sm"
        className="text-muted-foreground hover:text-foreground cursor-grab active:cursor-grabbing shrink-0 hover:bg-transparent"
        {...attributes}
        {...listeners}
        aria-label="Drag to reorder"
      >
        <GripVertical size={16} />
      </Button>
      <div className="flex-1 min-w-0">
        <span className="text-sm truncate block">{plugin.name}</span>
        <span className="text-xs text-muted-foreground">{plugin.id}</span>
      </div>
    </div>
  );
}

interface HookSectionCardProps {
  section: HookSection;
  saving: boolean;
  onReorder: (hook: string, ids: string[]) => void;
  onReset: (hook: string) => void;
}

function HookSectionCard({
  section,
  saving,
  onReorder,
  onReset,
}: HookSectionCardProps) {
  const intl = useIntl();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const ids = section.plugins.map((p) => p.id);
    const oldIndex = ids.indexOf(String(active.id));
    const newIndex = ids.indexOf(String(over.id));
    if (oldIndex === -1 || newIndex === -1) return;
    onReorder(section.hook, arrayMove(ids, oldIndex, newIndex));
  }

  return (
    <div className="border border-border rounded-lg p-3 space-y-2">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="font-mono text-sm truncate">{section.hook}</p>
          <p className="text-xs text-muted-foreground">
            {section.isOverride
              ? intl.formatMessage({
                  id: "plugin_hook_order.custom_order",
                  defaultMessage: "Custom order",
                })
              : intl.formatMessage({
                  id: "plugin_hook_order.alphabetical",
                  defaultMessage: "Alphabetical (default)",
                })}
          </p>
        </div>
        {section.isOverride && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={saving}
            onClick={() => onReset(section.hook)}
          >
            <RotateCcw className="size-3.5" />
            {intl.formatMessage({
              id: "actions.reset",
              defaultMessage: "Reset",
            })}
          </Button>
        )}
      </div>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={section.plugins.map((p) => p.id)}
          strategy={verticalListSortingStrategy}
        >
          <div className="space-y-1.5">
            {section.plugins.map((p) => (
              <SortableRow key={p.id} plugin={p} />
            ))}
          </div>
        </SortableContext>
      </DndContext>
    </div>
  );
}

export function PluginHookOrder() {
  const intl = useIntl();

  const pluginsResult = useQuery(GQL.PluginsDocument);
  const plugins = pluginsResult.data?.plugins;

  const orderResult = useQuery(GQL.PluginHookOrderDocument);
  const orderData = orderResult.data?.pluginHookOrder;

  const [setHookOrder, setHookOrderResult] = useMutation(
    GQL.SetPluginHookOrderDocument,
    {
      refetchQueries: [{ query: GQL.PluginHookOrderDocument }],
    },
  );

  const sections = useMemo(
    () => buildSections(plugins, orderData ?? []),
    [plugins, orderData],
  );

  const loading = pluginsResult.loading || orderResult.loading;

  if (loading) {
    return (
      <div className="flex items-center justify-center p-4">
        <Spinner />
      </div>
    );
  }

  if (sections.length === 0) {
    return (
      <Empty className="border border-dashed border-border rounded-lg">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <PlugIcon />
          </EmptyMedia>
          <EmptyTitle>
            {intl.formatMessage({
              id: "plugin_hook_order.empty_title",
              defaultMessage: "Nothing to order",
            })}
          </EmptyTitle>
          <EmptyDescription>
            {intl.formatMessage({
              id: "plugin_hook_order.empty",
              defaultMessage:
                "No hook events have multiple subscribed plugins. Order is only configurable when two or more enabled plugins respond to the same hook.",
            })}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  function handleReorder(hook: string, pluginIds: string[]) {
    setHookOrder({
      variables: { input: { hook, plugin_ids: pluginIds } },
    });
  }

  function handleReset(hook: string) {
    setHookOrder({
      variables: { input: { hook, plugin_ids: [] } },
    });
  }

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        {intl.formatMessage({
          id: "plugin_hook_order.description",
          defaultMessage:
            "Drag to reorder the plugins that respond to each hook event. Reset returns a hook to alphabetical order. Disabled plugins, and configured plugin IDs that no longer match an installed plugin, are silently filtered out at dispatch time.",
        })}
      </p>
      {sections.map((s) => (
        <HookSectionCard
          key={s.hook}
          section={s}
          saving={setHookOrderResult.loading}
          onReorder={handleReorder}
          onReset={handleReset}
        />
      ))}
    </div>
  );
}
