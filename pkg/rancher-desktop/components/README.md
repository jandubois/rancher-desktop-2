# Components forked from @rancher/components

These files are our fork of `@rancher/components`, copied from the
`components-v0.3.0-alpha.1` tag (`01c5bb443f`) of
[rancher/dashboard](https://github.com/rancher/dashboard). Our paths are
relative to `pkg/rancher-desktop`, and upstream paths to the root of
rancher/dashboard.

- `components/`: `BadgeState.vue`, `Banner.vue`, `Card.vue`, and `StringList.vue`
- `components/form/`: `Checkbox.vue`, `LabeledInput.vue`, `LabeledTooltip.vue`,
  `RadioButton.vue`, `RadioGroup.vue`, `TextAreaAutoGrow.vue`, and
  `ToggleSwitch.vue`
- `composables/`: `useCompactInput.ts` and `useLabeledFormElement.ts`
- `utils/`: `error.js`

To port an upstream change, look up our copy of each file it touches in this
table.

| Upstream | Ours |
|---|---|
| `pkg/rancher-components/src/components/Form/<Dir>/<Name>.vue` | `components/form/<Name>.vue` |
| `pkg/rancher-components/src/components/LabeledTooltip/LabeledTooltip.vue` | `components/form/LabeledTooltip.vue` |
| `pkg/rancher-components/src/components/<Name>/<Name>.vue` | `components/<Name>.vue` |
| `shell/composables/<name>.ts` | `composables/<name>.ts` |
| `shell/utils/error.js` | `utils/error.js` |

An upstream `<Name>.test.ts` is `__tests__/<Name>.spec.ts` next to our copy of
`<Name>`. In imports, `@shell/` becomes `@pkg/`, `@components/` paths
follow the table, and a test's relative import of its subject (`./index`
or `./<name>`) points at our copy, such as `../Card.vue`.
