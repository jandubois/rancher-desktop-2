# Lima Cluster API Provider

Rancher Manager, installed into the App's k3s cluster, can provision downstream clusters through Cluster API (CAPI). The `capi` controller in RDD is a CAPI infrastructure provider for those clusters. It turns each machine of such a cluster into a `LimaVM` in the control plane, and the `LimaVM` controller boots that VM the same way it boots the App's VM.

The provider runs on macOS only, because its node VMs use Lima's vzNAT network, which needs the `vz` VM type, and `vz` exists only on macOS. It supports k3s downstream clusters only.

## Creating a cluster

This document calls the App's k3s cluster the guest cluster. The provider adds three kinds to it, in the API group `infrastructure.cluster.x-k8s.io`, version `v1alpha1`. CAPI infrastructure providers generally use this group, so CAPI's manager role can already manage the provider's objects, and Rancher's Standard User role can create them in `fleet-default`. A Rancher `Cluster` (`provisioning.cattle.io`) names a `LimaCluster` in `rkeConfig.infrastructureRef`, and each of its machine pools names a `LimaMachineTemplate` in `machineConfigRef`. This example creates a cluster with one node VM that has 2 CPUs and 3 GiB of memory.

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: LimaCluster
metadata:
  name: lima-demo
  namespace: fleet-default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: LimaMachineTemplate
metadata:
  name: lima-demo-all
  namespace: fleet-default
spec:
  template:
    spec:
      cpus: 2
      memory: 3Gi
---
apiVersion: provisioning.cattle.io/v1
kind: Cluster
metadata:
  name: lima-demo
  namespace: fleet-default
spec:
  kubernetesVersion: v1.36.6+k3s1
  rkeConfig:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
      kind: LimaCluster
      name: lima-demo
    machinePools:
    - name: all
      quantity: 1
      etcdRole: true
      controlPlaneRole: true
      workerRole: true
      machineConfigRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
        kind: LimaMachineTemplate
        name: lima-demo-all
