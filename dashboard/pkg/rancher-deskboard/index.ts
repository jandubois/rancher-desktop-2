import { importTypes } from '@rancher/auto-import';
import { IInternal, IPlugin } from '@shell/core/types';
import dayjs from 'dayjs';
import 'dayjs/locale/de';
import 'dayjs/locale/es';
import 'dayjs/locale/fr';
import 'dayjs/locale/it';
import 'dayjs/locale/ja';
import 'dayjs/locale/ko';
import 'dayjs/locale/pt-br';
import 'dayjs/locale/vi';
import 'dayjs/locale/zh-cn';

// The languages that the shell does not ship, each under its own name.
const addedLocales: Record<string, string> = {
  de:      'Deutsch',
  es:      'Español',
  fr:      'Français',
  it:      'Italiano',
  ja:      '日本語',
  ko:      '한국어',
  'pt-br': 'Português (Brasil)',
  vi:      'Tiếng Việt',
};

// Where dayjs names a locale differently from the dashboard.
const dayjsLocales: Record<string, string> = { 'zh-hans': 'zh-cn' };

// Init the package
export default function(plugin: IPlugin, internal: IInternal): void {
  // Auto-import model, detail, edit from the folders
  importTypes(plugin);

  // Provide plugin metadata from package.json
  plugin.metadata = require('./package.json');

  // The shell ships only en-us and zh-hans.
  for (const [locale, label] of Object.entries(addedLocales)) {
    plugin.addLocale(locale, label);
  }

  // Each translations file is its own chunk, loaded when its locale is selected. The shell's
  // zh-hans loads first, so our zh-hans file overrides it key by key.
  for (const locale of [...Object.keys(addedLocales), 'zh-hans']) {
    plugin.addL10n(locale, () => import(`./translations/${ locale }.yaml`));
  }

  const { store, app: { router } } = internal;

  // The shell never tells dayjs the language, so month and day names would stay
  // in English.
  store.watch((_state: any, getters: any) => getters['i18n/current'](), (locale: string) => {
    dayjs.locale(dayjsLocales[locale] ?? locale);
  });

  let logoRoute = router.resolve({
    name: 'c-cluster-explorer',
    params: { cluster: "local" },
  });

  store.commit('setIsSingleProduct', {
    productNameKey: 'rancher-desktop.label',
    logoRoute,
    logo: require(`./assets/logo.svg`),
  });

  // Hide buttons in the header that don't work in Rancher Desktop.
  // KubeConfig buttons don't function (issues #2208) and Kubectl Shell
  // is not useful since users can use a local terminal (issue #8151).
  store.commit('type-map/product', {
    name:           'explorer',
    hideKubeShell:  true,
    hideKubeConfig: true,
    hideCopyConfig: true,
  });
}
