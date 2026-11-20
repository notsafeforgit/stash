// @vitest-environment jsdom
import type * as React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Button } from "./button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "./field";
import { Input } from "./input";

let root: Root;
let container: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

async function render(ui: React.ReactNode) {
  await act(async () => root.render(ui));
}

describe("FieldLabel", () => {
  it("names a button the field doesn't own through htmlFor", async () => {
    await render(
      <Field>
        <FieldLabel htmlFor="retry-count">Retry count</FieldLabel>
        <Button id="retry-count">Set</Button>
      </Field>,
    );

    const label = container.querySelector("label");
    expect(label?.htmlFor).toBe("retry-count");
    expect(label?.control).toBe(container.querySelector("button"));
  });

  it("names the field's input without htmlFor", async () => {
    await render(
      <Field>
        <FieldLabel>Display name</FieldLabel>
        <Input />
      </Field>,
    );

    const input = container.querySelector("input");
    expect(input?.labels?.[0]?.textContent).toBe("Display name");
  });
});

describe("Field", () => {
  it("shows the invalid state of a field marked with data-invalid", async () => {
    await render(
      <Field data-invalid>
        <FieldLabel htmlFor="page">Page</FieldLabel>
        <Input id="page" />
        <FieldError>Enter a page between 1 and 5.</FieldError>
      </Field>,
    );

    expect(container.querySelector("input")?.getAttribute("aria-invalid")).toBe(
      "true",
    );
    expect(
      container
        .querySelector('[data-slot="field-label"]')
        ?.hasAttribute("data-invalid"),
    ).toBe(true);
  });

  it("leaves the browser's validation in charge when data-invalid is false", async () => {
    await render(
      <Field data-invalid={false}>
        <FieldLabel htmlFor="page">Page</FieldLabel>
        <Input id="page" />
      </Field>,
    );

    expect(
      container
        .querySelector('[data-slot="field-label"]')
        ?.hasAttribute("data-invalid"),
    ).toBe(false);
  });

  it("lays a horizontal field out in a row", async () => {
    await render(
      <Field orientation="horizontal">
        <FieldLabel>Notify me</FieldLabel>
      </Field>,
    );

    expect(
      container
        .querySelector('[data-slot="field"]')
        ?.getAttribute("data-orientation"),
    ).toBe("horizontal");
  });

  it("keeps the description with the field", async () => {
    await render(
      <Field>
        <FieldLabel htmlFor="code">Code</FieldLabel>
        <Input id="code" />
        <FieldDescription>Six digits.</FieldDescription>
      </Field>,
    );

    const input = container.querySelector("input");
    const description = container.querySelector(
      '[data-slot="field-description"]',
    );
    expect(description?.id).toBeTruthy();
    expect(input?.getAttribute("aria-describedby")).toContain(description?.id);
  });
});

describe("FieldError", () => {
  it("shows each distinct message from errors and an alert role", async () => {
    await render(
      <Field>
        <FieldError
          errors={[
            { message: "Required" },
            { message: "Required" },
            { message: "Too short" },
          ]}
        />
      </Field>,
    );

    const alert = container.querySelector('[role="alert"]');
    expect(container.querySelectorAll("li").length).toBe(2);
    expect(alert?.textContent).toContain("Required");
    expect(alert?.textContent).toContain("Too short");
  });

  it("renders nothing without errors or children", async () => {
    await render(
      <Field>
        <FieldError errors={[]} />
      </Field>,
    );

    expect(container.querySelector('[data-slot="field-error"]')).toBeNull();
  });
});

describe("FieldLegend", () => {
  it("records its variant on data-variant inside a field set", async () => {
    await render(
      <FieldSet>
        <FieldLegend variant="label">Scope</FieldLegend>
      </FieldSet>,
    );

    expect(
      container
        .querySelector('[data-slot="field-legend"]')
        ?.getAttribute("data-variant"),
    ).toBe("label");
  });
});
