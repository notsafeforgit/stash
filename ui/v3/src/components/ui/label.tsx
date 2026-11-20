import type * as React from "react";
import { Label as LabelPrimitive } from "@ui-sdk/react/label";

function Label(props: React.ComponentProps<typeof LabelPrimitive>) {
  return <LabelPrimitive {...props} />;
}

export { Label };
