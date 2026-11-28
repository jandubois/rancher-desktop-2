# Terminal Sessions

The GUI shows an interactive shell for a container on its Shell tab, and for a Lima VM on a shell page opened from the Virtual Machines page. RDD runs each shell and owns its session, and the GUI attaches to the session over a WebSocket. So a session outlives the page that opened it. Switching tabs or pages, reloading the window, or restarting the app reattaches to the same shell, with its recent output. A session ends when its shell exits, and that includes the shell dying because its container or VM stopped or was deleted.

The terminal in the GUI is xterm.js, a full xterm emulator, so full-screen programs such as `top` and `vim` work. RDD gives each shell a pseudo-terminal (pty) of the terminal's size and passes the bytes through unchanged.

## Endpoints

Both endpoints are passthrough endpoints. The API server routes each one to the controller that registered it (see [controller manager discovery](discovery.md)).

- `/passthrough/engine/exec/${container}` opens a shell in a container. The last part of the path is the `Container` mirror's name, as for the `logs` endpoint.
- `/passthrough/limavm/shell/${namespace}/${name}` opens a shell in the VM of a `LimaVM` object.

Each request upgrades to a WebSocket and attaches to the target's session. If the target doesn't have a session yet, RDD starts one. A session has at most one client. When a second client attaches, RDD ends the first client's connection with a status that says so.

The API server treats a request under `/passthrough/` as an ordinary request, so it cuts each WebSocket at its request deadline, 60 s by default. It does that to the `logs` stream today. So RDD marks WebSocket upgrade requests under `/passthrough/` as long-running, as Kubernetes does for `exec` and `watch`. A plain request to a passthrough endpoint keeps the deadline.

## Wire format

The WebSocket uses `v5.channel.k8s.io`, the subprotocol of `kubectl exec`. RDD rejects a request that doesn't offer it. When it accepts one, it names the subprotocol in the handshake response, because client-go closes a connection whose response doesn't name it. The GUI opens the WebSocket with `new WebSocket(url, 'v5.channel.k8s.io')`. Every message is binary, and its first byte is the channel number.

Channel | Direction | Payload
--- | --- | ---
0 | GUI to RDD | Terminal input.
1 | RDD to GUI | Terminal output. The pty merges stderr into it, so channel 2 stays unused.
3 | RDD to GUI | A JSON `metav1.Status` when the session ends for this client.
4 | GUI to RDD | A JSON terminal size in columns and rows, such as `{"Width": 120, "Height": 40}`.

A browser can't read the HTTP status of a failed upgrade. So RDD upgrades every request that offers the subprotocol, and reports what happened on channel 3:

- `Success` when the shell exits with status 0.
- `Failure` with reason `NonZeroExitCode` and an `ExitCode` cause when it exits with another status.
- `Failure` with reason `Conflict` when another client takes the session over.
- `Failure` with the error as its message when the shell can't start, for example because the container or VM isn't running, or because SSH fails.

After the status, RDD closes the WebSocket with code 1000, because client-go treats any other code as an error. But when the shell's SSH connection closes before the shell's exit status arrives, RDD doesn't send a status. It closes with code 1011 instead, because client-go would report a 1000 close without a status as a clean exit.

In the protocol, a channel 255 message closes a stream, such as the input. RDD ignores it, because a terminal ends its input with a ^D byte on channel 0 instead. RDD doesn't decode the bytes, so a message can end in the middle of a UTF-8 character. That works, because the GUI writes each message's bytes to xterm.js, which decodes a UTF-8 character even when it's split across two writes.

RDD accepts input messages of up to 64 KiB. That's more than either client sends, because client-go sends at most 32 KiB of input in one message, and the GUI splits a larger paste into messages of under 32 KiB.

The protocol is Kubernetes' own, with its channel numbers defined in `k8s.io/apimachinery/pkg/util/remotecommand`, and client-go's `remotecommand.NewWebSocketExecutor` speaks it. So if RDD follows the protocol exactly, Go tests and a later CLI command can drive the endpoints without implementing the protocol themselves. A custom format, with terminal bytes in binary messages and JSON control messages in text ones, would save a little code at each end. But only RDD would document it, and the tests would need their own implementation of it.

