import type { PropsWithChildren } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { IntlProvider } from "react-intl";
import { describe, expect, it, vi } from "vitest";

vi.mock("src/components/ui/dialog", () => {
  const Part = ({ children }: PropsWithChildren) => <div>{children}</div>;

  return {
    Dialog: Part,
    DialogContent: ({
      children,
      className,
      overlayClassName,
    }: PropsWithChildren<{
      className?: string;
      overlayClassName?: string;
    }>) => (
      <div data-content-class={className} data-overlay-class={overlayClassName}>
        {children}
      </div>
    ),
    DialogDescription: Part,
    DialogFooter: Part,
    DialogHeader: Part,
    DialogTitle: Part,
  };
});

vi.mock("src/components/ui/button", () => ({
  Button: ({ children }: PropsWithChildren) => (
    <button type="button">{children}</button>
  ),
}));

import { DeleteDialog } from "./delete-dialog";

describe("DeleteDialog", () => {
  it("associates file-option labels with unique controls across dialogs", () => {
    const markup = renderToStaticMarkup(
      <IntlProvider locale="en">
        {["first", "second"].map((key) => (
          <DeleteDialog
            key={key}
            open
            showFileOptions
            onOpenChange={() => {}}
            onConfirm={async () => {}}
          />
        ))}
      </IntlProvider>,
    );
    const labelTargets = [
      ...markup.matchAll(/<label\b[^>]*for="([^"]+)"/g),
    ].map((match) => match[1]);
    expect(labelTargets).toHaveLength(4);
    expect(new Set(labelTargets).size).toBe(4);
    for (const id of labelTargets) {
      expect(markup).toContain(`id="${id}"`);
    }
  });

  it("raises both dialog surfaces above the image lightbox", () => {
    const markup = renderToStaticMarkup(
      <IntlProvider locale="en">
        <DeleteDialog
          open
          aboveLightbox
          onOpenChange={() => {}}
          onConfirm={async () => {}}
        />
      </IntlProvider>,
    );

    expect(markup).toContain('data-content-class="z-[10000]"');
    expect(markup).toContain('data-overlay-class="z-[10000]"');
  });
});
