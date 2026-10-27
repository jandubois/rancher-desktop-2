# Other Commands

## `rdd kubectl`

Runs the regular `kubectl` program. Since `rdd` needs to be a kube client anyways, this functionality is basically available for free (at least from a code size point of view).

`rdd` is a multi-call binary (like `busybox`), so `rdd kubectl` can also be invoked via a symlink called `kubectl`.

There is also the [rdd ctl](cmd_service.md#rdd-ctl) command that automatically sets the kube config and context to the rdd control plane before executing `kubectl`.

## `rdd nerdctl`

Runs `nerdctl` inside the App's VM, against the shared containerd socket. Like `rdd kubectl`, it can also be invoked via a link called `nerdctl`.

It never starts the control plane or the App, and fails unless the App runs the containerd engine and that engine is ready. `rdd run nerdctl` starts the App first (see [rdd run](cmd_app.md#rdd-run)).

## `rdd cache`

TBD

## `rdd snapshot`

TBD

## `rdd package`

TBD
