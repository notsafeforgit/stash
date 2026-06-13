import {
  type ConfigDataFragment,
  type FilterMode,
  FilterGroupOperator,
  type FindFilterType,
  SortDirectionEnum,
} from "src/core/generated-graphql";
import {
  type Criterion,
  UnsupportedCriterionOption,
} from "./criteria/criterion";
import { getFilterOptions } from "./factory";
import {
  type CriterionType,
  DisplayMode,
  type SavedFilterAST,
  type SavedUIOptions,
} from "./types";
import type { ListFilterOptions } from "./filter-options";
import { CustomFieldsCriterion } from "./criteria/custom-fields";
import {
  type FilterASTNode,
  type FilterASTGroupNode,
  cloneFilterASTNode,
  countFilterASTConditions,
  createASTConditionFromCriterion,
  createConditionNode,
  createASTGroup,
  decodeFilterASTNode,
  decodeSavedFilterASTNode,
  encodeFilterASTNode,
  encodeFilterASTNodeToSaved,
  filterASTNodeToGraphQL,
  astSupportsCriterionType,
  pruneInvalidFilterASTNode,
} from "./filter-ast";

interface DecodedParams {
  perPage?: number;
  sortby?: string;
  sortdir?: string;
  q?: string;
  p?: number;
  c?: string[];
  fa?: string;
}

interface EncodedParams {
  perPage?: string | null;
  sortby?: string | null;
  sortdir?: string | null;
  q?: string | null;
  p?: string | null;
  c?: string[];
  fa?: string | null;
}

const DEFAULT_PARAMS = {
  sortDirection: SortDirectionEnum.Asc,
  displayMode: DisplayMode.Grid,
  currentPage: 1,
  itemsPerPage: 40,
};

// object_filter key the transitional v3 format used to embed the compact
// AST; still read from old saved default-filter config values.
const FILTER_AST_SAVED_KEY = "__filter_ast";

/**
 * Structural input for `configureFromSavedFilter`: covers the
 * SavedFilterData fragment (filter_ast) plus legacy shapes that may still
 * live in config `defaultFilters` values (object_filter with criterion
 * entries and/or an embedded compact AST).
 */
export interface SavedFilterLike {
  find_filter?: Pick<
    FindFilterType,
    "q" | "page" | "per_page" | "sort" | "direction"
  > | null;
  object_filter?: unknown;
  filter_ast?: unknown;
}

export class ListFilterModel {
  public readonly mode: FilterMode;
  public readonly options: ListFilterOptions;
  private config?: ConfigDataFragment;
  public searchTerm: string = "";
  public currentPage = DEFAULT_PARAMS.currentPage;
  public itemsPerPage = DEFAULT_PARAMS.itemsPerPage;
  public sortDirection: SortDirectionEnum = DEFAULT_PARAMS.sortDirection;
  public sortBy?: string;
  public displayMode: DisplayMode = DEFAULT_PARAMS.displayMode;
  public zoomIndex: number = 1;
  public criteria: Criterion[] = [];
  public filterAst?: FilterASTNode;
  /**
   * A filter AST group that is always AND-ed into the query at send time.
   * It represents context-supplied constraints (e.g. "only scenes featuring
   * this performer") that the user must not be able to remove or bypass.
   * It is cloned through `clone()`, preserved across `configureFromSavedFilter`,
   * and excluded from `count()` so it never inflates the filter badge.
   */
  public lockedFilterAst?: FilterASTGroupNode;
  public randomSeed = -1;

  public constructor(
    mode: FilterMode,
    config?: ConfigDataFragment,
    options?: {
      defaultSortBy?: string;
      defaultSortDir?: SortDirectionEnum;
    },
  ) {
    this.mode = mode;
    this.config = config;
    this.options = getFilterOptions(mode);
    const { defaultSortBy, displayModeOptions } = this.options;

    if (options?.defaultSortBy) {
      this.sortBy = options.defaultSortBy;
      if (options.defaultSortDir) {
        this.sortDirection = options.defaultSortDir;
      }
    } else {
      this.sortBy = defaultSortBy;
      if (this.sortBy === "date") {
        this.sortDirection = SortDirectionEnum.Desc;
      }
    }
    this.displayMode = displayModeOptions[0];
  }

