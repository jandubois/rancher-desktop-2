import { FitAddon } from '@xterm/addon-fit';
import { ITerminalOptions, Terminal } from '@xterm/xterm';
import { nextTick } from 'vue';

const theme = {
  background:    '#1a1a1a',
  foreground:    '#e0e0e0',
  cursor:        '#e0e0e0',
  black:         '#000000',
  red:           '#ff5555',
  green:         '#50fa7b',
  yellow:        '#f1fa8c',
  blue:          '#8be9fd',
  magenta:       '#ff79c6',
  cyan:          '#8be9fd',
  white:         '#f8f8f2',
  brightBlack:   '#6272a4',
  brightRed:     '#ff6e6e',
  brightGreen:   '#69ff94',
  brightYellow:  '#ffffa5',
  brightBlue:    '#d6acff',
  brightMagenta: '#ff92df',
  brightCyan:    '#a4ffff',
  brightWhite:   '#ffffff',
};

/**
 * useXterm opens an xterm.js terminal in the element, with the app's theme
 * and font, and fits it to the element whenever the element changes size.
 * Aborting the signal disposes the terminal.
 * @param options Terminal options; `theme` entries override the app's theme.
 */
export async function useXterm(element: HTMLElement, options: ITerminalOptions, signal: AbortSignal) {
  const terminal = new Terminal({
    fontSize:   14,
    fontFamily: '"Courier New", "Monaco", monospace',
    ...options,
    theme:      { ...theme, ...options.theme },
  });
  const fitAddon = new FitAddon();

  terminal.loadAddon(fitAddon);
  signal.addEventListener('abort', () => {
    try {
      fitAddon.dispose();
    } catch {
      // Ignore errors here: fitAddon.dispose() can throw incorrectly.
    }
    terminal.dispose();
  });

  // Expose the terminal for e2e tests.
  (element as any).__xtermTerminal = terminal;
  terminal.open(element);

  await nextTick();
  if (!signal.aborted) {
    const resizeObserver = new ResizeObserver(fitAddon.fit.bind(fitAddon));

    resizeObserver.observe(element);
    signal.addEventListener('abort', () => resizeObserver.disconnect());
    fitAddon.fit();
  }

  return { terminal, fitAddon };
}
