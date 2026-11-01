/** The shapes the table views engine (@pkg/utils/table-views) takes and returns */

/** A table row. Any resource can be one, so only the labels a view reads directly are typed */
export interface TableViewRow {
  metadata?: {
    labels?:       Record<string, string>;
    [key: string]: unknown;
  };
  /** The model's own state name, eg `in-progress`, which the State column shows as "In Progress" */
  state?: unknown;
}

/** A table column: the header fields the table views engine reads */
export interface TableViewColumn {
  /**
   * Name of the header. This should be unique.
   */
  name?: string;

  /**
   * `false` for a column that isn't data, eg a button: table views leaves it out of Columns, Group
   * By and the query
   */
  tableViews?: boolean;

  /**
   * A string that will show in the table column as a header
   */
  label?: string;

  /**
   * A translation key where the resulting string will show in the table column as a header
   */
  labelKey?: string;

  /**
   * How the cell's value is resolved: either a string path into the row (i.e. `metadata.name`), or
   * a function given the row that returns the value to display.
   */
  value?: string | ((row: TableViewRow) => unknown);

  /**
   * A string which represents the path to access the value from the row object which we'll use to sort i.e. `row.meta.value`
   */
  sort?: string | string[] | boolean;

  /**
   * A string which represents the path to access the value from the row object which we'll use to search i.e. `row.meta.value`.
   * A column can name more than one path, as `sort` can. It can be false to disable searching on this field
   */
  search?: string | boolean | string[];

  /**
   * The name of a custom formatter.
   */
  formatter?: string;

  /**
   * Provide a function which accepts a row and returns the value that should be displayed in the column
   * @param row This can be any value which represents the row
   * @returns Can return {@link string | number | null | undefined} to display in the column
   */
  getValue?: (row: TableViewRow) => string | number | null | undefined;
}

export interface TableViewField {
  /** Token used in the query, eg `namespace` or `label:app` */
  id:             string;
  label:          string;
  isLabel:        boolean;
  labelKey?:      string;
  /** Holds moments in time: filtered and suggested as dates, grouped by only by month */
  isDate?:        boolean;
  /** The Group By entry that gathers a date column's rows by the month they fall in */
  byMonth?:       boolean;
  header?:        TableViewColumn;
  /** Only for the query: not a column, not grouped by, not searched by free text */
  queryOnly?:     boolean;
  /** Left out of free text for now, eg the description while its column isn't on the table */
  notInFreeText?: boolean;
  /** Matched whole rather than as a part, eg an id */
  exact?:         boolean;
  /** The values it takes, suggested as they are rather than read from the rows */
  values?:        TableViewValueSuggestion[];
  /** Rewrites a group's terms on it into terms on other fields, eg `pinned:true` into the pinned clusters' ids */
  expand?:        (terms: TableViewTerm[]) => TableViewTerm[];
}

export interface TableViewTerm {
  /** Field id, or null for free text */
  field:   string | null;
  value:   string;
  negated: boolean;
}

/** Terms with no joining word between them: the same field means either, different fields mean both */
export type TableViewGroup = TableViewTerm[];

export interface TableViewClause {
  groups: TableViewGroup[];
}

/** Clauses joined by `or`. `and` binds tighter, so `a or b and c` is `a or (b and c)` */
export interface TableViewQuery {
  clauses: TableViewClause[];
}

/** What a table shows: a view's query, columns, grouping and sort */
export interface TableViewState {
  query:           string;
  /** null means the table's default columns */
  columns:         string[] | null;
  /** null means the table's own order */
  columnOrder?:    string[] | null;
  labelColumns:    string[];
  groupBy:         string | null;
  /** null means the table's own sort */
  sort?:           string | null;
  sortDescending?: boolean;
}

/**
 * A view as stored. It is saved without its null and empty string properties, so a missing one means
 * the same as those
 */
export interface TableViewSaved extends Partial<TableViewState> {
  id:   string;
  name: string;
}

/**
 * A page's views, which one opens first and where the table's own tab sits: what a user's
 * preferences and a Page resource both keep for a page
 */
export interface TableViewsPageSettings {
  views?:         TableViewSaved[];
  defaultViewId?: string | null;
  allIndex?:      number;
}

export interface TableViewAction {
  action:   string;
  label?:   string;
  icon?:    string;
  svg?:     string;
  enabled?: boolean;
}

export interface TableViewQueryToken {
  start: number;
  end:   number;
  text:  string;
}

/**
 * A term resolved against the fields and located in the text. `state: active` spans two chunks, so
 * the autocomplete and highlighter need this rather than raw tokens
 */
export interface TableViewQueryTerm extends TableViewQueryToken {
  kind:       'term' | 'connective';
  /** The `-` or `!` prefix, if any. Empty for a spelled out `not`, which is its own token */
  negate:     string;
  negated:    boolean;
  field:      TableViewField | null;
  fieldText:  string;
  value:      string;
  valueStart: number;
}

/** `value-unknown` is a value the field doesn't have, eg one half typed */
export type TableViewQuerySegmentKind = 'field' | 'value' | 'value-unknown' | 'connective' | 'text' | 'plain';

export interface TableViewQuerySegment {
  text: string;
  kind: TableViewQuerySegmentKind;
}

export type TableViewQueryProblemKind =
  'emptyValue' | 'trailingOperator' | 'leadingJoiner' | 'consecutiveOperators' |
  'danglingNegation' | 'unbalancedQuote' | 'noTerms';

export interface TableViewQueryProblem {
  kind:   TableViewQueryProblemKind;
  start:  number;
  end:    number;
  text:   string;
  label?: string;
}

export interface TableViewValueSuggestion {
  value: string;
  count: number;
}
