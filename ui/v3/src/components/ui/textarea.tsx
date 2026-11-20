import type * as React from "react";
import { Textarea as TextareaPrimitive } from "@ui-sdk/react/textarea";

function Textarea({
  autoResize = true,
  ...props
}: React.ComponentProps<typeof TextareaPrimitive>) {
  return <TextareaPrimitive autoResize={autoResize} {...props} />;
}

export { Textarea };