RDD implements the framing on `gorilla/websocket`. The `logs` endpoint already uses it, so the terminal endpoints can share its connection handling (pings, deadlines, closing). Kubernetes' own `wsstream` server (`k8s.io/streaming/pkg/httpstream/wsstream`) would rule that out, because it is built on `golang.org/x/net/websocket`.

## Sessions

RDD keeps one session per target, keyed by the `Container` name or by the `LimaVM` namespace and name. A session holds the shell's SSH session, the attached client if there is one, and a replay buffer with the last 1 MiB of output. That is about what the shell terminal's 10,000-line scrollback holds.

The registry adds a new session under its lock before it starts the shell. So a client that attaches in the meantime becomes the session's client, and a target never gets a second shell. The shell starts when the client's first resize message arrives, so that its pty has the client's size from the start. If the client doesn't send one within a second, the shell starts with 80 columns and 24 rows. Input that arrives earlier waits until the shell runs.

When a client detaches, the shell keeps running, and its output keeps going into the buffer. When a client attaches, RDD sends a replay as one channel-1 message before any live output, even when the replay is empty. The replay starts with the terminal modes that were in force where the buffer starts, and continues with the buffer. If the buffer has dropped bytes, the replay skips to the buffer's first line break, so that it rarely begins inside an escape sequence. When the buffer doesn't have a line break, the replay skips to the first byte that starts a UTF-8 character.

The terminal modes that RDD tracks are the alternate screen, application cursor keys, mouse reporting, bracketed paste, and the cursor's visibility. A full-screen program sets them when it starts and resets them when it exits. RDD keeps their state at the start of the buffer by parsing each byte that drops out of it, and it also parses the bytes that the replay skips. Without the modes, a reattach to a program that has filled the buffer would show it on the normal screen, with its earlier screens in the scrollback, and xterm.js would ignore the mouse.

The GUI resets its terminal before it writes the replay. xterm.js answers terminal queries, such as the one that vim sends when it starts, through the same event that reports typed input. So the GUI ignores that event until the replay's write completes. Otherwise xterm.js would answer the replayed queries again, and the answers would reach the shell as typed text.

On the client's first resize message after the replay, RDD sets the pty to one row less than the client's size, or one row more for a client with a single row. It restores the size once the shell has written output, or after 200 ms. The two size changes make a full-screen program such as `top` redraw its screen at once. The wait between the two changes matters in a container shell. There the `docker` or `nerdctl` CLI gets the pty's SIGWINCH, and it forwards the size that it reads when it handles the signal. So two size changes in quick succession could reach the container as a single unchanged size.

The replay has one known flaw. A buffer that starts inside an escape sequence can still show a few stray characters at the top of the replay. A terminal emulator in RDD would send the exact screen on attach instead, but it needs an emulator library that RDD doesn't use today, and more code. Such an emulator can replace the buffer later without changing the wire format or the GUI. Keeping each xterm.js terminal alive in the GUI would also restore the exact screen, but only until the window reloads or the app restarts.

When the shell exits, RDD sends the status to the attached client, closes its WebSocket, and removes the session, so the next attach starts a new shell. If the shell exits after its client has detached, RDD drops its status, and the next attach starts a new shell without saying that the old one ended. A container that stops kills its shell, so its session ends the same way. A VM that stops closes the SSH connections of the shells in it, including the container shells when the App's VM stops. So their sessions end without a status. Sessions also end when RDD restarts, because its SSH connections close with it. Keeping the replay buffer until the target is deleted would keep the output across a stop and a start, but RDD would then have to remove a session when its `Container` or `LimaVM` object is deleted.

The GUI shows how a session ended in a banner above the terminal, and the terminal keeps its output. A message written into the terminal instead would look like the shell's own output, and a bar docked inside the terminal would cover its last lines, where the shell's final output is.

