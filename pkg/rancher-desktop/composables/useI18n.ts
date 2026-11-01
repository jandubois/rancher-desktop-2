// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import type { Store } from 'vuex';

/** The translator for setup code, which has no `this.t` */
export function useI18n(store: Store<any>) {
  return { t: (key: string, args?: Record<string, unknown>): string => store.getters['i18n/t'](key, args) };
}
