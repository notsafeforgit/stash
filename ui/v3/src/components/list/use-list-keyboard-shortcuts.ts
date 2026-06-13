import { useListShortcutScope } from "src/components/shortcut-provider";

export interface ListKeyboardShortcutsProps {
  currentPage?: number;
  pages?: number;
  onChangePage?: (page: number) => void;
  showEditFilter?: () => void;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
  onInvertSelection?: () => void;
  selectModeActive?: boolean;
  /** When true, all shortcuts are suppressed (e.g. while a lightbox is open). */
  disabled?: boolean;
}

export function useListKeyboardShortcuts(props: ListKeyboardShortcutsProps) {
  useListShortcutScope(props);
}
