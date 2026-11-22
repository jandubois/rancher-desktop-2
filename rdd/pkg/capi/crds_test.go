// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"gotest.tools/v3/assert"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// decodeCRDs returns the CRDs in the embedded bundle.
func decodeCRDs(t *testing.T) []apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	decoder := yaml.NewYAMLToJSONDecoder(bytes.NewBufferString(CRDs))
	var crds []apiextensionsv1.CustomResourceDefinition
	for {
		var crd apiextensionsv1.CustomResourceDefinition
		err := decoder.Decode(&crd)
		if errors.Is(err, io.EOF) {
			return crds
		}
		assert.NilError(t, err)
		if crd.Name == "" {
			// The header comment before the first document decodes to an empty CRD.
			continue
		}
		crds = append(crds, crd)
	}
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
