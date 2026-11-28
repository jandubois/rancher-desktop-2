import { EditorView } from '@codemirror/view';
import { mount } from '@vue/test-utils';

import ScriptEditor from '@pkg/components/form/ScriptEditor.vue';

describe('ScriptEditor', () => {
  function mountEditor(props: { modelValue: string, disabled?: boolean }) {
    const wrapper = mount(ScriptEditor, { props, attachTo: document.body });
    const view = EditorView.findFromDOM(wrapper.find('.cm-editor').element as HTMLElement);

    if (!view) {
      throw new Error('ScriptEditor did not create an EditorView');
    }

    return { wrapper, view };
  }

  test('shows the initial script', () => {
    const { wrapper } = mountEditor({ modelValue: 'echo one\necho two' });

    expect(wrapper.find('.cm-content').text()).toContain('echo one');
    expect(wrapper.find('.cm-content').text()).toContain('echo two');
    wrapper.unmount();
  });

  test('emits the document after an edit', () => {
    const { wrapper, view } = mountEditor({ modelValue: 'echo' });

    view.dispatch({ changes: { from: 4, insert: ' hi' } });

    expect(wrapper.emitted('update:modelValue')).toEqual([['echo hi']]);
    wrapper.unmount();
  });

  test('replaces the document when modelValue changes, without emitting it', async() => {
    const { wrapper, view } = mountEditor({ modelValue: 'old' });

    await wrapper.setProps({ modelValue: 'new' });

    expect(view.state.doc.toString()).toBe('new');
    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    wrapper.unmount();
  });

  test('is read-only while disabled and editable once enabled', async() => {
    const { wrapper, view } = mountEditor({ modelValue: 'x', disabled: true });

    expect(view.state.readOnly).toBe(true);
    expect(wrapper.find('.cm-content').attributes('contenteditable')).toBe('false');

    await wrapper.setProps({ disabled: false });

    expect(view.state.readOnly).toBe(false);
    expect(wrapper.find('.cm-content').attributes('contenteditable')).toBe('true');
    wrapper.unmount();
  });
});
