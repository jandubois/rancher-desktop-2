import { IntlMessageFormat } from 'intl-messageformat';
import { MutationTree } from 'vuex';

import { ActionTree, GetterTree, MutationsType } from './ts-helpers';
import packageJson from '../../../package.json' with { type: 'json' };

import { LOCALE } from '@pkg/config/cookies';
import { getVendor } from '@pkg/config/private-label';
import { RootState } from '@pkg/entry/store';
import { ipcRenderer } from '@pkg/utils/ipcRenderer';
import { get } from '@pkg/utils/object';
import { availableLocales, loadTranslations } from '@pkg/utils/translationLoader';

type I18nState = ReturnType<typeof state>;

// Formatters can't be serialized into state
const intlCache: Record<string, IntlMessageFormat | string> = {};
let ipcListenersBound = false;

export const state = function() {
  const out = {
    default:      'en-us',
    selected:     null as string | null,
    available:    [...availableLocales],
    translations: { 'en-us': loadTranslations('en-us') } as Record<string, Record<string, unknown>>,
  };

  return out;
};

export const getters = {
  availableLocales(state, getters) {
    const labelled = state.available.map((locale) => {
      const nativeName = get(state.translations[locale], `locale.${ locale }`);
      const translatedName =
        get(state.translations[getters.current()], `locale.${ locale }`) ??
        get(state.translations[state.default], `locale.${ locale }`);

      if ( !nativeName || !translatedName || nativeName === translatedName ) {
        return [locale, nativeName ?? translatedName ?? locale];
      }

      return [locale, `${ nativeName } (${ translatedName })`];
    });

    // Sort by the label the user reads, collated in the selected locale.
    // Fall back to the default when the selection is not a bundled locale,
    // because Intl.Collator rejects an unknown tag. The selection is null
    // until init runs, and older settings can carry 'none'.
    const collationLocale = state.available.includes(getters.current()) ? getters.current() : state.default;
    const collator = new Intl.Collator(collationLocale);

    labelled.sort(([, a], [, b]) => collator.compare(a, b));

    return Object.fromEntries(labelled);
  },

  t: (state, getters) => (key: string, args?: Record<string, unknown>) => {
    const cacheKey = `${ getters.current() }/${ key }`;
    let formatter = intlCache[cacheKey];

    if ( !formatter ) {
      let msg = get(state.translations[getters.current()], key);

      if ( !msg ) {
        msg = get(state.translations[state.default], key);
      }

      if ( !msg ) {
        // Visible placeholder, matching the main process; missing keys
        // must be debuggable, not silently blank.
        return `%${ key }%`;
      }

      if ( typeof msg === 'object' ) {
        console.error('Translation for', cacheKey, 'is an object');

        return `%${ key }%`;
      }

      if ( typeof msg !== 'string' ) {
        console.error('Translation for', cacheKey, 'is not a string:', msg);

        msg = String(msg);
      }

      if ( msg?.includes('{')) {
        try {
          // Uses the selected locale for formatting even when falling back to
          // English text. Acceptable: plural rules rarely diverge for the
          // strings used here.
          formatter = new IntlMessageFormat(msg, getters.current());
        } catch (e) {
          console.error(`Malformed ICU pattern for key "${ key }":`, e);
          formatter = msg;
        }
      } else {
        formatter = msg;
      }

      intlCache[cacheKey] = formatter;
    }

    if ( typeof formatter === 'string' ) {
      return formatter;
    } else {
      // Inject things like appName so they're always available in any translation
      const moreArgs = {
        vendor:  getVendor(),
        appName: packageJson.productName,
        ...args,
      };

      try {
        return formatter.format(moreArgs);
      } catch (e) {
        // A missing argument must not abort the component render;
        // degrade to the raw pattern like the main-process interpolator.
        console.error(`Cannot format translation for key "${ key }":`, e);

        return get(state.translations[getters.current()], key) ?? get(state.translations[state.default], key);
      }
    }
  },

  exists: (state, getters) => (key: string) => {
    const cacheKey = `${ getters.current() }/${ key }`;

    if ( intlCache[cacheKey] ) {
      return true;
    }

    let msg = get(state.translations[state.default], key);

    if ( !msg && state.selected ) {
      msg = get(state.translations[getters.current()], key);
    }

    if ( msg !== undefined ) {
      return true;
    }

    return false;
  },

  current: state => () => {
    return state.selected ?? state.default;
  },

  default: state => () => {
    return state.default;
  },

  withFallback(state, getters) {
    function withFallback(key: string, fallback: string): string;
    function withFallback(key: string, args: Record<string, unknown>, fallback: string, fallbackIsKey?: boolean): string;
    function withFallback(key: string, args: Record<string, unknown> | string, fallback?: string, fallbackIsKey = false) {
      // Support withFallback(key,fallback) when no args
      const parsedFallback = typeof args === 'string' ? args : fallback;
      const parsedArgs = typeof args === 'string' ? {} : args;

      if ( getters.exists(key) ) {
        return getters.t(key, parsedArgs);
      } else if ( parsedFallback === undefined ) {
        console.error(`withFallback called for missing key "${ key }" without a fallback`);

        return `%${ key }%`;
      } else if ( fallbackIsKey ) {
        return getters.t(parsedFallback, parsedArgs);
      } else {
        return parsedFallback;
      }
    }
    return withFallback;
  },
} satisfies GetterTree<I18nState>;

