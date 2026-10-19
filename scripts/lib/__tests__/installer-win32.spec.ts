/**
 * @jest-environment node
 */

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

import { jest } from '@jest/globals';

import buildUtils from '../build-utils';
import { buildCustomAction } from '../installer-win32';
import { makePEImage } from '../testUtils/pe-image';

describe('installer-win32', () => {
  describe('buildCustomAction', () => {
    let distDir: string;

    beforeEach(async() => {
      distDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), 'installer-win32-test-'));
      jest.spyOn(buildUtils, 'distDir', 'get').mockReturnValue(distDir);
    });

    afterEach(async() => {
      jest.restoreAllMocks();
      await fs.promises.rm(distDir, { recursive: true, force: true });
    });

    /** Stub out `go build` to write a PE image with the given section names to its `-o` path. */
    function stubGoBuild(names: string[]) {
      return jest.spyOn(buildUtils, 'spawn').mockImplementation((_command, ...args) => {
        const output = args[args.indexOf('-o') + 1] as string;

        return Object.assign(fs.promises.writeFile(output, makePEImage(names)), { child: {} as any });
      });
    }

    it('rejects a DLL with a stand-alone .ctors section', async() => {
      const spawn = stubGoBuild(['.text', '.rdata', '.ctors']);

      await expect(buildCustomAction()).rejects.toThrow(/stand-alone \.ctors section/);
      expect(spawn).toHaveBeenCalledTimes(1);
    });

    it('returns the path of a DLL without a .ctors section', async() => {
      const spawn = stubGoBuild(['.text', '.rdata', '.pdata']);

      await expect(buildCustomAction()).resolves.toEqual(path.join(distDir, 'wix-custom-action.dll'));
      expect(spawn).toHaveBeenCalledTimes(1);
    });
  });
});
