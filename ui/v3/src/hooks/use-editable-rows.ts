import { useState } from "react";

interface Row<T> {
  key: number;
  value: T;
}

/** Stable row identity across edits and removals, owned by this form instance.
 * External resets preserve existing positions and assign fresh keys to additions. */
export function useEditableRows<T>(value: T[], onChange: (value: T[]) => void) {
  const [state, setState] = useState(() => ({
    source: value,
    rows: value.map((item, key) => ({ key, value: item })),
    nextKey: value.length,
  }));
  if (state.source !== value) {
    let nextKey = state.nextKey;
    const rows = value.map((item, index) => ({
      key: state.rows[index]?.key ?? nextKey++,
      value: item,
    }));
    setState({ source: value, rows, nextKey });
  }

  function commit(rows: Row<T>[], nextKey = state.nextKey) {
    const source = rows.map((row) => row.value);
    setState({ source, rows, nextKey });
    onChange(source);
  }

  return {
    rows: state.rows,
    update: (index: number, item: T) =>
      commit(
        state.rows.map((row, position) =>
          position === index ? { ...row, value: item } : row,
        ),
      ),
    remove: (index: number) =>
      commit(state.rows.filter((_, position) => position !== index)),
    append: (item: T) =>
      commit(
        [...state.rows, { key: state.nextKey, value: item }],
        state.nextKey + 1,
      ),
  };
}