```

A `LimaCluster` doesn't need a spec. Its `spec.controlPlaneEndpoint` defaults to `localhost:6443`, the value that Rancher's own `RKECluster` sets. A `LimaMachineTemplate` sets `cpus` (default 2), `memory` (default `3Gi`) and `disk` (default: Lima's disk size) for the machines that CAPI creates from it. Give each machine pool its own `LimaMachineTemplate`. Rancher's UI matches each `MachineDeployment` to its pool by the template's name, so when two pools share a template, the + and - buttons of one pool can scale the other.

The node VMs download Rancher's system agent from Rancher's `server-url`, so that URL has to name an address that they can reach. `localhost` doesn't work, because inside a node VM it names the VM itself. Use a host name that resolves to the host's LAN address instead, such as `rancher.192.168.1.10.sslip.io`. The App's VM forwards Rancher's HTTPS port on all of the host's addresses, so that name reaches Rancher from the host and from every node VM.

## How a cluster gets its nodes

1. Rancher creates a CAPI `Cluster` whose infrastructure is the `LimaCluster`, and a `MachineDeployment` for each machine pool, whose machines come from the pool's `LimaMachineTemplate`.
2. RDD marks the `LimaCluster` provisioned and ready. Rancher holds back each machine's bootstrap data until then.
3. CAPI creates a `Machine` for each machine in a pool, and a `LimaMachine` that the `Machine` owns, copied from the pool's template.
4. Rancher writes the `Machine`'s bootstrap data into a `Secret`. It's a cloud-config that runs the install script for Rancher's system agent.
5. RDD puts the install script into a Lima template and creates the node VM.
6. Once the VM runs and the script has succeeded, RDD sets the `LimaMachine`'s provider ID and addresses, marks it provisioned, and sets its `Ready` condition.
7. CAPI copies that condition into the `Machine`'s `InfrastructureReady` condition. Rancher then sends the node its plan, which installs k3s, and CAPI matches the new `Node` to the `Machine` by its provider ID.

## The capi controller

The controller is in `pkg/controllers/app/capi` and registers itself only on macOS. Its API group is empty, as the `kubernetes` controller's is, so only `*` (the default) or its own name in the `--controllers` option starts it. The group `lima` would also start it in a control plane that runs only the `lima` controllers. That control plane doesn't have the `App` CRD, so watching `App` objects there would fail the cache sync and stop the controller manager.

A single reconciler does all the work. It watches the `App` and the node `LimaVM` objects, and every event starts the same pass over the guest cluster. It doesn't watch the guest cluster. Instead, a pass that leaves a `LimaMachine` waiting runs again 5 seconds later, and a pass that finishes its work runs again 30 seconds later. So RDD can take up to 30 seconds to notice a new `LimaCluster` or `LimaMachine`. A `LimaMachine` with the `BootstrapFailed` reason doesn't count as waiting, because a failed install script runs again only at the VM's next boot. So when an install script is still running after Lima stops waiting for it, its `LimaMachine` can show up as provisioned up to 30 seconds after the script succeeds.

Each pass:

1. Starts or stops the node VMs to follow the App.
2. Ends unless the App's `Running` and `KubernetesReady` conditions are both true. The next change to either condition starts a new pass.
3. Connects to the guest cluster through `k3s.yaml`, the kubeconfig that RDD mirrors to the host. It builds a new client whenever that file changes, for example when k3s moves to another port.
4. Removes the node VMs of an earlier guest cluster, whose `kube-system` namespace had another UID.
5. Ends if the guest cluster lacks CAPI's `machines.cluster.x-k8s.io` CRD.
6. Installs the provider's CRDs, once per guest connection.
7. Marks every `LimaCluster` provisioned and ready. A `LimaCluster` doesn't have infrastructure of its own, so it's provisioned as soon as RDD sees it.
8. Removes node VMs whose `LimaMachine` is gone, then creates, probes or deletes the node VM of each `LimaMachine`.

A failed request to the guest cluster is logged, and the pass runs again 5 seconds later, because the guest cluster is unreachable for a while whenever the App starts or k3s restarts. An error from RDD's own control plane goes back to controller-runtime, which retries with backoff. When step 8 has both kinds of error, RDD returns both to controller-runtime. The backoff grows to at most 30 seconds. While one `LimaMachine`'s error persists, the backoff replaces the 5- and 30-second waits between passes, so every other `LimaMachine` can also wait up to 30 seconds for its next pass, unless an event starts one sooner.

The reconciler polls the guest cluster's API server, over a second controller manager that watches the guest cluster. That manager would react faster. But controller-runtime rejects a controller name that the process has already registered, so starting the manager again each time the guest cluster comes back fails unless it turns that check off. Its cache would also hold a copy of every guest `Secret`, Rancher's tokens included, and its watches would have to start and stop with the guest cluster. The polling reconciler keeps only the guest connection and the last probe of each node VM in memory, and the next pass rebuilds both. So a restart of RDD, of the App or of k3s doesn't need special handling.

## The guest API

The types are in `pkg/capi/v1alpha1`, outside `pkg/apis`. `scripts/generate-crds.sh` adds every CRD under `pkg/apis` to the CRD files that RDD installs in its own control plane, and to the mock controller's bundle. The provider's CRDs belong in the guest cluster instead, so the script writes them to `pkg/capi/crds.yaml`, which RDD embeds.

The types don't import CAPI's Go module. `APIEndpoint` and `MachineAddress` copy the JSON form of CAPI's v1beta2 types, and the provider reads `Machine` and `Cluster` objects, and the control plane object that a `Cluster` references, as unstructured data. Each of the provider's CRDs has the label `cluster.x-k8s.io/v1beta2: v1alpha1`, which tells CAPI that version `v1alpha1` implements its v1beta2 contract.

The provider installs its CRDs through `pkg/util/crds`, the same code that installs RDD's own. It creates a missing CRD, updates one whose spec or labels differ, and waits until the API server serves each new CRD. It does this once per guest connection, so a CRD deleted by hand comes back only when RDD restarts or `k3s.yaml` changes.

RDD installs the CRDs only once CAPI is present, over installing them whenever k3s runs or behind an App setting. A k3s cluster without CAPI stays untouched, and installing Rancher, which installs CAPI by default, is all a user has to do.

The provider doesn't ship RBAC rules, because CAPI's manager role already grants every verb on `infrastructure.cluster.x-k8s.io` resources.

## Node VMs

### The install script

Rancher stores a `Machine`'s bootstrap data in a `Secret` under the key `value`. In Rancher v2.15.3 that data is a cloud-config with an optional `## template: jinja` line, a `#cloud-config` line, one `write_files` entry that holds the install script at `/usr/local/custom_script/install.sh` (gzipped and base64-encoded), and a `runcmd` that runs that script with `sh`. The App's distro doesn't run cloud-init, so RDD takes the script out and passes it to Lima. RDD rejects any other cloud-config, because applying part of one would skip the rest without an error. It also rejects an install script larger than 1 MiB.