  public clone() {
    const ret = Object.assign(
      new ListFilterModel(this.mode, this.config),
      this,
    );
    ret.criteria = this.criteria.map((c) => c.clone());
    if (this.filterAst) {
      ret.filterAst = cloneFilterASTNode(this.filterAst);
    }
    if (this.lockedFilterAst) {
      ret.lockedFilterAst = cloneFilterASTNode(
        this.lockedFilterAst,
      ) as FilterASTGroupNode;
    }
    return ret;
  }

  public empty() {
    return new ListFilterModel(this.mode, this.config);
  }

  // returns a clone of the filter for metadata fetching
  // this removes the sort, page size and page number and zoom index
  public metadataInfo() {
    const clone = this.clone();
    clone.sortBy = undefined;
    clone.randomSeed = -1;
    clone.currentPage = 1;
    clone.sortDirection = DEFAULT_PARAMS.sortDirection;
    clone.itemsPerPage = 0;
    clone.zoomIndex = 1;
    clone.displayMode = DEFAULT_PARAMS.displayMode;
    return clone;
  }

  // returns the number of filters applied (locked criteria excluded)
  public count() {
    return this.criteria.length + countFilterASTConditions(this.filterAst);
  }

  public configureFromDecodedParams(params: DecodedParams) {
    if (params.perPage !== undefined) {
      this.itemsPerPage = params.perPage;
    }
    // Track whether the sortby param was accepted so we can conditionally
    // apply sortdir below.
    let sortbyAccepted = params.sortby === undefined;
    if (params.sortby !== undefined) {
      let sortby = params.sortby;

      // parse the random seed if provided
      const match = sortby.match(/^random_(\d+)$/);
      if (match) {
        sortby = "random";
        this.randomSeed = Number.parseInt(match[1], 10);
      }

      if (this.options.sortByOptions.some((o) => o.value === sortby)) {
        this.sortBy = sortby;
        sortbyAccepted = true;
      }
      // If sortby is not valid for this filter mode, leave both sortBy and
      // sortDirection at their defaults — they go together.
    }
    if (sortbyAccepted && params.sortdir !== undefined) {
      this.sortDirection =
        params.sortdir === "desc"
          ? SortDirectionEnum.Desc
          : SortDirectionEnum.Asc;
    } else if (sortbyAccepted) {
      // #3193 - sortdir undefined means asc
      // #3559 - unless sortby is date, then desc
      this.sortDirection =
        params.sortby === "date"
          ? SortDirectionEnum.Desc
          : SortDirectionEnum.Asc;
    }
    if (params.q !== undefined) {
      this.searchTerm = params.q;
    }
    this.currentPage = params.p ?? 1;

    this.criteria = [];
    if (params.c !== undefined) {
      for (const jsonString of params.c) {
        try {
          const { type: criterionType, ...savedCriterion } =
            JSON.parse(jsonString);

          const criterion = this.makeCriterion(criterionType);
          criterion.fromDecodedParams(savedCriterion);

          this.criteria.push(criterion);
        } catch (err) {
          console.error("Failed to parse encoded criterion:", err);
        }
      }
    }

    if (params.fa !== undefined) {
      try {
        const json = atob(params.fa.replace(/-/g, "+").replace(/_/g, "/"));
        this.filterAst = decodeFilterASTNode(this.mode, JSON.parse(json));
      } catch (err) {
        console.error("Failed to parse encoded filter AST:", err);
      }
    } else {
      this.filterAst = undefined;
    }

    this.promoteLegacyCriteria();
  }

