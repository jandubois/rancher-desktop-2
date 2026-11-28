/**
 * The framing of rdd's terminal endpoints (`exec` and `shell`), which speak
 * the `v5.channel.k8s.io` subprotocol of `kubectl exec`.  Every message is
 * binary, and its first byte is the channel number.
 * See rdd/docs/design/terminal-sessions.md, "Wire format".
 */

export const TERMINAL_SUBPROTOCOL = 'v5.channel.k8s.io';

export enum TerminalChannel {
  Input = 0,
  Output = 1,
  Status = 3,
  Resize = 4,
}

/**
 * The most input bytes that one message holds, a little under the 32 KiB of
 * input that client-go sends in one message.
 */
export const MAX_INPUT_PAYLOAD = 32 * 1024 - 1;

/**
 * A Kubernetes `metav1.Status` from channel 3, with only the fields that the
 * GUI declares.
 */
export interface TerminalStatus {
  status?:  string;
  reason?:  string;
  message?: string;
  code?:    number;
  details?: { causes?: { reason?: string; message?: string }[] };
}

export type TerminalMessage =
  | { channel: TerminalChannel.Output; data: Uint8Array } |
  { channel: TerminalChannel.Status; status: TerminalStatus };

function frame(channel: TerminalChannel, payload: Uint8Array): Uint8Array<ArrayBuffer> {
  const message = new Uint8Array(payload.length + 1);

  message[0] = channel;
  message.set(payload, 1);

  return message;
}

/**
 * Encode typed or pasted input as channel-0 messages.  A message can end in
 * the middle of a UTF-8 character, because rdd passes the bytes through.
 */
export function encodeInput(input: string): Uint8Array<ArrayBuffer>[] {
  const bytes = new TextEncoder().encode(input);
  const messages: Uint8Array<ArrayBuffer>[] = [];

  for (let offset = 0; offset < bytes.length; offset += MAX_INPUT_PAYLOAD) {
    messages.push(frame(TerminalChannel.Input, bytes.subarray(offset, offset + MAX_INPUT_PAYLOAD)));
  }

  return messages;
}

/**
 * Encode a terminal size as a channel-4 message.
 */
export function encodeResize(cols: number, rows: number): Uint8Array<ArrayBuffer> {
  const json = JSON.stringify({ Width: cols, Height: rows });

  return frame(TerminalChannel.Resize, new TextEncoder().encode(json));
}

/**
 * Decode a message from rdd, or return undefined for a channel that the GUI
 * ignores.
 */
export function decodeMessage(message: ArrayBuffer): TerminalMessage | undefined {
  const bytes = new Uint8Array(message);
  const payload = bytes.subarray(1);

  switch (bytes[0]) {
  case TerminalChannel.Output:
    return { channel: TerminalChannel.Output, data: payload };
  case TerminalChannel.Status: {
    const text = new TextDecoder().decode(payload);

    try {
      return { channel: TerminalChannel.Status, status: JSON.parse(text) };
    } catch {
      return { channel: TerminalChannel.Status, status: { status: 'Failure', message: text } };
    }
  }
  }

  return undefined;
}

/**
 * How a session ended for this client, and what the GUI offers next.  Both
 * actions open a new connection; rdd reattaches to a running shell or starts
 * a new one.
 */
export type TerminalSessionEnd = {
  severity: 'info' | 'warning' | 'error';
  action:   'startNewShell' | 'reattach';
} & (
  | { kind: 'exited'; exitStatus: number } |
  { kind: 'failed'; message: string } |
  { kind: 'takenOver' } |
  { kind: 'connectionLost' }
);

/**
 * Map the channel-3 status to how the session ended; undefined means the
 * connection closed without a status.
 */
export function sessionEnd(status: TerminalStatus | undefined): TerminalSessionEnd {
  if (!status) {
    return { kind: 'connectionLost', severity: 'error', action: 'reattach' };
  }
  if (status.status === 'Success') {
    return {
      kind: 'exited', exitStatus: 0, severity: 'info', action: 'startNewShell',
    };
  }
  if (status.reason === 'Conflict') {
    return { kind: 'takenOver', severity: 'warning', action: 'reattach' };
  }
  if (status.reason === 'NonZeroExitCode') {
    const cause = status.details?.causes?.find(cause => cause.reason === 'ExitCode');
    const exitStatus = Number.parseInt(cause?.message ?? '', 10);

    if (!Number.isNaN(exitStatus)) {
      return {
        kind: 'exited', exitStatus, severity: 'error', action: 'startNewShell',
      };
    }
  }

  return {
    kind: 'failed', message: status.message ?? '', severity: 'error', action: 'startNewShell',
  };
}
