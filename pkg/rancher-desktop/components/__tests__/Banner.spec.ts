import { mount } from '@vue/test-utils';

import Banner from '../Banner.vue';

import cleanHtmlDirective from '@pkg/plugins/clean-html-directive';

const global = { plugins: [cleanHtmlDirective], stubs: { t: true } };

describe('component: Banner', () => {
  it('should display text based on label', () => {
    const label = 'test';
    const wrapper = mount(
      Banner,
      { propsData: { label }, global });

    const element = wrapper.find('span').element;

    expect(element.textContent).toBe(label);
  });

  it('should display an icon', () => {
    const icon = 'my-icon';
    const wrapper = mount(Banner, { propsData: { icon }, global });

    const element = wrapper.find(`.${ icon }`).element;

    expect(element.classList).toContain(icon);
  });

  it('should not display an icon', () => {
    const wrapper = mount(Banner, { global });

    const iconElement = wrapper.find('[data-testid="banner-icon"]');

    expect(iconElement.exists()).toBe(false);
  });

  it('should emit close event', () => {
    const wrapper = mount(Banner, { propsData: { closable: true }, global });
    const element = wrapper.find(`[data-testid="banner-close"]`).element;

    element.click();

    expect(wrapper.emitted('close')).toHaveLength(1);
  });

  it('should add the right color', () => {
    const color = 'red';
    const wrapper = mount(Banner, { propsData: { color }, global });

    const element = wrapper.element;

    expect(element.classList).toContain(color);
  });

  it('should stack the banner messages', () => {
    const stacked = true;
    const wrapper = mount(Banner, { propsData: { stacked }, global });

    const element = wrapper.find(`[data-testid="banner-content"]`).element;

    expect(element.classList).toContain('stacked');
  });
});