  // Pull AST-supported criteria out of `this.criteria` and into `this.filterAst`
  // so v2.5-shaped URLs and saved filters end up in the AST without round-trip
  // re-emitting the legacy criterion-by-criterion form. Anything the AST can't
  // represent (custom_fields, hidden-only compatibility criteria) stays in
  // `this.criteria` and keeps its dedicated saved-filter slot.
  private promoteLegacyCriteria() {
    if (this.criteria.length === 0) return;

    const astable: Criterion[] = [];
    const remainder: Criterion[] = [];
    for (const c of this.criteria) {
      if (astSupportsCriterionType(this.mode, c.criterionOption.type)) {
        astable.push(c);
      } else {
        remainder.push(c);
      }
    }

    if (astable.length > 0) {
      const conditions = astable.map((c) =>
        createASTConditionFromCriterion(this.mode, c),
      );
      if (
        this.filterAst &&
        this.filterAst.kind === "group" &&
        this.filterAst.operator === FilterGroupOperator.And
      ) {
        this.filterAst = {
          ...this.filterAst,
          children: [...this.filterAst.children, ...conditions],
        };
      } else if (this.filterAst) {
        this.filterAst = createASTGroup(this.mode, FilterGroupOperator.And, [
          this.filterAst,
          ...conditions,
        ]);
      } else {
        this.filterAst = createASTGroup(
          this.mode,
          FilterGroupOperator.And,
          conditions,
        );
      }
    }

    this.criteria = remainder;
  }

  // Does not decode any URL-encoding, only type conversions
  public static decodeParams(params: EncodedParams): DecodedParams {
    const ret: DecodedParams = {};

    if (params.perPage) {
      ret.perPage = Number.parseInt(params.perPage, 10);
    }
    if (params.sortby) {
      ret.sortby = params.sortby;
    }
    if (params.sortdir) {
      ret.sortdir = params.sortdir;
    }
    if (params.q) {
      ret.q = params.q;
    }
    if (params.p) {
      ret.p = Number.parseInt(params.p, 10);
    }
    if (params.c && params.c.length !== 0) {
      ret.c = params.c.map((jsonString) =>
        ListFilterModel.translateJSON(jsonString, true),
      );
    }

    if (params.fa) {
      ret.fa = params.fa;
    }

    return ret;
  }

  private static translateJSON(jsonString: string, decoding: boolean) {
    let inString = false;
    let escaped = false;
    return [...jsonString]
      .map((c) => {
        if (escaped) {
          // this character has been escaped, skip
          escaped = false;
          return c;
        }

        switch (c) {
          case "\\":
            // escape the next character if in a string
            if (inString) {
              escaped = true;
            }
            break;
          case '"':
            // unescaped quote, toggle inString
            inString = !inString;
            break;
          case "(":
            // decode only: restore ( to { if not in a string
            if (decoding && !inString) {
              return "{";
            }
            break;
          case ")":
            // decode only: restore ) to } if not in a string
            if (decoding && !inString) {
              return "}";
            }
            break;
          case "{":
            // encode only: replace { with ( if not in a string
            if (!decoding && !inString) {
              return "(";
            }
            break;
          case "}":
            // encode only: replace } with ) if not in a string
            if (!decoding && !inString) {
              return ")";
            }
            break;
        }

        return c;
      })
      .join("");
  }

  public configureFromQueryString(queryString: string) {
    const query = new URLSearchParams(queryString);
    const params = {
      perPage: query.get("perPage"),
      sortby: query.get("sortby"),
      sortdir: query.get("sortdir"),
      q: query.get("q"),
      p: query.get("p"),
      c: query.getAll("c"),
      fa: query.get("fa"),
    };
    const decoded = ListFilterModel.decodeParams(params);
    this.configureFromDecodedParams(decoded);
  }