export const mutations = {
  loadTranslations(state, { locale, translations }: { locale: string, translations: Record<string, unknown> }) {
    state.translations[locale] = translations;
  },

  setSelected(state, locale: string) {
    state.selected = locale;
  },
} satisfies MutationsType<I18nState> & MutationTree<I18nState>;

export const actions = {
  async init({ state, commit, dispatch }) {
    // Load all translation files so availableLocales can show native names.
    // Acceptable overhead with a small number of locales; revisit if locale
    // count grows significantly.
    await Promise.allSettled(
      state.available
        .filter(locale => !state.translations[locale])
        .map(locale => dispatch('load', locale)),
    );

    // The main process passes the resolved locale as a URL param, so the first
    // paint is already localized; the cookie is a fallback for a window opened
    // without one. Later changes arrive via settings-update below.
    const urlLocale = new URLSearchParams(window.location.search).get('locale');
    let selected = urlLocale || this.$cookies.get(LOCALE, { parseJSON: false });

    if ( !selected ) {
      selected = state.default;
    }

    if (!ipcListenersBound) {
      ipcListenersBound = true;

      // Listen for settings changes (from preferences UI or rdctl) to sync locale.
      ipcRenderer.on('settings-update', (_, settings) => {
        const locale = settings?.application?.locale || state.default;

        if ( locale !== state.selected ) {
          dispatch('switchTo', locale);
        }
      });
    }

    return dispatch('switchTo', selected);
  },

  load({ commit }, locale: string) {
    const translations = loadTranslations(locale);

    commit('loadTranslations', { locale, translations });

    return true;
  },

  async switchTo({ state, commit, dispatch }, locale: string) {
    if ( !locale ) {
      locale = state.default;
    }

    if ( !state.translations[locale] ) {
      try {
        await dispatch('load', locale);
      } catch (e) {
        if ( locale !== 'en-us' ) {
          // Try to show something...

          commit('setSelected', 'en-us');

          return;
        }
      }
    }

    for (const key of Object.keys(intlCache)) {
      delete intlCache[key];
    }

    commit('setSelected', locale);
    this.$cookies.set(LOCALE, locale, {
      encode: x => x,
      maxAge: 86400 * 365,
      secure: true,
      path:   '/',
    });
  },

} satisfies ActionTree<I18nState, RootState, typeof mutations, typeof getters>;