### The Lima template

The Lima template of a node VM has these settings.

- `images` names the App's embedded distro, so node VMs boot the same image as the App.
- `vmType: vz` and one vzNAT network with `guestToGuest: true`. macOS blocks traffic between vzNAT guests, and `guestToGuest` routes it through the host, so the nodes of a cluster reach each other.
- `containerd` is off, because the k3s that Rancher installs brings its own.
- One port forward rule ignores every guest port, so a node's k3s doesn't take host ports such as 6443 and 443 from the App's VM.
- `cpus`, `memory` and `disk` come from the `LimaMachine`.
- Two provision entries hold the install script, base64-encoded, in a `mode: data` file that only root can read, and a `mode: system` guard script that runs it.

Lima runs the content and script of every provision entry through Go's `text/template`, and logs the whole text when that fails. So the install script goes into the template as base64. Base64 never contains `{`, so `text/template` leaves the script unchanged and never fails on it, and Lima never logs it.

Lima runs `mode: system` scripts on every boot. The guard script exits at once when `/var/lib/lima-capi/bootstrapped` exists. Otherwise it installs `openssl-3` with zypper if `openssl` is missing, decodes the install script with umask 077, runs it, and creates that marker. A failed install exits before the marker, so the next boot tries again. The marker is outside `/var/lib/rancher`, where Rancher's system agent and k3s keep their state.

Rancher's install script checks Rancher's CA certificate with `openssl` when Rancher's certificate is self-signed, and the App's distro doesn't include `openssl`. "Rancher Desktop 2" doesn't run `openssl` anywhere else in the guest, so node VMs install it on their first boot, over adding it to the distro. That first boot needs the openSUSE package repositories.

Passing the script through Lima's provision entries, over copying it into the VM with SSH after boot, lets Lima retry a failed install on every boot without help from RDD. The cost is that the registration token is stored in more places (see [the registration token](#the-registration-token)).

The App's own k3s runs as `k3s-rancher-desktop.service`. The distro's systemd drop-ins apply to that unit only, so they don't touch the `k3s.service` that Rancher installs on a server node.

### Names

The `LimaVM` gets the `LimaMachine`'s name, and the guest's hostname is `lima-<name>`. RDD checks that the name is a valid Lima instance name, that the hostname has at most 63 characters, and that the longest socket path that Lima creates in the instance directory is shorter than macOS's limit of 104 bytes. That path starts with `~/.rd2/lima/<name>/`, so the longest usable name depends on the length of the home directory path. A name with a `.` fails too. Lima turns the dot into `-` in the hostname, so the `Node` that k3s names after the hostname wouldn't match the provider ID `k3s://lima-<name>` that RDD sets. A name that fails a check gets the `InvalidName` reason.

### Objects in the control plane