  public configureFromSavedFilter(savedFilter: SavedFilterLike) {
    const { find_filter: findFilter, object_filter: objectFilter } =
      savedFilter;

    this.itemsPerPage =
      typeof findFilter?.per_page === "string"
        ? Number.parseInt(findFilter.per_page, 10)
        : (findFilter?.per_page ?? this.itemsPerPage);
    this.sortBy = findFilter?.sort ?? this.sortBy;
    // parse the random seed if provided
    const match = this.sortBy?.match(/^random_(\d+)$/);
    if (match) {
      this.sortBy = "random";
      this.randomSeed = Number.parseInt(match[1], 10);
    }
    this.sortDirection = findFilter?.direction ?? this.sortDirection;
    this.searchTerm = findFilter?.q ?? this.searchTerm;

    // Display mode and zoom are per-view localStorage prefs and are not
    // restored from saved filters — see `useDisplayModePref` /
    // `useZoomPref`.

    this.currentPage = 1;

    this.criteria = [];
    this.filterAst = undefined;

    const rawAst = (savedFilter.filter_ast as { root?: unknown } | null)?.root;
    if (rawAst) {
      // canonical form: the whole filter is one AST; conditions the builder
      // can't edit are split back out into `this.criteria`
      const decoded = decodeSavedFilterASTNode(rawAst, (type) =>
        this.makeCriterion(type),
      );
      this.splitNonBuilderConditions(decoded);
    } else if (objectFilter) {
      // legacy shapes, still found in old config defaultFilters values:
      // criterion entries keyed by type plus an optional embedded compact AST
      const obj = objectFilter as Record<string, unknown>;
      const astValue = obj[FILTER_AST_SAVED_KEY];

      if (astValue) {
        this.filterAst = decodeFilterASTNode(this.mode, astValue);
      }

      for (const [k, v] of Object.entries(obj)) {
        if (k === FILTER_AST_SAVED_KEY) continue;
        const criterion = this.makeCriterion(k as CriterionType);
        criterion.setFromSavedCriterion(v);
        this.criteria.push(criterion);
      }
    }

    this.promoteLegacyCriteria();
  }

  // Inverse of the fold in `makeFilterAst`: non-builder conditions
  // (custom_fields, hidden compatibility criteria) at the root AND level
  // move back into `this.criteria`; everything else stays in the AST.
  private splitNonBuilderConditions(root: FilterASTNode) {
    const isBuilderCondition = (node: FilterASTNode) =>
      node.kind !== "condition" ||
      astSupportsCriterionType(this.mode, node.field);

    if (root.kind === "condition") {
      if (isBuilderCondition(root)) {
        this.filterAst = root;
      } else {
        this.criteria.push(root.criterion);
      }
      return;
    }

    if (root.operator === FilterGroupOperator.And) {
      const keep: FilterASTNode[] = [];
      for (const child of root.children) {
        if (isBuilderCondition(child)) {
          keep.push(child);
        } else if (child.kind === "condition") {
          this.criteria.push(child.criterion);
        }
      }
      if (keep.length > 0) {
        this.filterAst = { ...root, children: keep };
      }
      return;
    }

    // non-builder conditions cannot occur below an OR (the fold only adds
    // them at the root AND level); keep the tree as-is
    this.filterAst = root;
  }

  private setRandomSeed() {
    if (this.sortBy === "random") {
      // #321 - set the random seed if it is not set
      if (this.randomSeed === -1) {
        // generate 8-digit seed
        this.randomSeed = Math.floor(Math.random() * 10 ** 8);
      }
    } else {
      this.randomSeed = -1;
    }
  }

  private getSortBy(): string | undefined {
    this.setRandomSeed();

    if (this.sortBy === "random") {
      return `random_${this.randomSeed.toString()}`;
    }

    return this.sortBy;
  }

  // Returns query parameters with necessary parts URL-encoded
  public getEncodedParams(): EncodedParams {
    const encodedCriteria: string[] = this.criteria.map((criterion) => {
      const queryParams = criterion.toQueryParams();
      let str = ListFilterModel.translateJSON(
        JSON.stringify(queryParams),
        false,
      );

      // URL-encode other characters
      str = encodeURI(str);

      // only the reserved characters ?#&;=+ need to be URL-encoded
      // as they have special meaning in query strings
      str = str.replaceAll("?", encodeURIComponent("?"));
      str = str.replaceAll("#", encodeURIComponent("#"));
      str = str.replaceAll("&", encodeURIComponent("&"));
      str = str.replaceAll(";", encodeURIComponent(";"));
      str = str.replaceAll("=", encodeURIComponent("="));
      str = str.replaceAll("+", encodeURIComponent("+"));

      return str;
    });

    let encodedFilterAST: string | undefined;
    if (this.filterAst) {
      // Base64url: JSON.stringify produces pure ASCII so btoa is safe.
      // Swap standard base64 chars (+, /) for URL-safe equivalents (-, _)
      // and strip = padding (restored on decode by atob's tolerance).
      encodedFilterAST = btoa(
        JSON.stringify(encodeFilterASTNode(this.filterAst)),
      )
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=/g, "");
    }

