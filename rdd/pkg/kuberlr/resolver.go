// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package kuberlr

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/discovery"
	componentbasever "k8s.io/component-base/version"
	kubectlcmd "k8s.io/kubectl/pkg/cmd"
)

// envSkipResolver breaks a loop through the kubeconfig's credential
// plugin. Resolve's cluster probe runs the exec credential plugin to
// authenticate, even for command lines that kubectl runs without
// credentials, such as a kuberc alias of `config view`. A credential
// plugin that runs such a command line reaches rdd kubectl again through
// the kubectl shim, and without the guard that nested call would probe
// and run the plugin again, without end. A credential plugin run as a
// top-level kubectl plugin, like kubelogin's `kubectl oidc-login
// get-token`, never probes, because such plugins are client-only (see
// parseCommandLine). Resolve sets the variable only while it probes, so
// the credential plugin's nested call skips resolution. The kubectl that
// rdd then runs does not inherit the variable, so the kubectl calls that
// a kubectl plugin makes still resolve. kuberlr guards the same loop with
// KUBERLR_RESOLVING_VERSION.
// TestResolveSetsRecursionGuardWhileProbing pins when Resolve sets and
// unsets the variable.
const envSkipResolver = "RDD_KUBECTL_RESOLVED"

// skipResolver is the same-process counterpart to envSkipResolver.
var skipResolver bool

// SkipResolver short-circuits Resolve for the rest of this process.
// rdd ctl calls this because its embedded apiserver always matches
// the embedded kubectl's version by construction.
func SkipResolver() {
	skipResolver = true
}

// serverProbeTimeout caps the discovery call so an unreachable cluster
// can't stall every kubectl invocation; reachable clusters answer in <100ms.
const serverProbeTimeout = 2 * time.Second

// Resolve returns the path of a kubectl acceptable for the cluster that
// parseCommandLine selects, or "" to use the embedded kubectl.
//
// It prefers the embedded kubectl, then the highest acceptable cached
// binary, and only when both miss downloads the latest patch release
// of the server's minor. It returns "" for client-only commands, for
// command lines that kubectl's command tree cannot parse (see
// parseCommandLine for both), and when the probe fails for any reason.
// It also returns "", with a warning, when it cannot determine the
// version to download. But a determined version that fails to download
// or verify is an error, and so is a failure to set the recursion guard.
func Resolve(ctx context.Context, args []string) (string, error) {
	if skipResolver || os.Getenv(envSkipResolver) != "" {
		return "", nil
	}
	clientOnly, cf, err := parseCommandLine(args)
	if err != nil {
		// kubectl rejects most such command lines too, and the embedded
		// kubectl reports the error. But kubectl accepts one where the
		// arguments that a kuberc alias adds supply a flag's value.
		logrus.WithError(err).Debug("kubectl resolver: cannot parse command line; using embedded kubectl")
		return "", nil
	}
	if clientOnly {
		return "", nil
	}
	embedded, err := embeddedVersion()
	if err != nil {
		// `go run` and IDE debug builds skip the Makefile's -X flags,
		// leaving the unparseable in-tree default (v0.0.0-master+...);
		// fall through rather than break every dev invocation.
		logrus.WithError(err).Debug("kubectl resolver: embedded version not parseable; using embedded kubectl")
		return "", nil
	}
	server, ok, err := probeServerVersion(cf)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	if acceptable(embedded, server) {
		return "", nil
	}
	if path, ok := bestCached(server); ok {
		return path, nil
	}
	want, err := latestPatch(ctx, server)
	if err != nil {
		// Final fallback: rdd embeds a recent kubectl and Kubernetes
		// rarely breaks backward compatibility, so running it beats
		// failing when the mirror is unreachable or has no marker yet.
		logrus.WithError(err).Warnf("kubectl resolver: cannot determine the latest v%d.%d patch release; using embedded kubectl", server.Major, server.Minor)
		return "", nil
	}
	return ensureCached(ctx, want)
}

// acceptable reports whether a kubectl of client's version supports
// every feature of a server of server's version: same Major, and a
// Minor equal to the server's or one higher. One minor behind is
// still within the official ±1 skew, but lacks the features added in
// the server's minor, so it is rejected. Patch versions never matter.
func acceptable(client, server semver.Version) bool {
	if client.Major != server.Major {
		return false
	}
	diff := int64(client.Minor) - int64(server.Minor)
	return diff == 0 || diff == 1
}

// embeddedVersion reads the kubectl version baked in via the Makefile's
// -X flags; it's a var so tests can stub the dev-build parse failure.
var embeddedVersion = func() (semver.Version, error) {
	return semver.ParseTolerant(componentbasever.Get().GitVersion)
}

// probeServerVersion runs serverVersion with envSkipResolver set in
// this process, so an exec credential plugin that calls kubectl again
// skips resolution; the variable is unset again before it returns.
func probeServerVersion(cf *genericclioptions.ConfigFlags) (semver.Version, bool, error) {
	if err := os.Setenv(envSkipResolver, "1"); err != nil {
		return semver.Version{}, false, fmt.Errorf("setting %s: %w", envSkipResolver, err)
	}
	defer func() { _ = os.Unsetenv(envSkipResolver) }()
	v, ok := serverVersion(cf)
	return v, ok, nil
}

