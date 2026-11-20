import type * as React from "react";
import {
  Collapsible as CollapsiblePrimitive,
  CollapsiblePanel,
  CollapsibleTrigger as CollapsibleTriggerPrimitive,
} from "@ui-sdk/react/collapsible";

function Collapsible(props: React.ComponentProps<typeof CollapsiblePrimitive>) {
  return <CollapsiblePrimitive {...props} />;
}

function CollapsibleTrigger(
  props: React.ComponentProps<typeof CollapsibleTriggerPrimitive>,
) {
  return <CollapsibleTriggerPrimitive {...props} />;
}

function CollapsibleContent(
  props: React.ComponentProps<typeof CollapsiblePanel>,
) {
  return <CollapsiblePanel {...props} />;
}

export { Collapsible, CollapsibleTrigger, CollapsibleContent };
