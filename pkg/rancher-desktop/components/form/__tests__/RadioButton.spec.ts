import { shallowMount } from '@vue/test-utils';

import RadioButton from '../RadioButton.vue';

import cleanHtmlDirective from '@pkg/plugins/clean-html-directive';

const global = { plugins: [cleanHtmlDirective], stubs: { t: true } };

describe('radioButton.vue', () => {
  it('renders label slot contents', () => {
    const wrapper = shallowMount(RadioButton, { slots: { label: 'Test Label' }, propsData: { val: {}, value: {} }, global });

    expect(wrapper.find('.radio-label').text()).toBe('Test Label');
  });

  it('renders label prop contents', () => {
    const wrapper = shallowMount(
      RadioButton,
      {
        propsData: {
          label: 'Test Label', val: {}, value: {},
        },
        global,
      });

    expect(wrapper.find('.radio-label').text()).toBe('Test Label');
  });

  it('renders slot contents when both slot and label prop are provided', () => {
    const wrapper = shallowMount(RadioButton, {
      slots:     { label: 'Test Label - Slot' },
      propsData: {
        label: 'Test Label - Props', val: {}, value: {},
      },
      global,
    });

    expect(wrapper.find('.radio-label').text()).toBe('Test Label - Slot');
  });
});