// serverVersion probes the cluster that cf selects. ok is false on any
// failure, all logged at debug except unparseable version (warn — a
// garbled apiserver /version response is worth surfacing by default).
func serverVersion(cf *genericclioptions.ConfigFlags) (semver.Version, bool) {
	// ToRESTConfig applies the WrapConfigFn that kuberc's
	// credentialPluginPolicy installs, so the probe runs only the exec
	// plugins that kubectl would run.
	cfg, err := cf.ToRESTConfig()
	if err != nil {
		logrus.WithError(err).Debug("kubectl resolver: cannot load kubeconfig; using embedded kubectl")
		return semver.Version{}, false
	}
	// Force the fast probe timeout, overriding any --request-timeout in
	// args; the kubectl child itself still honors the user's value.
	cfg.Timeout = serverProbeTimeout
	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		logrus.WithError(err).Debug("kubectl resolver: cannot build discovery client; using embedded kubectl")
		return semver.Version{}, false
	}
	info, err := client.ServerVersion()
	if err != nil {
		logrus.WithError(err).Debug("kubectl resolver: cluster probe failed; using embedded kubectl")
		return semver.Version{}, false
	}
	v, err := semver.ParseTolerant(info.GitVersion)
	if err != nil {
		logrus.WithError(err).Warnf("kubectl resolver: cannot parse server version %q; using embedded kubectl", info.GitVersion)
		return semver.Version{}, false
	}
	return v, true
}

// parseCommandLine parses args through kubectl's own command tree, with
// the user's kuberc applied. It reports whether the command needs no
// cluster probe, and returns the ConfigFlags that select the cluster to
// probe. Parsing the way kubectl does gets attached shorthand values
// (-ojson), kuberc defaults and alias options, and the kuberc credential
// plugin policy right.
//
// It does not see an alias's prepended and appended args, because
// kuberc hands the rewritten command line to cobra's Execute, which this
// parse never runs. An alias also counts as cluster-bound, whatever it
// runs. A command line that the tree cannot find, such as a
// shell-completion request or a top-level kubectl plugin, is
// client-only, and so is a plugin under `create`, such as
// kubectl-create-foo. Flags that the tree lacks, such as klog's --v, are
// skipped.
// The klog flags stay out of the tree because parsing them would change
// klog's process-global state. A malformed known flag returns the parse
// error, so the caller falls through to the embedded kubectl instead of
// probing the default cluster.
func parseCommandLine(args []string) (clientOnly bool, cf *genericclioptions.ConfigFlags, err error) {
	// kubectl binds the deprecated --username/--password flags, and a
	// kuberc may name them.
	cf = genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()
	if len(args) == 0 {
		return true, cf, nil
	}
	// NewKubectlCommand exits the process when the kuberc fails to load
	// or validate, with kubectl's own error on stderr. That stops rdd
	// kubectl even when the kubectl it would run is older and ignores
	// kuberc. Non-fatal kuberc warnings go to stderr too, and a kubectl
	// that reads kuberc prints them again.
	root := kubectlcmd.NewKubectlCommand(kubectlcmd.KubectlOptions{
		ConfigFlags: cf,
		Arguments:   append([]string{"kubectl"}, args...),
		IOStreams:   genericiooptions.IOStreams{In: os.Stdin, Out: io.Discard, ErrOut: os.Stderr},
	})
	// Cobra prints deprecated-flag notices while parsing, and kubectl
	// prints them again when it runs.
	root.SetOut(io.Discard)
	// cobra adds the help command and flag only in Execute.
	root.InitDefaultHelpCmd()
	cmd, rest, err := root.Find(args)
	if err != nil {
		// The tree lacks shell-completion requests (`__complete`),
		// top-level kubectl plugins and unknown commands, including any
		// top-level command that only a newer kubectl has. kubectl does
		// not contact a cluster for a plugin or an unknown command.
		// Skipping the probe for completion spares every TAB the probe
		// and any download, whose progress the shell would hide.
		return true, cf, nil
	}
	// kubectl runs `create <name>` as the plugin kubectl-create-<name> when
	// create has no such subcommand, and does not contact a cluster for it.
	// kubectl releases before 1.29 do not run such plugins by default.
	if cmd.Parent() == root && kubectlcmd.IsSubcommandPluginAllowed(cmd.Name()) &&
		len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		return true, cf, nil
	}
	cmd.InitDefaultHelpFlag()
	cmd.FParseErrWhitelist.UnknownFlags = true
	if err := cmd.ParseFlags(rest); err != nil {
		return false, nil, err
	}
	// A bound Password makes ToRawKubeConfigLoader prompt on stdin, which
	// would hang the probe.
	cf.Username, cf.Password = nil, nil
	if help, _ := cmd.Flags().GetBool("help"); help {
		return true, cf, nil
	}
	return isClientOnly(cmd), cf, nil
}

// clientOnlySubcommands lists kubectl subcommands that never contact a
// cluster. config and kuberc touch local state, kustomize renders a
// kustomization, plugin lists local PATH entries, and completion,
// options, and help print text. kubectl releases before 1.36 do not have
// a top-level kuberc command, so `kubectl kuberc` must run in the
// embedded kubectl.
var clientOnlySubcommands = map[string]bool{
	"completion": true,
	"config":     true,
	"kuberc":     true,
	"kustomize":  true,
	"plugin":     true,
	"options":    true,
	"help":       true,
}

// isClientOnly reports whether cmd, found and parsed in kubectl's
// command tree, needs no cluster probe. The root command itself only
// prints help. For any other command, its top-level subcommand decides.
func isClientOnly(cmd *cobra.Command) bool {
	if !cmd.HasParent() {
		return true
	}
	for cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	if cmd.Name() == "version" {
		// `kubectl version` without --client probes the server, so
		// treat it as cluster-bound.
		client, _ := cmd.Flags().GetBool("client")
		return client
	}
	return clientOnlySubcommands[cmd.Name()]
}
