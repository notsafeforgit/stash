import type * as React from "react";
import {
  Tabs as TabsPrimitive,
  TabsList as TabsListPrimitive,
  TabsPanel,
  TabsTab,
} from "@ui-sdk/react/tabs";

type TabsRootProps = React.ComponentProps<typeof TabsPrimitive<string>>;

type TabsProps = Omit<
  TabsRootProps,
  "value" | "defaultValue" | "onValueChange"
> & {
  value?: string;
  defaultValue?: string;
  onValueChange?: (
    value: string,
    details: Parameters<NonNullable<TabsRootProps["onValueChange"]>>[1],
  ) => void;
};

function Tabs({ onValueChange, ...props }: TabsProps) {
  return (
    <TabsPrimitive
      {...props}
      onValueChange={(value, details) => {
        if (value === null) {
          details.cancel();
          return;
        }
        onValueChange?.(value, details);
      }}
    />
  );
}

type TabsListProps = React.ComponentProps<typeof TabsListPrimitive> & {
  variant?: "default" | "line";
};

function TabsList({ variant = "line", ...props }: TabsListProps) {
  return <TabsListPrimitive data-variant={variant} {...props} />;
}

export { Tabs, TabsList, TabsTab as TabsTrigger, TabsPanel as TabsContent };
