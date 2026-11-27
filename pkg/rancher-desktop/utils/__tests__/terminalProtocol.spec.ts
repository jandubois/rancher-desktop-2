import {
  decodeMessage, encodeInput, encodeResize, MAX_INPUT_PAYLOAD, sessionEnd, TerminalChannel,
} from '../terminalProtocol';

function message(channel: number, payload: string): ArrayBuffer {
  return Uint8Array.from([channel, ...new TextEncoder().encode(payload)]).buffer;
}

describe('encodeInput', () => {
  it('sends short input as one channel-0 message', () => {
    expect(encodeInput('ls\r')).toEqual([Uint8Array.from([0, 0x6C, 0x73, 0x0D])]);
  });

  it('splits large input into messages of under 32 KiB of payload', () => {
    // '€' is three bytes, so some boundary falls inside a character.
    const input = 'a€'.repeat(40_000);
    const messages = encodeInput(input);
    const expected = new TextEncoder().encode(input);

    expect(messages.length).toBeGreaterThan(1);
    for (const m of messages) {
      expect(m[0]).toBe(TerminalChannel.Input);
      expect(m.length - 1).toBeLessThan(32 * 1024);
      expect(m.length - 1).toBeGreaterThan(0);
    }
    // The first message ends inside a character.
    expect(() => new TextDecoder('utf-8', { fatal: true }).decode(messages[0].subarray(1))).toThrow();
    const payload = new Uint8Array(expected.length);
    let offset = 0;

    for (const m of messages) {
      payload.set(m.subarray(1), offset);
      offset += m.length - 1;
    }
    expect(offset).toBe(expected.length);
    expect(payload).toEqual(expected);
    expect(MAX_INPUT_PAYLOAD).toBeLessThan(32 * 1024);
  });

  it('does not send a message for empty input', () => {
    expect(encodeInput('')).toEqual([]);
  });
});

describe('encodeResize', () => {
  it('sends the size as channel-4 JSON with Width and Height', () => {
    const m = encodeResize(120, 40);

    expect(m[0]).toBe(TerminalChannel.Resize);
    expect(new TextDecoder().decode(m.subarray(1))).toBe('{"Width":120,"Height":40}');
  });
});

describe('decodeMessage', () => {
  it('decodes channel 1 as output bytes', () => {
    expect(decodeMessage(message(1, 'hi'))).toEqual({
      channel: TerminalChannel.Output, data: new TextEncoder().encode('hi'),
    });
  });

  it('decodes an empty channel-1 message as empty output', () => {
    expect(decodeMessage(message(1, ''))).toEqual({ channel: TerminalChannel.Output, data: new Uint8Array() });
  });

  it('decodes channel 3 as a status', () => {
    expect(decodeMessage(message(3, '{"status":"Success"}'))).toEqual({
      channel: TerminalChannel.Status, status: { status: 'Success' },
    });
  });

  it('ignores other channels', () => {
    expect(decodeMessage(message(2, 'stderr'))).toBeUndefined();
    expect(decodeMessage(message(255, ''))).toBeUndefined();
  });
});

describe('sessionEnd', () => {
  it('reports a clean exit', () => {
    expect(sessionEnd({ status: 'Success' })).toEqual({
      kind: 'exited', exitStatus: 0, severity: 'info', action: 'startNewShell',
    });
  });

  it('reports a non-zero exit status', () => {
    expect(sessionEnd({
      status:  'Failure',
      reason:  'NonZeroExitCode',
      message: 'command terminated with non-zero exit code 3',
      details: { causes: [{ reason: 'ExitCode', message: '3' }] },
    })).toEqual({
      kind: 'exited', exitStatus: 3, severity: 'error', action: 'startNewShell',
    });
  });

  it('reports a takeover by another client', () => {
    expect(sessionEnd({
      status: 'Failure', reason: 'Conflict', code: 409, message: 'another client attached to the session',
    })).toEqual({ kind: 'takenOver', severity: 'warning', action: 'reattach' });
  });

  it('reports any other failure with rdd\'s message', () => {
    expect(sessionEnd({ status: 'Failure', message: 'container is not running' })).toEqual({
      kind: 'failed', message: 'container is not running', severity: 'error', action: 'startNewShell',
    });
  });

  it('reports a connection closed without a status as lost', () => {
    expect(sessionEnd(undefined)).toEqual({ kind: 'connectionLost', severity: 'error', action: 'reattach' });
  });
});