The banner's button reads "Start new shell" after the shell exited or failed, and "Reattach" after another client took the session over or the connection closed without a status. Either button opens a new connection, and RDD reattaches to the shell if it is still running or starts a new one. The GUI disables the button while the container or VM isn't running, because the button would only fail. A target that isn't running doesn't have a shell to reattach to, and RDD starts a new shell only in a running target. The GUI doesn't reconnect on its own. Two clients that reconnected on their own would take the session from each other in turn, and each retry against a stopped container would start and fail another `docker exec`.

## Starting a shell

All three kinds of shell use one SSH implementation. RDD connects in process to the instance's SSH address and port with `golang.org/x/crypto/ssh`, gives up after 30 s, and logs in as the instance's user with Lima's user key. It accepts only the host key that Lima pinned in the instance's `ssh.known_hosts` file (`sshutil.ReadPinnedHostKey`), and asks only for that key's algorithm (`rsa-sha2-512` and `rsa-sha2-256` for an RSA key). The guest has host keys of several types, and Lima pins one of them, the ed25519 key when the guest has one. x/crypto's default list of host key algorithms asks for ECDSA first, so with the defaults the guest would present a key that doesn't match the pinned one. Lima pins the key before it reports SSH as ready, so a file without a pinned key means the instance isn't ready, and RDD refuses to connect. Then RDD requests a pty of the client's size with `TERM=xterm-256color`, runs one command, turns each resize message into an SSH window change, and reports the command's exit status on channel 3.

Each session has its own SSH connection. RDD sends a keepalive request every 30 s and ends the session without a status when three go unanswered, as `guestexec.Command` configures the `ssh` binary to do. Otherwise a detached session whose connection hangs, for example after the Mac sleeps, would never end, and the GUI would reattach to a dead shell.

RDD doesn't run the `ssh` binary for these shells. The binary sends window changes only when its own input is a terminal, so RDD would have to run it under a pty, using `creack/pty` on Unix and ConPTY on Windows. `golang.org/x/crypto/ssh` sends window-change requests itself, and RDD already compiles it in through Lima.

The command depends on the target:

- A Lima VM runs the user's login shell, `exec "$SHELL" --login`.
- A moby container runs `docker exec -it -e TERM=xterm-256color ${id} sh -c "${shell}"` in the App's VM, `rd`. `${id}` is the container's `status.id`. Every user in the guest can write to the Docker socket, so the command doesn't need `sudo`.
- A containerd container runs `sudo nerdctl --address /run/k3s/containerd/containerd.sock --namespace ${namespace} exec -it -e TERM=xterm-256color ${id} sh -c "${shell}"` in the same VM, with the container's `status.namespace` and `status.id`.

`${shell}` is `if command -v bash >/dev/null; then exec bash; fi; exec sh`. In a container without `sh`, the session ends at once with the error that `docker` or `nerdctl` prints. With `-t`, `docker` and `nerdctl` create a pty in the container and resize it when their own pty changes size. `-t` also needs a terminal on their own input, and the SSH pty is one.

