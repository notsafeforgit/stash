/**
 * Single metadata row used by every entity detail tab (scene, image,
 * tag, gallery, performer, group, studio). Stacked layout: small
 * label on top, value left-aligned underneath. Sized so the
 * narrow detail sidebar (320 px on desktop) doesn't have to
 * spend half its width on label gutter.
 *
 * Caller wraps a series of `<MetaRow>` in a `<dl>` for semantics.
 */
import type React from "react";

export function MetaRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-0.5 py-1.5 first:pt-0 last:pb-0">
      <dt className="text-[0.7rem] uppercase tracking-wide text-muted-foreground font-medium leading-none">
        {label}
      </dt>
      <dd className="m-0 text-sm [overflow-wrap:anywhere] select-text">
        {children}
      </dd>
    </div>
  );
}
