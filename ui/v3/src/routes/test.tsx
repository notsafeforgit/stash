import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { PinnableComboBox } from "src/components/ui/pinnable-combo-box";
import { Button } from "src/components/ui/button";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "src/components/ui/bottom-sheet";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";

const TEST_OPTIONS = [
  { value: "apple", label: "Apple" },
  { value: "banana", label: "Banana" },
  { value: "cherry", label: "Cherry" },
  { value: "date", label: "Date" },
  { value: "elderberry", label: "Elderberry" },
  { value: "fig", label: "Fig" },
  { value: "grape", label: "Grape" },
  { value: "honeydew", label: "Honeydew" },
  { value: "kiwi", label: "Kiwi" },
  { value: "lemon", label: "Lemon" },
  { value: "mango", label: "Mango" },
  { value: "nectarine", label: "Nectarine" },
  { value: "orange", label: "Orange" },
  { value: "papaya", label: "Papaya" },
  { value: "quince", label: "Quince" },
  { value: "raspberry", label: "Raspberry" },
  { value: "strawberry", label: "Strawberry" },
  { value: "tangerine", label: "Tangerine" },
  { value: "ugli", label: "Ugli fruit" },
  { value: "watermelon", label: "Watermelon" },
];

const FRUITS = [
  { value: "apple", label: "Apple" },
  { value: "apricot", label: "Apricot" },
  { value: "avocado", label: "Avocado" },
  { value: "banana", label: "Banana" },
  { value: "blueberry", label: "Blueberry" },
  { value: "cherry", label: "Cherry" },
  { value: "coconut", label: "Coconut" },
  { value: "date", label: "Date" },
  { value: "dragonfruit", label: "Dragon Fruit" },
  { value: "elderberry", label: "Elderberry" },
  { value: "fig", label: "Fig" },
  { value: "grape", label: "Grape" },
  { value: "guava", label: "Guava" },
  { value: "honeydew", label: "Honeydew" },
  { value: "kiwi", label: "Kiwi" },
  { value: "lemon", label: "Lemon" },
  { value: "lime", label: "Lime" },
  { value: "lychee", label: "Lychee" },
  { value: "mango", label: "Mango" },
  { value: "nectarine", label: "Nectarine" },
];

const VEGGIES = [
  { value: "artichoke", label: "Artichoke" },
  { value: "broccoli", label: "Broccoli" },
  { value: "carrot", label: "Carrot" },
  { value: "celery", label: "Celery" },
  { value: "cucumber", label: "Cucumber" },
  { value: "eggplant", label: "Eggplant" },
  { value: "garlic", label: "Garlic" },
  { value: "kale", label: "Kale" },
  { value: "leek", label: "Leek" },
  { value: "mushroom", label: "Mushroom" },
  { value: "onion", label: "Onion" },
  { value: "parsnip", label: "Parsnip" },
  { value: "pea", label: "Pea" },
  { value: "pepper", label: "Pepper" },
  { value: "potato", label: "Potato" },
  { value: "pumpkin", label: "Pumpkin" },
  { value: "radish", label: "Radish" },
  { value: "spinach", label: "Spinach" },
  { value: "squash", label: "Squash" },
  { value: "zucchini", label: "Zucchini" },
];

const SELECT_ITEMS = [
  { value: null, label: "Select an item" },
  ...FRUITS,
  ...VEGGIES,
];

function SelectScrollableTest() {
  const [value, setValue] = useState<string | null>(null);
  const label =
    SELECT_ITEMS.find((o) => o.value === value)?.label ?? "Select an item";
  return (
    <section className="flex flex-col gap-2">
      <p className="text-sm text-muted-foreground">
        Shadcn-style scrollable select (20 items, 2 groups):
      </p>
      <Select
        items={SELECT_ITEMS}
        value={value}
        onValueChange={(v) => setValue(v)}
      >
        <SelectTrigger className="w-full max-w-64">
          <SelectValue>{label}</SelectValue>
        </SelectTrigger>
        <SelectContent visibleItems={8}>
          <SelectGroup>
            <SelectLabel>Fruits</SelectLabel>
            {FRUITS.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectGroup>
          <SelectGroup>
            <SelectLabel>Vegetables</SelectLabel>
            {VEGGIES.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <p className="text-xs text-muted-foreground">Selected: {label}</p>
    </section>
  );
}

function TestPage() {
  const [pageValue, setPageValue] = useState("apple");
  const [sheetValue, setSheetValue] = useState("apple");
  const [sheetOpen, setSheetOpen] = useState(false);

  const pageLabel =
    TEST_OPTIONS.find((o) => o.value === pageValue)?.label ?? pageValue;
  const sheetLabel =
    TEST_OPTIONS.find((o) => o.value === sheetValue)?.label ?? sheetValue;

  return (
    <div className="p-6 flex flex-col gap-8 max-w-sm">
      <h1 className="text-lg font-semibold">Combobox scroll test</h1>

      <SelectScrollableTest />

      <section className="flex flex-col gap-2">
        <p className="text-sm text-muted-foreground">
          Combobox on page (outside any drawer):
        </p>
        <PinnableComboBox
          currentLabel={pageLabel}
          options={TEST_OPTIONS}
          selectedValue={pageValue}
          onSelect={setPageValue}
        />
        <p className="text-xs text-muted-foreground">Selected: {pageLabel}</p>
      </section>

      <section className="flex flex-col gap-2">
        <p className="text-sm text-muted-foreground">
          Combobox inside a BottomSheet:
        </p>
        <Button onClick={() => setSheetOpen(true)}>Open drawer</Button>
      </section>

      <BottomSheet open={sheetOpen} onOpenChange={setSheetOpen}>
        <BottomSheetHeader className="border-b border-border py-3! px-4!">
          <BottomSheetTitle>Drawer combobox test</BottomSheetTitle>
        </BottomSheetHeader>
        <div className="p-4 flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            Combobox inside drawer:
          </p>
          <PinnableComboBox
            currentLabel={sheetLabel}
            options={TEST_OPTIONS}
            selectedValue={sheetValue}
            onSelect={setSheetValue}
          />
          <p className="text-xs text-muted-foreground">
            Selected: {sheetLabel}
          </p>
        </div>
      </BottomSheet>
    </div>
  );
}

export const Route = createFileRoute("/test")({
  component: TestPage,
});
