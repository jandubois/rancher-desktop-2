import {
  decodeMessage, encodeInput, encodeResize, sessionEnd, TerminalChannel, TerminalSessionEnd,
} from '@pkg/utils/terminalProtocol';

interface Disposable {
  dispose(): void;
}

/**
 * The parts of a WebSocket that a connection uses.
 */
export interface TerminalSocket {
  binaryType: BinaryType;
  send(data: Uint8Array<ArrayBuffer>): void;
  close(): void;
  addEventListener(type: 'open', listener: () => void): void;
  addEventListener(type: 'message', listener: (event: MessageEvent) => void): void;
  addEventListener(type: 'close', listener: () => void): void;
}

/**
 * The parts of an xterm.js `Terminal` that a connection uses.
 */
export interface TerminalScreen {
  readonly cols: number;
  readonly rows: number;
  write(data: Uint8Array, callback?: () => void): void;
  reset(): void;
  onData(listener: (data: string) => void): Disposable;
  onResize(listener: (size: { cols: number; rows: number }) => void): Disposable;
}

export interface TerminalConnectionCallbacks {
  /** Called once the replay is written, when typed input starts reaching the shell. */
  onReady?: () => void;
  /** Called once when the session ends for this client. */
  onEnd:    (end: TerminalSessionEnd) => void;
}

/**
 * One connection to an rdd terminal session: it relays the terminal's input
 * and size to the socket, and the socket's output to the terminal.
 */
export class TerminalConnection {
  protected socket:        TerminalSocket;
  protected terminal:      TerminalScreen;
  protected callbacks:     TerminalConnectionCallbacks;
  protected subscriptions: Disposable[] = [];
  protected open = false;
  protected replayReceived = false;
  /** Set once the replay is written; input before then is xterm.js answering replayed queries. */
  protected replayWritten = false;
  protected ended = false;

  constructor(socket: TerminalSocket, terminal: TerminalScreen, callbacks: TerminalConnectionCallbacks) {
    this.socket = socket;
    this.terminal = terminal;
    this.callbacks = callbacks;

    socket.binaryType = 'arraybuffer';
    socket.addEventListener('open', () => {
      this.open = true;
      this.send(encodeResize(terminal.cols, terminal.rows));
    });
    socket.addEventListener('message', event => this.receive(event));
    socket.addEventListener('close', () => this.end(sessionEnd(undefined)));

    this.subscriptions.push(
      terminal.onData((data) => {
        if (this.replayWritten) {
          encodeInput(data).forEach(message => this.send(message));
        }
      }),
      terminal.onResize(({ cols, rows }) => this.send(encodeResize(cols, rows))),
    );
  }

  protected send(message: Uint8Array<ArrayBuffer>) {
    if (this.open && !this.ended) {
      this.socket.send(message);
    }
  }

  protected receive(event: MessageEvent) {
    if (this.ended || !(event.data instanceof ArrayBuffer)) {
      return;
    }
    const message = decodeMessage(event.data);

    switch (message?.channel) {
    case TerminalChannel.Output:
      if (this.replayReceived) {
        this.terminal.write(message.data);
      } else {
        // The first output is the replay, which rdd sends even when it is
        // empty. The replay restores the screen, so write it to a reset terminal.
        // A new shell's replay is empty, and xterm.js calls a write's callback
        // for empty data too, so typed input still reaches the new shell.
        this.replayReceived = true;
        this.terminal.reset();
        this.terminal.write(message.data, () => {
          this.replayWritten = true;
          this.callbacks.onReady?.();
        });
      }
      break;
    case TerminalChannel.Status:
      this.end(sessionEnd(message.status));
      break;
    }
  }

  protected end(end: TerminalSessionEnd) {
    if (this.ended) {
      return;
    }
    this.ended = true;
    this.socket.close();
    this.callbacks.onEnd(end);
  }

  /**
   * Close the connection without reporting an end; rdd keeps the shell running.
   */
  dispose() {
    this.ended = true;
    this.subscriptions.forEach(subscription => subscription.dispose());
    this.subscriptions = [];
    this.socket.close();
  }
}
