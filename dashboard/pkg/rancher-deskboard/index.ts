import { importTypes } from '@rancher/auto-import';
import { IInternal, IPlugin } from '@shell/core/types';
import dayjs from 'dayjs';
import 'dayjs/locale/de';
import 'dayjs/locale/ja';
import 'dayjs/locale/vi';
import 'dayjs/locale/zh-cn';

// Where dayjs names a locale differently from the dashboard.
const dayjsLocales: Record<string, string> = { 'zh-hans': 'zh-cn' };

// Init the package
export default function(plugin: IPlugin, internal: IInternal): void {
  // Auto-import model, detail, edit from the folders
  importTypes(plugin);

  // Provide plugin metadata from package.json
  plugin.metadata = require('./package.json');

  // The shell ships only en-us and zh-hans.
  plugin.addLocale('de', 'Deutsch');
  plugin.addLocale('ja', '日本語');
  plugin.addLocale('vi', 'Tiếng Việt');

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