For each `LimaMachine`, RDD creates a `ConfigMap` named `<name>-source` that holds the Lima template, and a `LimaVM` named `<name>` that references it, both in the `rancher-desktop` namespace. Both have labels with the identity of the guest cluster (the UID of its `kube-system` namespace) and with the `LimaMachine`'s namespace, name and UID. Both also have a non-controller owner reference to the `App`, so deleting the `App` deletes them too. The `LimaVM` has the [owned finalizer](controllers.md#finalizers) `rdd.rancherdesktop.io/owned-by-LimaMachine`, so RDD's webhook rejects an attempt to delete it directly.

Once the `LimaVM` exists, RDD deletes the `<name>-source` `ConfigMap`. If creating the `LimaVM` fails, RDD deletes the `ConfigMap` too, so the next pass renders the template again and a corrected spec takes effect. The `LimaVM` controller copies the template into its own `ConfigMap`, `<name>-template`, when the `LimaVM` is created, and never reads the source again. The `-source` suffix keeps the input `ConfigMap` apart from these copies. Without it, a `LimaMachine` named `rd-template` would need the `ConfigMap` that holds the copy of the App VM's template, and would fail with `NameConflict`.

A `LimaVM` or `ConfigMap` in the `rancher-desktop` namespace that already has one of these names, but doesn't have a label with the `LimaMachine`'s UID, belongs to something else. So does a `LimaVM` named `<name>` in any other namespace, because Lima instance names are unique on the host. RDD leaves such an object alone and sets the `NameConflict` reason. RDD's webhook rejects a `LimaVM` whose name is taken in any namespace, but that error would reach only RDD's log. So RDD checks the name itself before it creates the `LimaVM`, and the conflict shows as the `LimaMachine`'s `Ready` reason.

### Readiness

RDD probes a node VM over SSH once its `LimaVM` reports `Running`. The probe reads the IPv4 address of `lima0`, the vzNAT interface, and checks for the guard's marker. Lima reports a VM as running once its boot scripts have run, whether or not they succeeded. It also reports the VM as running once it stops waiting for them. Lima doesn't report a failed boot script, and the `Degraded` status that it sets when it stops waiting doesn't reach the `LimaVM`. So RDD learns from the marker whether the install script succeeded, and it probes on every pass until it finds the marker. After that, RDD probes again only after a restart.

Once the probe finds both the address and the marker, RDD sets the `LimaMachine`'s `spec.providerID` to `k3s://lima-<name>`, the ID that k3s gives the `Node`, and puts the `lima0` address and the hostname in `status.addresses`. It then sets `status.initialization.provisioned` and marks the `LimaMachine` ready. RDD never unsets `provisioned`. Without the marker, the `Ready` reason is `BootstrapFailed`. A script that is still running can succeed later, and a failed one runs again at the next boot.

The `Ready` condition's reason tells how far a `LimaMachine` has got.

| Reason | Meaning |
| --- | --- |
| `WaitingForMachineOwner` | The `LimaMachine` doesn't have an owner `Machine` yet, or that `Machine` doesn't exist. |
| `InvalidName` | The name can't be a Lima instance name, contains a `.`, or makes the hostname or a socket path too long. |
| `NameConflict` | The `LimaVM` or `ConfigMap` that the `LimaMachine` needs belongs to something else, or a `LimaVM` in another namespace has its name. |
| `WaitingForControlPlane` | The `Machine` doesn't name its `Cluster` yet, the guest cluster doesn't serve the `Cluster` kind or the control plane's kind yet, or the `Cluster`, its `controlPlaneRef`, the control plane object or that object's version doesn't exist yet. |
| `UnsupportedDistribution` | The Kubernetes version of the cluster's control plane isn't a k3s version. RDD reads it from the control plane's `spec.kubernetesVersion`, where Rancher sets it, or else from `spec.version`, the field defined in Cluster API's contract. Rancher doesn't set the version on the `Machine`. |
| `WaitingForBootstrapData` | The `Machine` doesn't name its bootstrap `Secret` yet, or that `Secret` doesn't exist. |
| `UnsupportedBootstrapData` | The bootstrap data isn't the cloud-config that Rancher generates. |
| `Provisioning` | RDD is creating the node VM, or waits for it to run and answer the probe. |
| `CreateFailed`, `StartFailed` | The `LimaVM` controller couldn't create or start the node VM. |
| `BootstrapFailed` | The node VM runs, but the install script hasn't succeeded. |
| `Ready` | The node VM runs, and the install script has succeeded. The condition is `True`. |

