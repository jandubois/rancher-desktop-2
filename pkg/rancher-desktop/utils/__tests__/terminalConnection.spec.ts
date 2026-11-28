import { jest } from '@jest/globals';

import { TerminalConnection, TerminalScreen, TerminalSocket } from '../terminalConnection';

class FakeSocket implements TerminalSocket {
  binaryType: BinaryType = 'blob';
  sent:       Uint8Array[] = [];
  listeners:  Record<string, ((event?: any) => void)[]> = {};
  send(data: Uint8Array) {
    this.sent.push(data);
  }

  close = jest.fn();
  addEventListener(type: string, listener: (event?: any) => void) {
    (this.listeners[type] ??= []).push(listener);
  }

  emit(type: string, event?: any) {
    this.listeners[type]?.forEach(listener => listener(event));
  }

  receive(channel: number, payload: string) {
    this.emit('message', { data: Uint8Array.from([channel, ...new TextEncoder().encode(payload)]).buffer });
  }
}

class FakeTerminal implements TerminalScreen {
  cols = 80;
  rows = 24;
  written:          string[] = [];
  pendingCallbacks: (() => void)[] = [];
  dataListener:     (data: string) => void = () => {};
  reset = jest.fn(() => {
    this.written.push('<reset>');
  });

  write(data: Uint8Array, callback?: () => void) {
    this.written.push(new TextDecoder().decode(data));
    if (callback) {
      this.pendingCallbacks.push(callback);
    }
  }

  onData(listener: (data: string) => void) {
    this.dataListener = listener;

    return { dispose: () => {} };
  }

  onResize() {
    return { dispose: () => {} };
  }
}

function inputSent(socket: FakeSocket) {
  return socket.sent.filter(m => m[0] === 0).map(m => new TextDecoder().decode(m.subarray(1)));
}

function startConnection(...args: ConstructorParameters<typeof TerminalConnection>) {
  return new TerminalConnection(...args);
}

describe('TerminalConnection', () => {
  it('sends the terminal size when the connection opens', () => {
    const socket = new FakeSocket();

    startConnection(socket, new FakeTerminal(), { onEnd: () => {} });
    socket.emit('open');
    expect(socket.binaryType).toBe('arraybuffer');
    expect(socket.sent.map(m => new TextDecoder().decode(m))).toEqual(['\x04{"Width":80,"Height":24}']);
  });

  it('resets the terminal before the replay and ignores input until the replay is written', () => {
    const socket = new FakeSocket();
    const terminal = new FakeTerminal();

    const onReady = jest.fn();

    startConnection(socket, terminal, { onReady, onEnd: () => {} });
    socket.emit('open');
    socket.receive(1, 'replay\x1b[c');
    expect(terminal.written).toEqual(['<reset>', 'replay\x1b[c']);

    // xterm.js answers the replayed query before the write completes.
    terminal.dataListener('\x1b[?1;2c');
    expect(inputSent(socket)).toEqual([]);
    expect(onReady).not.toHaveBeenCalled();

    terminal.pendingCallbacks.forEach(callback => callback());
    expect(onReady).toHaveBeenCalledTimes(1);
    terminal.dataListener('ls\r');
    expect(inputSent(socket)).toEqual(['ls\r']);

    socket.receive(1, 'live');
    expect(terminal.written).toEqual(['<reset>', 'replay\x1b[c', 'live']);
    expect(terminal.reset).toHaveBeenCalledTimes(1);
  });

  it('reports the status, then ignores the close that follows', () => {
    const socket = new FakeSocket();
    const onEnd = jest.fn();

    startConnection(socket, new FakeTerminal(), { onEnd });
    socket.emit('open');
    socket.receive(3, '{"status":"Failure","reason":"Conflict"}');
    socket.emit('close');
    expect(onEnd).toHaveBeenCalledTimes(1);
    expect(onEnd).toHaveBeenCalledWith(expect.objectContaining({ kind: 'takenOver' }));
  });

  it('reports a close without a status as a lost connection', () => {
    const socket = new FakeSocket();
    const onEnd = jest.fn();

    startConnection(socket, new FakeTerminal(), { onEnd });
    socket.emit('open');
    socket.emit('close');
    expect(onEnd).toHaveBeenCalledWith(expect.objectContaining({ kind: 'connectionLost' }));
  });

  it('does not report an end after it is disposed', () => {
    const socket = new FakeSocket();
    const onEnd = jest.fn();
    const connection = new TerminalConnection(socket, new FakeTerminal(), { onEnd });

    socket.emit('open');
    connection.dispose();
    socket.emit('close');
    expect(socket.close).toHaveBeenCalled();
    expect(onEnd).not.toHaveBeenCalled();
  });
});
