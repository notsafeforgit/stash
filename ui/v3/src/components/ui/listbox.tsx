import {
  createListboxController,
  Listbox as ListboxPrimitive,
  ListboxItem as ListboxItemPrimitive,
  ListboxItemIndicator as ListboxItemIndicatorPrimitive,
  type ListboxController,
  type ListboxProps,
} from "@ui-sdk/react/listbox";
import { matchRanges } from "@ui-sdk/react/text-match";
import type * as React from "react";

import { cn } from "@/lib/utils";

type DistributiveOmit<T, K extends PropertyKey> = T extends unknown
  ? Omit<T, K>
  : never;

type WithClassName<P> = DistributiveOmit<P, "className"> & {
  className?: string;
};

function Listbox<T = string, Multiple extends boolean | undefined = false>({
  className,
  ...props
}: WithClassName<ListboxProps<T, Multiple>>) {
  return (
    <ListboxPrimitive
      {...(props as ListboxProps<unknown, false>)}
      className={cn(className)}
    />
  );
}

function ListboxItem<T = string>({
  className,
  ...props
}: WithClassName<React.ComponentProps<typeof ListboxItemPrimitive<T>>>) {
  return <ListboxItemPrimitive<T> className={cn(className)} {...props} />;
}

function ListboxItemIndicator({
  className,
  ...props
}: WithClassName<React.ComponentProps<typeof ListboxItemIndicatorPrimitive>>) {
  return <ListboxItemIndicatorPrimitive className={cn(className)} {...props} />;
}

export {
  createListboxController,
  Listbox,
  ListboxItem,
  ListboxItemIndicator,
  matchRanges,
  type ListboxController,
};
