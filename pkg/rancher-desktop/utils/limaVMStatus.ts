import type { IoRancherdesktopLimaV1alpha1LimaVM as LimaVM } from '@rdd-client';

export type LimaVMState = 'Running' | 'Starting' | 'Stopping' | 'Stopped' | 'Failed' | 'Pending' | 'Unknown';

export interface LimaVMStatus {
  state:    LimaVMState;
  /** The failed condition's message; set only when the state is Failed. */
  message?: string;
}

/**
 * Derive a LimaVM's current state from its desired state (`spec.running`)
 * and from the `Created` and `Running` conditions that the LimaVM controller
 * sets.
 */
export function limaVMStatus(vm: LimaVM): LimaVMStatus {
  const wantRunning = !!vm.spec?.running;
  const conditions = vm.status?.conditions ?? [];
  const created = conditions.find(c => c.type === 'Created');
  const running = conditions.find(c => c.type === 'Running');

  if (created?.status === 'False' && created.reason === 'CreateFailed') {
    return { state: 'Failed', message: created.message };
  }
  if (!running) {
    if (created?.status !== 'True') {
      return { state: 'Pending' };
    }
    return { state: wantRunning ? 'Starting' : 'Stopped' };
  }
  switch (running.status) {
  case 'True':
    return { state: wantRunning ? 'Running' : 'Stopping' };
  case 'False':
    switch (running.reason) {
    case 'StartFailed': case 'StopFailed':
      return { state: 'Failed', message: running.message };
    case 'Stopped':
      return { state: wantRunning ? 'Starting' : 'Stopped' };
    default:
      // Starting, and any reason not listed above.
      return { state: wantRunning ? 'Starting' : 'Stopping' };
    }
  }
  return { state: 'Unknown' };
}
