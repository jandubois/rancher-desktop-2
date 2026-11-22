// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"encoding/json"
	"testing"

	"gotest.tools/v3/assert"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/crds"
)

// decodeCRDs returns the CRDs in the embedded bundle.
func decodeCRDs(t *testing.T) []apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	decoded, err := crds.Decode(CRDs)
	assert.NilError(t, err)
	return decoded
}

func TestCRDs(t *testing.T) {
	var names []string
	for _, crd := range decodeCRDs(t) {
		names = append(names, crd.Name)
		assert.Equal(t, crd.Labels["cluster.x-k8s.io/v1beta2"], "v1alpha1", crd.Name)
	}
	assert.DeepEqual(t, names, []string{
		"limaclusters.infrastructure.cluster.x-k8s.io",
		"limamachines.infrastructure.cluster.x-k8s.io",
		"limamachinetemplates.infrastructure.cluster.x-k8s.io",
	})
}

// TestLimaClusterDefaultsControlPlaneEndpoint checks that a LimaCluster created
// without a spec still gets the default control plane endpoint, which CAPI
// copies into its Cluster. The test applies the CRD's defaults with the API
// server's own defaulting code.
func TestLimaClusterDefaultsControlPlaneEndpoint(t *testing.T) {
	var limaCluster *apiextensionsv1.CustomResourceDefinition
	for _, crd := range decodeCRDs(t) {
		if crd.Spec.Names.Kind == "LimaCluster" {
			limaCluster = &crd
		}
	}
	assert.Assert(t, limaCluster != nil)
	var schema apiextensions.JSONSchemaProps
	assert.NilError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
		limaCluster.Spec.Versions[0].Schema.OpenAPIV3Schema, &schema, nil))
	structural, err := structuralschema.NewStructural(&schema)
	assert.NilError(t, err)

	obj := map[string]any{"kind": "LimaCluster"}
	structuraldefaulting.Default(obj, structural)
	spec, err := json.Marshal(obj["spec"])
	assert.NilError(t, err)
	assert.Equal(t, string(spec), `{"controlPlaneEndpoint":{"host":"localhost","port":6443}}`)
}

// TestLimaMachineRejectsInvalidSizes checks that the LimaMachine and
// LimaMachineTemplate CRDs reject a negative cpus value, and a memory or disk
// value that isn't positive. Without these rules, a negative memory or disk
// reaches the Lima template webhook, which rejects the node VM with an error
// that only rdd's log shows. The test runs the API server's own schema and CEL
// validation.
func TestLimaMachineRejectsInvalidSizes(t *testing.T) {
	kinds := 0
	for _, crd := range decodeCRDs(t) {
		kind := crd.Spec.Names.Kind
		if kind == "LimaCluster" {
			continue
		}
		kinds++
		var schema apiextensions.JSONSchemaProps
		assert.NilError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
			crd.Spec.Versions[0].Schema.OpenAPIV3Schema, &schema, nil))
		structural, err := structuralschema.NewStructural(&schema)
		assert.NilError(t, err)
		schemaValidator, _, err := validation.NewSchemaValidator(&schema)
		assert.NilError(t, err)
		celValidator := cel.NewValidator(structural, true, celconfig.PerCallLimit)
		for _, tc := range []struct {
			field string
			value any
			valid bool
		}{
			{"cpus", int64(0), true},
			{"cpus", int64(2), true},
			{"cpus", int64(-1), false},
			{"memory", "4Gi", true},
			{"memory", "0", false},
			{"memory", "-1Gi", false},
			{"memory", int64(1 << 30), true},
			{"memory", int64(0), false},
			{"memory", int64(-1 << 30), false},
			{"disk", "100Gi", true},
			{"disk", "0", false},
			{"disk", "-1Gi", false},
			{"disk", int64(100 << 30), true},
			{"disk", int64(0), false},
			{"disk", int64(-1 << 30), false},
		} {
			spec := map[string]any{tc.field: tc.value}
			if kind == "LimaMachineTemplate" {
				spec = map[string]any{"template": map[string]any{"spec": spec}}
			}
			obj := map[string]any{"spec": spec}
			errs := validation.ValidateCustomResource(field.NewPath(""), obj, schemaValidator)
			celErrs, _ := celValidator.Validate(t.Context(), field.NewPath(""), structural, obj, nil, celconfig.RuntimeCELCostBudget)
			errs = append(errs, celErrs...)
			assert.Equal(t, len(errs) == 0, tc.valid, "%s %s=%#v: %v", kind, tc.field, tc.value, errs)
		}
	}
	assert.Equal(t, kinds, 2)
}