`nerdctl exec` exits with status 1 whenever its command exits with a non-zero status, and prints that status in an error message. So when a containerd shell ends with `exit 3`, the terminal's last line is nerdctl's `exec failed with exit code 3`, and channel 3 reports exit code 1. [containerd/nerdctl#3462](https://github.com/containerd/nerdctl/pull/3462) would make nerdctl exit with its command's status.

On Windows, the same SSH path reaches the `sshd` that Lima's WSL2 driver runs in the distro, as `rdd limavm shell` does. A shell in the App's VM runs in WSL's root network namespace. It doesn't enter the namespaces of `network-namespace.service`, as the guest's `wsl-exec` script does. Only that service's namespace reaches the DNS server, so name lookups fail in the shell. `rdd limavm shell rd` has the same limit. A container shell runs in the container's own namespaces. The containerd command relies on the guest's `nerdctl` wrapper in `/usr/local/bin`, which is on `sudo`'s path. The wrapper runs nerdctl in containerd's namespaces.

The SSH code finds the instance through Lima's store, which reads `LIMA_HOME`. The `LimaVM` controller sets it when it registers. The engine controller sets it to the same path, `instance.LimaHome()`, so that it finds the App's VM even when `--controllers` runs it in a process without the `LimaVM` controller.

A method of the `engine` interface returns each backend's command line, and the engine controller passes it to the shared SSH code. So moby could later open its shells through the Docker API instead, by replacing that method with one that opens the session. That method would call the Docker API's exec create, attach, resize and inspect, over the Docker connection that the engine controller already uses for logs. The Docker API would take SSH and the guest's `docker` CLI out of the path, at the cost of a second session implementation. It would cover moby only, because containerd's exec passes its stdio as FIFOs that the client creates on its own machine (`cio.NewFIFOSetInDir`), and RDD's containerd client runs on the host.

## Code

RDD:

- `pkg/service/cmd/options/config.go` wraps the `LongRunningFunc` that `BuildGenericConfig` sets, so that it marks WebSocket upgrade requests under `/passthrough/` as long-running. It does so before `CreateAggregatorConfig` copies the config, because the aggregator's copy runs the request filters.
- `pkg/guestexec` gets the in-process SSH pty session, next to `Command`, which keeps running the `ssh` binary for `rdd limavm shell`.
- A new `pkg/terminal` package holds the session registry, the replay buffer with its mode tracking, the channel framing, and the WebSocket handler. The connection handling moves there from `HandleLogs` in `container_passthrough.go`, and the `logs` endpoint and both terminal endpoints use it. Each controller owns one registry.
- The App VM's name and the guest's containerd socket move to `pkg/apis/app/v1alpha1`, beside `AppNamespace`. The App controller, `rdd nerdctl` and the engine controller then share them; today the first two each define the VM's name.
- The engine controller registers `exec` next to `logs`. Its handler looks the session up in the registry first, so a reattach works while the engine watcher is down, when `HandleLogs` answers 503. It asks the engine for a command line only to start a new shell, and reads the engine under `engineMu`, as `HandleLogs` does. The `engine` interface gets a method that returns a `Container`'s shell command line, and the moby and containerd backends implement it.
- The `LimaVM` controller implements `PassthroughController` with a `shell` endpoint. It keeps the manager's client for the handler, and doesn't return a handler until it registers, as the engine controller does.

GUI:

- `ContainerShell.vue` becomes `TerminalSession.vue`, a terminal component that takes a passthrough endpoint and a target path. It opens the WebSocket, speaks the framing, sends a resize message whenever xterm's fit addon changes the size, splits large input, and shows how the shell ended. It replaces the `container-exec/*` IPC messages that `ContainerShell.vue` sent. The main process no longer handles them.
- The xterm.js setup (theme, font, fit addon, resize observer) moves into a composable shared with `ContainerLogs.vue`.
- The container info page shows its Shell tab again, with the terminal component on `exec`.
- The Virtual Machines page gets a Shell action in the row menu of each running VM, the App's VM included. The action opens a page for the VM, with its name and status in the header and the terminal component on `shell`.

## Tests

- Go unit tests cover the replay buffer and where the replay starts, the mode tracking, and the framing.
- Jest tests cover the GUI's framing and its replay handling, and check that only the banner's button reconnects.
- BATS tests drive both endpoints against a real VM, for a moby container, a containerd container, and a `LimaVM`. They use a hidden `rdd` subcommand built on client-go's `NewWebSocketExecutor`, because the `curl` command in `do_websocket` can only read. The subcommand builds with rdd, and it can become the later CLI command. The tests check output that differs from the typed command (`echo $((6*7))` prints `42`), the exit status, the size after a resize (`stty size`), the replay after a detach and a new attach, a second client taking a session over, a session ending when its container stops, and a container without `sh`. The output has to differ, because the terminal echoes the typed command, which would match any check for its own text.
- Two BATS tests open a shell and a `logs` stream with a short `timeout` query parameter, and check that each outlives it. Without the long-running exemption, the API server would cut both at that deadline.
- The e2e `Container Shell Tab` suite stays marked `fixme`. The e2e harness runs rdd with only the mock controller, which doesn't have a VM or a container engine. Until e2e can run against a VM, the GUI gets a manual check on a scratch instance. When the suite runs again, it checks output that differs from the typed command, and its test for a container without `script` becomes one for a container without `sh`.

Several shells per target, shells in containers without `sh`, and a limit on the number of sessions are out of scope.
