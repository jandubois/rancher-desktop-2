import { limaVMStatus } from '../limaVMStatus';

interface Condition { type: string, status: string, reason?: string, message?: string }

function vm(running: boolean, conditions: Condition[]) {
  const full = conditions.map(c => ({ lastTransitionTime: new Date(0), message: '', reason: '', ...c }));

  return { spec: { running }, status: { conditions: full } } as any;
}

const createFailed = { type: 'Created', status: 'False', reason: 'CreateFailed', message: 'no disk' };
const created = { type: 'Created', status: 'True', reason: 'Created' };
const notCreated = { type: 'Created', status: 'False', reason: 'Pending' };
const running = (status: string, reason = '', message = '') => ({ type: 'Running', status, reason, message });

describe('limaVMStatus', () => {
  it.each([
    ['Created False, CreateFailed', [createFailed], { state: 'Failed', message: 'no disk' }, { state: 'Failed', message: 'no disk' }],
    ['Running True', [created, running('True', 'Started')], { state: 'Running' }, { state: 'Stopping' }],
    ['Running False, Starting', [created, running('False', 'Starting')], { state: 'Starting' }, { state: 'Stopping' }],
    ['Running False, Stopped', [created, running('False', 'Stopped')], { state: 'Starting' }, { state: 'Stopped' }],
    ['Running False, StartFailed', [created, running('False', 'StartFailed', 'boom')], { state: 'Failed', message: 'boom' }, { state: 'Failed', message: 'boom' }],
    ['Running False, StopFailed', [created, running('False', 'StopFailed', 'stuck')], { state: 'Failed', message: 'stuck' }, { state: 'Failed', message: 'stuck' }],
    ['Running Unknown', [created, running('Unknown', 'Reconciling')], { state: 'Unknown' }, { state: 'Unknown' }],
    ['no Running, Created True', [created], { state: 'Starting' }, { state: 'Stopped' }],
    ['no Running, Created not True', [notCreated], { state: 'Pending' }, { state: 'Pending' }],
    ['no conditions', [], { state: 'Pending' }, { state: 'Pending' }],
  ])('%s', (_, conditions, whenRunning, whenStopped) => {
    expect(limaVMStatus(vm(true, conditions))).toEqual(whenRunning);
    expect(limaVMStatus(vm(false, conditions))).toEqual(whenStopped);
  });
});
