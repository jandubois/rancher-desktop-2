// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"testing"

	"gotest.tools/v3/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
)

func TestNerdctlAppError(t *testing.T) {
	engineReady := func(gen int64) []metav1.Condition {
		return []metav1.Condition{{
			Type:               appv1alpha1.AppConditionContainerEngineReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: gen,
		}}
	}
	cases := []struct {
		name       string
		engine     string
		conditions []metav1.Condition
		wantErr    string
	}{
		{"moby", "moby", engineReady(2), `but the App uses "moby"`},
		{"no condition", "containerd", nil, "containerd engine is not ready"},
		{"stale condition", "containerd", engineReady(1), "containerd engine is not ready"},
		{"ready", "containerd", engineReady(2), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &appv1alpha1.App{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appv1alpha1.AppSpec{ContainerEngine: appv1alpha1.ContainerEngineSpec{Name: tc.engine}},
				Status:     appv1alpha1.AppStatus{Conditions: tc.conditions},
			}
			err := nerdctlAppError(app)
			if tc.wantErr == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
