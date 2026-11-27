<script lang="ts" setup>
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from '@codemirror/language';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { Annotation, Compartment, EditorState } from '@codemirror/state';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';

defineOptions({ name: 'script-editor' });

const props = withDefaults(defineProps<{
  modelValue: string;
  disabled?:  boolean;
  ariaLabel?: string;
}>(), { disabled: false, ariaLabel: undefined });

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();

const container = ref<HTMLElement>();
const editable = new Compartment();
/** Marks the transactions that load a new modelValue, so the update listener does not emit them back as edits. */
const externalUpdate = Annotation.define<boolean>();
let view: EditorView | undefined;

const theme = EditorView.theme({
  '&': {
    height:          '100%',
    color:           'var(--input-text)',
    backgroundColor: 'var(--input-bg)',
    border:          '1px solid var(--input-border)',
    borderRadius:    'var(--border-radius)',
  },
  '&.cm-focused': {
    outline:     'none',
    borderColor: 'var(--outline)',
  },
  '.cm-scroller': {
    overflow:   'auto',
    fontFamily: "'Roboto Mono', monospace",
  },
  '.cm-content': { caretColor: 'var(--input-text)' },
  '.cm-gutters': {
    color:           'var(--disabled-text)',
    backgroundColor: 'var(--box-bg)',
    borderRight:     '1px solid var(--input-border)',
  },
});

// Commands keep the text color. The shell mode recognizes only a fixed list
// of common commands (echo, but not sysctl), so coloring them would look
// arbitrary.
const highlightStyle = HighlightStyle.define([
  { tag: [tags.comment, tags.meta], color: 'var(--script-comment)' },
  { tag: tags.keyword, color: 'var(--script-keyword)' },
  { tag: tags.string, color: 'var(--script-string)' },
  { tag: tags.definition(tags.variableName), color: 'var(--script-variable)' },
  { tag: [tags.number, tags.atom], color: 'var(--script-constant)' },
]);

function editableExtension(disabled: boolean) {
  return [EditorView.editable.of(!disabled), EditorState.readOnly.of(disabled)];
}

onMounted(() => {
  view = new EditorView({
    parent: container.value,
    state:  EditorState.create({
      doc:        props.modelValue,
      extensions: [
        lineNumbers(),
        history(),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        StreamLanguage.define(shell),
        syntaxHighlighting(highlightStyle),
        theme,
        editable.of(editableExtension(props.disabled)),
        EditorView.contentAttributes.of(props.ariaLabel ? { 'aria-label': props.ariaLabel } : {}),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && update.transactions.some(tr => !tr.annotation(externalUpdate))) {
            emit('update:modelValue', update.state.doc.toString());
          }
        }),
      ],
    }),
  });
});

onBeforeUnmount(() => {
  view?.destroy();
  view = undefined;
});

watch(() => props.modelValue, (value) => {
  if (view && value !== view.state.doc.toString()) {
    view.dispatch({
      changes:     { from: 0, to: view.state.doc.length, insert: value },
      annotations: externalUpdate.of(true),
    });
  }
});

watch(() => props.disabled, (disabled) => {
  view?.dispatch({ effects: editable.reconfigure(editableExtension(disabled)) });
});
</script>

<template>
  <div
    ref="container"
    class="script-editor"
    :class="{ disabled }"
  />
</template>

<style lang="scss" scoped>
  // Syntax colors from GitHub's Primer palette, in a light and a dark variant.
  // The app's theme variables have too few distinct hues for syntax
  // highlighting, and their yellow, --warning, is hard to read on the light
  // theme's white background.
  .script-editor {
    --script-comment: #59636e;
    --script-keyword: #cf222e;
    --script-string: #0a3069;
    --script-variable: #953800;
    --script-constant: #0550ae;

    height: 100%;
    min-height: 0;

    &.disabled :deep(.cm-editor) {
      color: var(--input-disabled-text);
      background-color: var(--input-disabled-bg);
    }
  }

  @media (prefers-color-scheme: dark) {
    .script-editor {
      --script-comment: #9198a1;
      --script-keyword: #ff7b72;
      --script-string: #a5d6ff;
      --script-variable: #ffa657;
      --script-constant: #79c0ff;
    }
  }
</style>
