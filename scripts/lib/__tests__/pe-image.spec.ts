/**
 * @jest-environment node
 */

import { peSectionNames } from '../pe-image';
import { makePEImage } from '../testUtils/pe-image';

describe('pe-image', () => {
  describe('peSectionNames', () => {
    it('returns the section names in table order', () => {
      const names = ['.text', '.rdata', '.ctors', '.textbss', '/4'];

      expect(peSectionNames(makePEImage(names))).toEqual(names);
    });

    it('rejects a file that is not a PE image', () => {
      expect(() => peSectionNames(Buffer.from('#!/bin/sh\n'))).toThrow(/Not a PE image/);
    });

    it('rejects a truncated section table', () => {
      expect(() => peSectionNames(makePEImage(['.text']).subarray(0, -1))).toThrow(/Truncated PE section table/);
    });
  });
});
