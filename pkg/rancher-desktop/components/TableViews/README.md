# Table views from rancher/dashboard

The query box and the View menu on our tables come from the table views in
[rancher/dashboard](https://github.com/rancher/dashboard). This file records
what we took and what we changed, so that a later merge can start here. It is
separate from `components/README.md` because that file describes the
`components-v0.3.0-alpha.1` tag, and these files came from a `master` commit.

## Base

We copied commit `775e0a1166c3ab56ff42d9a538695cc2228ae487`, committed
2026-10-30.
That is unreleased `master`, and upstream was still changing the feature then.

The commit `Copy the table views code from rancher/dashboard` holds the files
as upstream had them, so a diff against it shows everything we changed. A rebase
changes its hash, so find it by subject:

```console
git log --format=%H --grep='^Copy the table views code from rancher/dashboard$'
```

## Path map

Our paths are relative to `pkg/rancher-desktop`, and upstream paths to the root
of rancher/dashboard. `RC` stands for `pkg/rancher-components/src/components`.

| Upstream | Ours |
|---|---|
| `RC/Rc{Button,Icon,Separator}/<Name>.vue` | `components/<Name>.vue` |
| `RC/RcDropdown/<Name>.vue` | `components/<Name>.vue` |
| `RC/RcDropdown/use*.ts` | `composables/use*.ts` |
| `RC/Rc{Button,Dropdown,Icon,Separator}/types.ts` | `types/Rc{Button,Dropdown,Icon,Separator}.ts` |
| `RC/_variables.scss` | `components/_variables.scss` |
| `RC/utils/status.ts` | `utils/status.ts` |
| `shell/components/TableViews/<file>` | `components/TableViews/<file>` |
| `shell/composables/{useClickOutside,useDragReorder}.ts` | `composables/<name>.ts` |
| `shell/types/table-views.ts` | `types/table-views.ts` |
| `shell/utils/table-columns.ts` | `utils/table-columns.ts` |
| `shell/utils/table-views/<name>.ts` | `utils/table-views/<name>.ts` |

An upstream `<name>.test.ts` is `__tests__/<name>.spec.ts` next to our copy.

## Changes to the copies

- **Imports** follow the path map. Barrels (`RcDropdown`, `RcButton`, `RcIcon`,
  `Form/Checkbox`, `utils/table-views`) became direct imports. The `RcDropdown`
  barrel would also pull in LabeledSelect.
- **Instance id:** `TableViewQueryInput.vue` gets it from `useId()`. Upstream's
  `let uid` is inside `<script setup>`, so it restarts per instance and every
  box gets the same menu id.
- **Clear button:** it has `role-link`, because `rancher-desktop.scss` draws a
  red outline around a button with neither a `role-*` nor a `variant-` class.
- **Escaping:** `TableViewControls.vue` runs `text`, `label` and `fields`
  through `escapeHtml` before they reach `v-clean-html`, because our translations
  README makes that the caller's job.
- **`config/table-headers`:** `fields.ts` and `filter-rows.ts` use literal
  `'state'` and `'name'`. The import supplied only those two names and pulled
  in 120 files.
- **Types:** `fields.ts` and `types/table-views.ts` no longer import
  `@shell/core/types`, and the server-pagination types are gone.
  `TableViewColumn` is a standalone interface.
- **Server-side code:** `serverPathFor`, `summaryToValues`, `rawFieldValue` and
  the `paginationHeaders` parameter are gone. Without `paginationHeader`,
  `rawFieldValue` always equals `fieldValue`.
- **Icons:** `icon-notify-warning` and `icon-notify-info` became `icon-warning`
  and `icon-info`, because `rancher-icons` v2.0.21 lacks the `notify` names.
  `types/RcIcon.ts` still maps them, but nothing uses those entries.
- **`t`:** `composables/useI18n.ts` replaces upstream's
  `shell/composables/useI18n.ts`. It reads `i18n/t`, because our `t` does not
  take a `raw` argument.
- **`RcButton.vue`** disables `no-deprecated` for its own deprecated props.
- **Specs:** `table-views.spec.ts` lost the tests of `server-filters`, `export`
  and the functions above.
- **`unsupportedFields`** stays in `TableViewControls.vue` though nothing sets
  it, so that a merge there stays simple.
- **View menu values:** `TableViewControls.vue` gives `.menu-nav-value`
  `--dropdown-hover-text` on a highlighted row, because `--muted` is
  unreadable on our `--dropdown-hover-bg`.

## Left out

We took only what the query box and the View menu need, so a merge can skip
these upstream files:

- `shell/components/TableViews/`: `TableViewTabs.vue`,
  `TableSelectionActions.vue`, `TableViewExportModal.vue`,
  `TableViewPublishModal.vue` and `TableViewShareModal.vue`
- `shell/utils/table-views/`: every file that we did not copy, such as
  `export.ts`, `server-filters.ts` and `share.ts`
- `RcIconTooltip`, which only upstream's newer `Form/Checkbox/Checkbox.vue`
  reaches. We keep our own `components/form/Checkbox.vue`.

## Ported by hand

`ViewTable.vue` replaces `shell/mixins/resource-table-views.js` and the view
handling in `shell/components/ResourceTable.vue`. Line numbers drift, so this
table names upstream's functions.

| Upstream | Dropped or changed |
|---|---|
| `viewFields`, `viewQueryFields` | Pagination headers. `viewQueryFields` became `viewFields`, because we don't have `queryFields` or `freeTextWhenShown`. |
| `viewSortableFields`, `viewDateFields` | Server-side branches |
| `viewGroupFields` | Uses `option.label` in place of `tableGroupingLabel` |
| `viewFilterFields` | Query-only fields |
| `viewQuery`, `viewRows` | Server-side branch and `expandQuery` |
| `headersForView`, our `viewHeaders` | The Age lookup, so label columns go last |
| `groupSortFor`, our `viewGroupSort` | The `serverPathFor` fallback and the fallback to a `groupSort` prop. Returns an array, as our `groupSort` prop expects. |
| `settledQuery` watcher | State link handling |
| `applyViewSort` | Only returns to the default sort when the sorted column is hidden |
| `groupByOf`, `groupFieldFor`, `viewGroupField`, `monthGrouping`, `defaultColumnIds`, `coreColumnIds` | Schema and `this.group` branches |
| `computedGroupBy`, `computedGroupRef` in `ResourceTable.vue` | Schema and `this.group` branches |

We took these by hand too:

- `SortableTable`: the `queried` prop with its `noResults()` check, and the
  function forms of `groupBy` and `groupRef` in `grouping.js` and `sorting.js`.
  The rest of our fork stays at its own base.
- `assets/styles/base/_mixins.scss`: `focus-outline`, without upstream's comment.
- `assets/styles/themes/_light.scss` and `_dark.scss`: `--primary-keyboard-focus`,
  which upstream defines in `themes/_modern.scss`.
- `entry/plugins.ts`: `[contenteditable]` in the shortkey exemptions, from
  `SHORTKEY_PREVENT_SELECTORS` in `shell/utils/dom.js`.
- `assets/translations/en-us.yaml`: the `tableViews` keys that the code
  references, because `i18n-report check` fails on unused keys. Tags are quoted,
  as in `'<b>'{text}'</b>'`, because our formatter does not set `ignoreTag`.

## Our own code

- `ViewTable.vue` has a `viewMenu` prop. A table that sets it to false gets the
  query box alone.
- `utils/table-views/storage.ts` keeps each table's view in `localStorage`
  under one key. Upstream's `views` and `defaultViewId` would fit beside `view`
  in that format, if we add saved views.
- `assets/styles/rancher-desktop.scss`: the rule behind the red outline also
  skips `RcButton.vue`'s `variant-` classes.
- `components/SortableTable/index.vue` drops the search column from its header
  grid when there is no search cell, so `header-middle` content reaches the
  right edge.
- `ViewTable.spec.ts`, `TableViewControls.spec.ts` and `storage.spec.ts` don't
  have an upstream counterpart.

## Merging

1. In a rancher/dashboard checkout, run `git diff <base>..upstream/master` over
   the upstream paths in the path map and under "Ported by hand", and read how
   the functions in that section changed.
2. Apply those changes through the path map.
3. Run the specs.
4. Update the commit under "Base".

Any commit that changes our delta updates this file too.
