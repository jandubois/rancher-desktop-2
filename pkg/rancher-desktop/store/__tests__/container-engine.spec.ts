import { jest } from '@jest/globals';

import { ActionContext } from '@pkg/store/ts-helpers';
import mockModules from '@pkg/utils/testUtils/mockModules';

import type { state as StateFunc } from '../container-engine';

type State = ReturnType<typeof StateFunc>;

mockModules({
  '@pkg/entry/store': undefined,
  electron:           undefined,
});

const { getters: realGetters, actions: realActions, mutations } = await import('../container-engine');

describe('container-engine store', () => {
  describe('getters.currentNamespace', () => {
    const { currentNamespace } = realGetters;
    it('should return undefined when namespace are not supported', () => {
      const getters = { supportsNamespaces: false };
      expect(currentNamespace({} as State, getters)).toBeUndefined();
    });
    it('should return selected namespace when available', () => {
      const getters = { supportsNamespaces: true };
      const state = { selectedNamespace: 'three-per-em' } as unknown as State;
      expect(currentNamespace(state, getters)).toBe('three-per-em');
    });
    it('should return default as a fallback if available', () => {
      const getters = {
        supportsNamespaces: true,
        namespaces:         ['figure', 'default', 'hair'],
      };
      const state = { } as unknown as State;
      expect(currentNamespace(state, getters)).toBe('default');
    });
    it('should pick the first one if no default is available', () => {
      const getters = {
        supportsNamespaces: true,
        namespaces:         ['punctuation', 'thin'],
      };
      const state = { } as unknown as State;
      expect(currentNamespace(state, getters)).toBe('punctuation');
    });
    it('should return undefined if no namespaces exist', () => {
      const getters = {
        supportsNamespaces: true,
        namespaces:         [],
      };
      const state = { } as unknown as State;
      expect(currentNamespace(state, getters)).toBeUndefined();
    });
  });
  describe('actions.setCurrentNamespace', () => {
    const { setCurrentNamespace } = realActions;
    it('should ignore no-op updates', () => {
      const commit = jest.fn();
      const state = { selectedNamespace: 'six-per-em' } as unknown as State;
      const context = { commit, state } as unknown as ActionContext<State, typeof mutations, typeof realGetters>;
      setCurrentNamespace.call(null as any, context, { namespace: 'six-per-em' });
      expect(commit).not.toHaveBeenCalled();
    });
    it('should error if namespaces are not supported', () => {
      const commit = jest.fn();
      const state = { selectedNamespace: 'thin' } as unknown as State;
      const getters = { supportsNamespaces: false };
      const context = { commit, state, getters } as unknown as ActionContext<State, typeof mutations, typeof realGetters>;
      jest.spyOn(console, 'log').mockImplementation(() => {});
      setCurrentNamespace.call(null as any, context, { namespace: 'hair' });
      expect(commit).toHaveBeenCalledWith('SET_ERROR', expect.objectContaining({
        error:  expect.any(Error),
        source: 'namespaces',
      }));
    });
    it('should error if new namespace does not exist', () => {
      const commit = jest.fn();
      const state = { selectedNamespace: 'en' } as unknown as State;
      const getters = { supportsNamespaces: true, namespaces: ['medium-mathematical'] };
      const context = { commit, state, getters } as unknown as ActionContext<State, typeof mutations, typeof realGetters>;
      jest.spyOn(console, 'log').mockImplementation(() => {});
      setCurrentNamespace.call(null as any, context, { namespace: 'zero-width' });
      expect(commit).toHaveBeenCalledWith('SET_ERROR', expect.objectContaining({
        error:  expect.any(Error),
        source: 'namespaces',
      }));
    });
    it('should set the selected namespace', () => {
      const commit = jest.fn();
      const state = { selectedNamespace: 'no-break' } as unknown as State;
      const getters = { supportsNamespaces: true, namespaces: ['narrow-no-break'] };
      const dispatch = jest.fn();
      const context = { commit, state, getters, dispatch } as unknown as ActionContext<State, typeof mutations, typeof realGetters>;
      setCurrentNamespace.call(null as any, context, { namespace: 'narrow-no-break' });
      expect(commit).toHaveBeenCalledWith('SET_SELECTED_NAMESPACE', 'narrow-no-break');
      expect(dispatch).toHaveBeenCalledWith('rewatchResources', expect.anything());
    });
  });
});