For `CreateFailed` and `StartFailed`, the message names the `rdd ctl get limavm` command that shows the `LimaVM`'s own message. RDD doesn't copy that message, because Lima's YAML errors quote the template lines near the error, and the template holds the registration token. CAPI copies the condition onto the `Machine`, where users who may read `Machine` objects but not `Secret` objects would see the token.

### Running state

Node VMs run only while the App's `spec.running` and `spec.kubernetes.enabled` are both true, so stopping the App, or turning Kubernetes off, stops them too. Rancher and CAPI run in the App's k3s cluster and can't manage the node VMs while it is down.

### Deletion and garbage collection

RDD adds the finalizer `capi.rancherdesktop.io/limamachine` to a `LimaMachine` once its owner `Machine` exists. When the `LimaMachine` is deleted, RDD deletes its `ConfigMap` and `LimaVM`, waits while the `LimaVM` controller stops the VM and deletes its disk, and then removes the finalizer. CAPI in turn waits for the `LimaMachine` to disappear before it deletes the `Machine`.

Every pass that reaches the guest cluster also removes the node `LimaVM` and `ConfigMap` objects that belong to another guest cluster, whether or not CAPI is present. A pass that gets as far as listing the `LimaMachine` objects also removes those of a `LimaMachine` that no longer exists. Resetting the App's k3s cluster leaves the objects of the old guest cluster behind, and removing a `LimaMachine`'s finalizer by hand leaves the objects of that `LimaMachine`. The `ConfigMap` objects hold registration tokens, so RDD removes them along with the VMs.

## The registration token

Rancher's bootstrap data includes the token that the node's system agent registers with. The provider's errors and conditions never quote it, and its log lines quote it only in the case at the end of this section. Its errors about the bootstrap data name the field and what it expected, and it drops YAML parse errors, which can quote their input.

The token is stored in these places.

- The bootstrap `Secret` in the guest cluster. RDD reads it only until the node VM exists.
- The `<name>-source` `ConfigMap`, until the `LimaVM` exists.
- The `LimaVM`'s validated template, `<name>-template`, for the life of the VM.
- `lima.yaml` and `cidata.iso` in the Lima instance directory, `~/.rd2/lima/<name>`, which only the user can read.
- `install.sh.b64` and `install.sh` in `/usr/local/custom_script` in the guest, which only root can read.

One path can still put the token in RDD's log on the host. If Lima ever failed to parse a template that it marshalled itself, the error would quote template lines. RDD's webhook would then reject the node VM, and controller-runtime logs the create error that the provider returns. At verbosity 5, controller-runtime's webhook server logs each rejection as well. The provider also logs a failed probe at verbosity 1. Each of these log lines would then include the token.

## Not supported

CAPI's contract says that a provider should honor a paused cluster (`spec.paused` or the `cluster.x-k8s.io/paused` annotation) and the `cluster.x-k8s.io/managed-by` label. The provider ignores both. Rancher pauses a cluster only during certificate rotation, encryption key rotation and etcd restore, and CAPI's `MachineDeployment`, `MachineSet` and `Machine` controllers stop then too, so CAPI doesn't create or delete `LimaMachine` objects during a pause. Rancher never sets the label.

## Tests

The `bats-capi` suite in `bats/tests/35-capi-controllers` tests the provider without Rancher. It installs minimal CRDs for CAPI's `Machine` and `Cluster` and for Rancher's `RKEControlPlane`, and creates one object of each kind, a `LimaMachine` and a bootstrap `Secret` whose script appends a line to a file. Then it follows the node VM through creation, a restart, an App stop and deletion. CI runs it on macOS only.
