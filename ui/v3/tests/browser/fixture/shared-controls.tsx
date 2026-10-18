import { useState } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import { FolderSelector } from "@/components/filters/filters/folder-filter";
import { ClearableInput } from "@/components/ui/clearable-input";
import { PinButton } from "@/components/ui/pinnable-combo-box";
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
} from "@/components/ui/pagination";
import messages from "@/locales/en-GB.json";
import flattenMessages from "@/utils/flatten-messages";
import "./style.css";

function Fixture() {
  const [query, setQuery] = useState("selected query");
  const [expanded, setExpanded] = useState(false);
  const [pinned, setPinned] = useState(false);
  const [page, setPage] = useState(1);
  return (
    <main className="flex max-w-sm flex-col gap-4 p-4">
      <ClearableInput
        value={query}
        setValue={setQuery}
        placeholder="Search folders"
      />
      <FolderSelector
        folderMap={[
          {
            __typename: "Folder",
            id: "parent",
            basename: "Library",
            path: "/library",
            expanded,
            children: [
              {
                __typename: "Folder",
                id: "child",
                basename: "Nested",
                path: "/library/nested",
                expanded: false,
                children: [],
              },
            ],
          },
        ]}
        onSelect={() => undefined}
        onToggleExpanded={() => setExpanded(!expanded)}
      />
      <PinButton pinned={pinned} onToggle={() => setPinned(!pinned)} />
      <Pagination>
        <PaginationContent>
          {[1, 2].map((number) => (
            <PaginationItem key={number}>
              <PaginationLink
                aria-label={`Page ${number}`}
                isActive={page === number}
                onClick={() => setPage(number)}
              >
                {number}
              </PaginationLink>
            </PaginationItem>
          ))}
          <PaginationItem>
            <PaginationNext disabled={page === 2} onClick={() => setPage(2)} />
          </PaginationItem>
        </PaginationContent>
      </Pagination>
    </main>
  );
}

const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <IntlProvider locale="en-GB" messages={flattenMessages(messages)}>
    <Fixture />
  </IntlProvider>,
);
