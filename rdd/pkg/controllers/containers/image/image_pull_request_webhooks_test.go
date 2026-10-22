// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package image

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// k8sNamespace differs from the App's namespace, so a test fails if a
// controller hard-codes the App's namespace instead of using the object's.
const k8sNamespace = "test-namespace"

func newImageWebhookTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	assert.NilError(t, v1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func TestImagePullRequestValidatorValidateCreate(t *testing.T) {
	t.Run("valid with existing container namespace", func(t *testing.T) {
		containerNamespace := &v1alpha1.ContainerNamespace{}
		containerNamespace.Name = "moby"
		containerNamespace.Namespace = k8sNamespace
		c := newImageWebhookTestClient(t, containerNamespace)
		v := &imagePullRequestValidator{reader: c}

		req := &v1alpha1.ImagePullRequest{}
		req.Namespace = k8sNamespace
		req.Spec.Namespace = "moby"
		req.Spec.RepoTag = "alpine:latest"

		warnings, err := v.ValidateCreate(t.Context(), req)
		assert.NilError(t, err)
		assert.Assert(t, warnings == nil)
	})

	t.Run("valid when spec.namespace is empty", func(t *testing.T) {
		c := newImageWebhookTestClient(t)
		v := &imagePullRequestValidator{reader: c}

		req := &v1alpha1.ImagePullRequest{}
		req.Namespace = k8sNamespace
		req.Spec.RepoTag = "alpine:latest"

		warnings, err := v.ValidateCreate(t.Context(), req)
		assert.NilError(t, err)
		assert.Assert(t, warnings == nil)
	})

	t.Run("invalid when container namespace is missing", func(t *testing.T) {
		c := newImageWebhookTestClient(t)
		v := &imagePullRequestValidator{reader: c}

		req := &v1alpha1.ImagePullRequest{}
		req.Namespace = k8sNamespace
		req.Spec.Namespace = "missing"
		req.Spec.RepoTag = "alpine:latest"

		warnings, err := v.ValidateCreate(t.Context(), req)
		assert.Assert(t, warnings == nil)
		assert.ErrorContains(t, err, fmt.Sprintf(`spec.namespace "missing" does not refer to an existing ContainerNamespace in namespace %q`, k8sNamespace))
	})

	t.Run("invalid when repo tag cannot be parsed", func(t *testing.T) {
		containerNamespace := &v1alpha1.ContainerNamespace{}
		containerNamespace.Name = "moby"
		containerNamespace.Namespace = k8sNamespace
		c := newImageWebhookTestClient(t, containerNamespace)
		v := &imagePullRequestValidator{reader: c}

		req := &v1alpha1.ImagePullRequest{}
		req.Namespace = k8sNamespace
		req.Spec.Namespace = "moby"
		req.Spec.RepoTag = "Invalid Repo Tag"

		warnings, err := v.ValidateCreate(t.Context(), req)
		assert.Assert(t, warnings == nil)
		assert.ErrorContains(t, err, "invalid spec.repoTag")
	})
}

func TestImagePullRequestValidatorValidateUpdate(t *testing.T) {
	containerNamespace := &v1alpha1.ContainerNamespace{}
	containerNamespace.Name = "moby"
	containerNamespace.Namespace = k8sNamespace
	c := newImageWebhookTestClient(t, containerNamespace)
	v := &imagePullRequestValidator{reader: c}

	t.Run("valid when spec.repoTag is unchanged", func(t *testing.T) {
		oldReq := &v1alpha1.ImagePullRequest{}
		oldReq.Namespace = k8sNamespace
		oldReq.Spec.Namespace = "moby"
		oldReq.Spec.RepoTag = "busybox:latest"

		newReq := &v1alpha1.ImagePullRequest{}
		newReq.Namespace = k8sNamespace
		newReq.Spec.Namespace = "moby"
		newReq.Spec.RepoTag = "busybox:latest"

		warnings, err := v.ValidateUpdate(t.Context(), oldReq, newReq)
		assert.NilError(t, err)
		assert.Assert(t, warnings == nil)
	})

	t.Run("invalid when spec.repoTag changes", func(t *testing.T) {
		oldReq := &v1alpha1.ImagePullRequest{}
		oldReq.Namespace = k8sNamespace
		oldReq.Spec.Namespace = "moby"
		oldReq.Spec.RepoTag = "alpine:latest"

		newReq := &v1alpha1.ImagePullRequest{}
		newReq.Namespace = k8sNamespace
		newReq.Spec.Namespace = "moby"
		newReq.Spec.RepoTag = "busybox:latest"

		warnings, err := v.ValidateUpdate(t.Context(), oldReq, newReq)
		assert.Assert(t, warnings == nil)
		assert.ErrorContains(t, err, "spec.repoTag is immutable")
	})

	t.Run("invalid when spec.namespace changes", func(t *testing.T) {
		oldReq := &v1alpha1.ImagePullRequest{}
		oldReq.Namespace = k8sNamespace
		oldReq.Spec.Namespace = "moby"
		oldReq.Spec.RepoTag = "busybox:latest"

		newReq := &v1alpha1.ImagePullRequest{}
		newReq.Namespace = k8sNamespace
		newReq.Spec.Namespace = "other"
		newReq.Spec.RepoTag = "busybox:latest"

		warnings, err := v.ValidateUpdate(t.Context(), oldReq, newReq)
		assert.Assert(t, warnings == nil)
		assert.ErrorContains(t, err, "spec.namespace is immutable")
	})
}

func TestImagePullRequestValidatorValidateDelete(t *testing.T) {
	c := newImageWebhookTestClient(t)
	v := &imagePullRequestValidator{reader: c}

	req := &v1alpha1.ImagePullRequest{}
	req.Namespace = k8sNamespace
	req.Spec.Namespace = "moby"
	req.Spec.RepoTag = "alpine:latest"

	warnings, err := v.ValidateDelete(t.Context(), req)
	assert.NilError(t, err)
	assert.Assert(t, warnings == nil)
}