    return {
      perPage:
        this.itemsPerPage !== DEFAULT_PARAMS.itemsPerPage
          ? String(this.itemsPerPage)
          : undefined,
      sortby: this.getSortBy(),
      sortdir:
        this.sortBy === "date"
          ? this.sortDirection === SortDirectionEnum.Asc
            ? "asc"
            : undefined
          : this.sortDirection === SortDirectionEnum.Desc
            ? "desc"
            : undefined,
      q: this.searchTerm ? encodeURIComponent(this.searchTerm) : undefined,
      p:
        this.currentPage !== DEFAULT_PARAMS.currentPage
          ? String(this.currentPage)
          : undefined,
      c: encodedCriteria,
      fa: encodedFilterAST,
    };
  }

  public makeQueryParameters(): string {
    const query: string[] = [];
    const params = this.getEncodedParams();

    if (params.q) {
      query.push(`q=${params.q}`);
    }
    if (params.c) {
      for (const c of params.c) {
        query.push(`c=${c}`);
      }
    }
    if (params.fa) {
      query.push(`fa=${params.fa}`);
    }
    if (params.sortby) {
      query.push(`sortby=${params.sortby}`);
    }
    if (params.sortdir) {
      query.push(`sortdir=${params.sortdir}`);
    }
    if (params.perPage) {
      query.push(`perPage=${params.perPage}`);
    }
    if (params.p) {
      query.push(`p=${params.p}`);
    }

    return query.join("&");
  }

  public makeCriterion(type: CriterionType) {
    const { criterionOptions } = getFilterOptions(this.mode);

    const option = criterionOptions.find((o) => o.type === type);

    if (!option) {
      return new UnsupportedCriterionOption(type).makeCriterion(this.config);
    }

    return option.makeCriterion(this.config);
  }

  public makeFindFilter(): FindFilterType {
    return {
      q: this.searchTerm,
      page: this.currentPage,
      per_page: this.itemsPerPage,
      sort: this.getSortBy(),
      direction: this.sortDirection,
    };
  }

  public makeFilter() {
    // When any AST filter is active (user or locked), filter conditions are
    // sent exclusively via makeFilterAST(); the legacy object filter is unused.
    if (this.filterAst || this.lockedFilterAst) {
      return {};
    }

    const output: Record<string, unknown> = {};
    for (const c of this.criteria) {
      c.applyToCriterionInput(output);
    }
    return output;
  }

  private makeFilterASTNode(includeLocked: boolean) {
    let root: FilterASTNode | undefined = pruneInvalidFilterASTNode(
      this.filterAst ? cloneFilterASTNode(this.filterAst) : undefined,
    );

    const conditions = this.criteria
      .filter((c) => c.isValid())
      .map((c) => createConditionNode(c));
    if (conditions.length > 0) {
      if (
        root &&
        root.kind === "group" &&
        root.operator === FilterGroupOperator.And
      ) {
        root = { ...root, children: [...root.children, ...conditions] };
      } else if (root) {
        root = createASTGroup(this.mode, FilterGroupOperator.And, [
          root,
          ...conditions,
        ]);
      } else if (conditions.length === 1) {
        root = conditions[0];
      } else {
        root = createASTGroup(this.mode, FilterGroupOperator.And, conditions);
      }
    }

    if (includeLocked) {
      const locked = pruneInvalidFilterASTNode(
        this.lockedFilterAst
          ? cloneFilterASTNode(this.lockedFilterAst)
          : undefined,
      );
      if (locked && root) {
        root = createASTGroup(this.mode, FilterGroupOperator.And, [
          locked,
          root,
        ]);
      } else if (locked) {
        root = locked;
      }
    }

    return root;
  }

  public makeFilterAST() {
    const root = this.makeFilterASTNode(true);

    if (!root) {
      return undefined;
    }

    return { root: filterASTNodeToGraphQL(root) };
  }

  /**
   * Builds the canonical `filter_ast` payload for SaveFilterInput (and the
   * config defaultFilters values): the builder AST with any non-builder
   * criteria folded into the root AND group. Returns undefined when the
   * filter has no criteria.
   */
  public makeFilterAst(): SavedFilterAST | undefined {
    const root = this.makeFilterASTNode(false);

    if (!root) return undefined;
    return { root: encodeFilterASTNodeToSaved(root) };
  }

  public makeSavedUIOptions(): SavedUIOptions {
    // Saved filters carry no UI prefs — see `SavedUIOptions`.
    return {};
  }

  public criteriaFor(type: CriterionType) {
    return this.criteria.filter((c) => c.criterionOption.type === type);
  }

  public replaceCriteria(type: CriterionType, newCriteria: Criterion[]) {
    const criteria = [
      ...this.criteria.filter((c) => c.criterionOption.type !== type),
      ...newCriteria,
    ];

    return this.setCriteria(criteria);
  }

  public clearCriteria(clearSearchTerm = false) {
    const ret = this.clone();
    if (clearSearchTerm) {
      ret.searchTerm = "";
    }
    ret.criteria = [];
    ret.filterAst = undefined;
    ret.currentPage = 1;
    return ret;
  }

  public clearSearchTerm() {
    const ret = this.clone();
    ret.searchTerm = "";
    ret.currentPage = 1; // reset to first page
    return ret;
  }

  public setCriteria(criteria: Criterion[]) {
    const ret = this.clone();
    ret.criteria = criteria;
    return ret;
  }

  public removeCriterion(type: CriterionType) {
    const ret = this.clone();
    const c = ret.criteria.find((cc) => cc.criterionOption.type === type);

    if (!c) return ret;

    const newCriteria = ret.criteria.filter((cc) => {
      return cc.getId() !== c.getId();
    });

    ret.criteria = newCriteria;
    ret.currentPage = 1;
    return ret;
  }

  public removeCustomFieldCriterion(type: CriterionType, index: number) {
    const ret = this.clone();
    const c = ret.criteria.find((cc) => cc.criterionOption.type === type);

    if (!c) return ret;

    if (c instanceof CustomFieldsCriterion) {
      const newCriteria = c.value.filter((_, i) => i !== index);
      c.value = newCriteria;
    }

    return ret;
  }

  public setPageSize(pageSize: number) {
    const ret = this.clone();
    ret.itemsPerPage = pageSize;
    ret.currentPage = 1; // reset to first page
    return ret;
  }

  public setSortBy(sortBy: string | undefined) {
    const ret = this.clone();
    ret.sortBy = sortBy;
    ret.currentPage = 1; // reset to first page
    // Picking "random" from the sort dropdown — including re-picking
    // it while already on random — should produce a fresh shuffle.
    // Clear the seed so the next `setRandomSeed` generates a new one.
    // The user gets re-select-to-reshuffle as a natural affordance.
    if (sortBy === "random") {
      ret.randomSeed = -1;
    }
    return ret;
  }

  // `toggleSortDirection` deliberately does NOT touch `randomSeed`.
  // Flipping asc/desc on a random sort reverses the order of the same
  // shuffled sequence; reseeding would also reshuffle, which is not
  // what "flip direction" should mean.
  public toggleSortDirection() {
    const ret = this.clone();

    if (ret.sortDirection === SortDirectionEnum.Asc) {
      ret.sortDirection = SortDirectionEnum.Desc;
    } else {
      ret.sortDirection = SortDirectionEnum.Asc;
    }

    ret.currentPage = 1; // reset to first page
    return ret;
  }

  public reshuffleRandomSort() {
    const ret = this.clone();
    ret.currentPage = 1;
    ret.randomSeed = -1;
    return ret;
  }

  public changePage(page: number) {
    const ret = this.clone();
    ret.currentPage = page;
    return ret;
  }

  public setZoom(zoomIndex: number) {
    const ret = this.clone();
    ret.zoomIndex = zoomIndex;
    return ret;
  }

  public setDisplayMode(displayMode: DisplayMode) {
    const ret = this.clone();
    ret.displayMode = displayMode;
    return ret;
  }
}
