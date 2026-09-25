// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package image

import (
	"context"
	"errors"
	"fmt"

	"github.com/distribution/reference"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/api"
)

type imagePullRequestValidator struct {
	reader client.Reader
}

// ValidateCreate implements [ctrlwebhookadmission.Validator].
func (v *imagePullRequestValidator) ValidateCreate(ctx context.Context, imagePullRequest *v1alpha1.ImagePullRequest) (ctrlwebhookadmission.Warnings, error) {
	var errs []error
	// spec.namespace is optional: it defaults to the default namespace.
	if imagePullRequest.Spec.Namespace != "" {
		// Check that the specified ContainerNamespace exists.  If this fails (e.g.
		// because the engine is not ready), reject the change (/ create); we cannot
		// guarantee that the namespace is valid in that case.  We do not need to
		// worry about already existing image pull requests; if it is already known
		// to be valid, we don't care if the engine is not ready yet.
		// Note that the namespace name may be encoded.
		key := types.NamespacedName{
			Namespace: imagePullRequest.Namespace,
			Name:      api.MirrorName[*v1alpha1.ContainerNamespace](imagePullRequest.Spec.Namespace),
		}
		var containerNamespace v1alpha1.ContainerNamespace
		err := v.reader.Get(ctx, key, &containerNamespace)
		if apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("spec.namespace %q does not refer to an existing ContainerNamespace in namespace %q", imagePullRequest.Spec.Namespace, imagePullRequest.Namespace))
		} else if err != nil {
			errs = append(errs, fmt.Errorf("failed to validate spec.namespace %q in namespace %q: %w", imagePullRequest.Spec.Namespace, imagePullRequest.Namespace, err))
		}
	}

	errs = append(errs, v.validate(ctx, imagePullRequest))

	return nil, errors.Join(errs...)
}

// ValidateUpdate implements [ctrlwebhookadmission.Validator].
func (v *imagePullRequestValidator) ValidateUpdate(ctx context.Context, oldImagePullRequest, newImagePullRequest *v1alpha1.ImagePullRequest) (ctrlwebhookadmission.Warnings, error) {
	var errs []error
	if oldImagePullRequest.Spec.Namespace != newImagePullRequest.Spec.Namespace {
		errs = append(errs, errors.New("spec.namespace is immutable"))
	}
	if oldImagePullRequest.Spec.RepoTag != newImagePullRequest.Spec.RepoTag {
		errs = append(errs, errors.New("spec.repoTag is immutable"))
	}
	errs = append(errs, v.validate(ctx, newImagePullRequest))

	return nil, errors.Join(errs...)
}

// ValidateDelete implements [ctrlwebhookadmission.Validator].
func (v *imagePullRequestValidator) ValidateDelete(_ context.Context, _ *v1alpha1.ImagePullRequest) (ctrlwebhookadmission.Warnings, error) {
	return nil, nil
}

func (v *imagePullRequestValidator) validate(_ context.Context, imagePullRequest *v1alpha1.ImagePullRequest) error {
	if _, err := reference.ParseNormalizedNamed(imagePullRequest.Spec.RepoTag); err != nil {
		return fmt.Errorf("invalid spec.repoTag: %w", err)
	}

	return nil
}

var _ ctrlwebhookadmission.Validator[*v1alpha1.ImagePullRequest] = &imagePullRequestValidator{}
