// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package base

import (
	"testing"

	"gotest.tools/v3/assert"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	admissionregistrationv1apply "k8s.io/client-go/applyconfigurations/admissionregistration/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWebhookTimeoutSeconds(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	clientConfig := admissionregistrationv1apply.WebhookClientConfig().WithURL("https://127.0.0.1:1/x")
	apply := func(t *testing.T, webhookType WebhookType, timeout *int32) {
		t.Helper()
		wm := &webhookManagerImpl[*corev1.ConfigMap]{
			config:      WebhookConfig[*corev1.ConfigMap]{Name: "test", WebhookName: "test.example.com", TimeoutSeconds: timeout},
			webhookType: webhookType,
		}
		var err error
		if webhookType == MutatingWebhook {
			err = wm.createMutatingWebhook(t.Context(), c, clientConfig, nil, admissionregistrationv1.Fail, admissionregistrationv1.SideEffectClassNone)
		} else {
			err = wm.createValidatingWebhook(t.Context(), c, clientConfig, nil, admissionregistrationv1.Fail, admissionregistrationv1.SideEffectClassNone)
		}
		assert.NilError(t, err)
	}

	apply(t, MutatingWebhook, ptr.To[int32](30))
	mutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
	assert.NilError(t, c.Get(t.Context(), client.ObjectKey{Name: "test"}, mutating))
	assert.DeepEqual(t, mutating.Webhooks[0].TimeoutSeconds, ptr.To[int32](30))

	apply(t, ValidatingWebhook, nil)
	validating := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	assert.NilError(t, c.Get(t.Context(), client.ObjectKey{Name: "test"}, validating))
	assert.Assert(t, validating.Webhooks[0].TimeoutSeconds == nil)
}
