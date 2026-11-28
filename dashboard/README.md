# Cluster Explorer dashboard

This directory holds `pkg/rancher-deskboard`, our plugin for Rancher's
dashboard, and the Makefile that builds the dashboard with it. rdd embeds the
result and serves it through `rdd steve`.

The Makefile downloads a pinned commit of rancher-sandbox/dashboard, which is a
Rancher release tag plus our fixes to Rancher's shell. It copies the plugin
into that tree's `pkg/` folder and builds there, installing from Rancher's own
`package.json` and `yarn.lock`. So the dashboard gets the dependency versions
that Rancher builds its own release with, and this repository doesn't keep a
lockfile for it. Rancher's lockfile is for Yarn 1, so the build runs Yarn 1
through Corepack.

The Makefile also builds `pkg/rancher-desktop`, a Rancher UI extension that
adds Rancher Desktop to the Cluster API providers on Rancher's Create page.
rdd serves it to a Rancher Manager installed in Rancher Desktop's Kubernetes
cluster. The dashboard build registers every package under `pkg/` that has
a `rancher` field as a builtin extension, but this extension doesn't belong
in the dashboard that rdd embeds. So the Makefile copies it into Rancher's
tree only after building the dashboard.

We download a checksummed archive instead of adding rancher-sandbox/dashboard
as a git submodule. With a submodule, every clone and worktree would have to
initialize it.

The comments in the Makefile say how to move to a new Rancher tag or shell
commit.
