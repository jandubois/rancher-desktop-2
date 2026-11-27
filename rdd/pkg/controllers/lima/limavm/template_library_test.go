// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"gotest.tools/v3/assert"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/limatemplates"
)

// libraryEntry returns an unmanaged library ConfigMap.
func libraryEntry(namespace, name, path, content string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Name:        name,
		Namespace:   namespace,
		Labels:      map[string]string{TemplateLibraryLabel: "true"},
		Annotations: map[string]string{TemplatePathAnnotation: path},
		Data:        map[string]string{v1alpha1.TemplateConfigMapKey: content},
	}
}

func TestLibraryConfigMapName(t *testing.T) {
	names := map[string]bool{}
	for _, path := range []string{
		"_images/ubuntu-24.04.yaml", "images/ubuntu-24.04.yaml", "default.yaml", "Default.yaml",
		"_default/mounts.yaml", "experimental/wsl2.yaml", "scripts/setup.sh", "_.yaml",
	} {
		name := libraryConfigMapName(path)
		assert.Equal(t, len(validation.IsDNS1123Subdomain(name)), 0, "%s -> %s", path, name)
		assert.Assert(t, !names[name], "%s -> %s collides", path, name)
		names[name] = true
		assert.Equal(t, libraryConfigMapName(path), name, "not deterministic")
	}
	name := libraryConfigMapName("_images/ubuntu-24.04.yaml")
	assert.Assert(t, strings.HasPrefix(name, "images.ubuntu-24.04-") && len(name) == len("images.ubuntu-24.04-01234567"), name)
}

func TestConfigMapFS(t *testing.T) {
	c := fake.NewClientBuilder().WithObjects(
		libraryEntry("ns", "a", "_images/a.yaml", "arch: x86_64\n"),
		libraryEntry("other", "b", "b.yaml", "other namespace\n"),
		&corev1.ConfigMap{
			Name: "unlabeled", Namespace: "ns",
			Annotations: map[string]string{TemplatePathAnnotation: "c.yaml"},
			Data:        map[string]string{v1alpha1.TemplateConfigMapKey: "no library label\n"},
		},
	).Build()

	fsys := newConfigMapFS(t.Context(), c, "ns")
	b, err := fs.ReadFile(fsys, "_images/a.yaml")
	assert.NilError(t, err)
	assert.Equal(t, string(b), "arch: x86_64\n")
	for _, missing := range []string{"a", "b.yaml", "c.yaml"} {
		_, err := fs.ReadFile(fsys, missing)
		assert.ErrorIs(t, err, fs.ErrNotExist, missing)
	}
}

func TestSyncTemplateLibrary(t *testing.T) {
	bundle := fstest.MapFS{
		"default.yaml":      {Data: []byte("base: template:_images/x\n")},
		"_images/x.yaml":    {Data: []byte("arch: x86_64\n")},
		"_images/new.yaml":  {Data: []byte("arch: aarch64\n")},
		"experimental.yaml": {Data: []byte("vmType: qemu\n")},
	}
	edited := libraryConfigMap("_images/x.yaml", "arch: riscv64\n")
	gone := libraryConfigMap("removed.yaml", "cpus: 1\n")
	unmanaged := libraryEntry(TemplateLibraryNamespace, "mine", "mine.yaml", "cpus: 2\n")
	// An unmanaged ConfigMap at a stock path is taken over like an edit.
	taken := libraryEntry(TemplateLibraryNamespace, "taken", "_images/new.yaml", "cpus: 4\n")
	c := fake.NewClientBuilder().WithObjects(edited, gone, unmanaged, taken).Build()

	assert.NilError(t, syncTemplateLibrary(t.Context(), c, c, bundle))

	assert.NilError(t, c.Get(t.Context(), types.NamespacedName{Name: TemplateLibraryNamespace}, &corev1.Namespace{}))
	list := &corev1.ConfigMapList{}
	assert.NilError(t, c.List(t.Context(), list, client.InNamespace(TemplateLibraryNamespace)))
	got := map[string]string{}
	for _, cm := range list.Items {
		path := cm.Annotations[TemplatePathAnnotation]
		_, dup := got[path]
		assert.Assert(t, !dup, "two ConfigMaps hold %s", path)
		got[path] = cm.Data[v1alpha1.TemplateConfigMapKey]
	}
	want := map[string]string{"mine.yaml": "cpus: 2\n"}
	for path, f := range bundle {
		want[path] = string(f.Data)
	}
	assert.DeepEqual(t, got, want)
	assert.NilError(t, c.Get(t.Context(), client.ObjectKeyFromObject(taken), taken))
	assert.Equal(t, taken.Labels[managedByLabel], TemplateLibraryManagedByValue)

	// A second pass over the library that the first one wrote succeeds too.
	assert.NilError(t, syncTemplateLibrary(t.Context(), c, c, bundle))
}

func TestEmbedTemplate(t *testing.T) {
	c := fake.NewClientBuilder().WithObjects(
		libraryEntry(TemplateLibraryNamespace, "stock", "stock.yaml", "base: template:_default/cpus\nvmType: qemu\n"),
		libraryEntry(TemplateLibraryNamespace, "cpus", "_default/cpus.yaml", "cpus: 3\n"),
		libraryEntry("user", "common", "common.yaml", "memory: 3GiB\n"),
	).Build()
	d := &defaulter{Client: c, Reader: c}
	ref := types.NamespacedName{Namespace: "user", Name: "webapp"}

	t.Run("template name", func(t *testing.T) {
		out, err := d.embedTemplate(t.Context(), ref, "base: template:stock\narch: x86_64\n")
		assert.NilError(t, err)
		assert.Assert(t, !strings.Contains(out, "base:"), out)
		assert.Assert(t, strings.Contains(out, "vmType: qemu"), out)
		assert.Assert(t, strings.Contains(out, "cpus: 3"), out)
		assert.Assert(t, strings.Contains(out, "arch: x86_64"), out)
	})
	t.Run("relative path", func(t *testing.T) {
		out, err := d.embedTemplate(t.Context(), ref, "base: common.yaml\n")
		assert.NilError(t, err)
		assert.Assert(t, !strings.Contains(out, "base:"), out)
		assert.Assert(t, strings.Contains(out, "memory: 3GiB"), out)
	})
	t.Run("unknown template name", func(t *testing.T) {
		_, err := d.embedTemplate(t.Context(), ref, "base: template:nonexistent\n")
		assert.ErrorContains(t, err, "template `nonexistent.yaml` not found")
	})
	t.Run("no references", func(t *testing.T) {
		in := "# comment\nimages:\n- location: https://foo.test.\n"
		out, err := d.embedTemplate(t.Context(), ref, in)
		assert.NilError(t, err)
		assert.Equal(t, out, in)
	})
}

// TestEmbedStockTemplates checks that every bundled template resolves its whole
// base chain once the bundle is synced into the library.
func TestEmbedStockTemplates(t *testing.T) {
	bundle := limatemplates.FS()
	c := fake.NewClientBuilder().Build()
	assert.NilError(t, syncTemplateLibrary(t.Context(), c, c, bundle))
	d := &defaulter{Client: c, Reader: c}
	ref := types.NamespacedName{Namespace: "user", Name: "webapp"}

	paths, err := fs.Glob(bundle, "*.yaml")
	assert.NilError(t, err)
	nested, err := fs.Glob(bundle, "*/*.yaml")
	assert.NilError(t, err)
	for _, path := range append(paths, nested...) {
		name := strings.TrimSuffix(path, ".yaml")
		t.Run(name, func(t *testing.T) {
			_, err := d.embedTemplate(t.Context(), ref, "base: template:"+name+"\n")
			assert.NilError(t, err)
		})
	}
}

func TestLibraryConfigMapValidator(t *testing.T) {
	existing := libraryEntry("user", "a", "common.yaml", "cpus: 1\n")
	v := &LibraryConfigMapValidator{Reader: fake.NewClientBuilder().WithObjects(existing).Build()}

	withoutAnnotation := libraryEntry("user", "b", "", "")
	delete(withoutAnnotation.Annotations, TemplatePathAnnotation)
	withTemplateLabel := libraryEntry("user", "b", "b.yaml", "")
	withTemplateLabel.Labels[controllers.TemplateConfigMapLabel] = "true"

	cases := map[string]struct {
		configMap *corev1.ConfigMap
		wantErr   string
	}{
		"valid":                          {libraryEntry("user", "b", "b.yaml", "cpus: 2\n"), ""},
		"script is not parsed":           {libraryEntry("user", "b", "setup.sh", "#!/bin/sh\n{"), ""},
		"same ConfigMap keeps its path":  {libraryEntry("user", "a", "common.yaml", "cpus: 2\n"), ""},
		"same path in another namespace": {libraryEntry("other", "b", "common.yaml", "cpus: 2\n"), ""},
		"extensionless in user ns":       {libraryEntry("user", "b", "setup", "#!/bin/sh\n"), ""},
		"missing annotation":             {withoutAnnotation, "must have a"},
		"leading slash":                  {libraryEntry("user", "b", "/b.yaml", ""), "must be a relative"},
		"parent dir":                     {libraryEntry("user", "b", "x/../b.yaml", ""), "must be a relative"},
		"duplicate path":                 {libraryEntry("user", "b", "common.yaml", ""), `ConfigMap "a"`},
		"bad yaml":                       {libraryEntry("user", "b", "b.yml", "cpus: [\n"), "failed to parse"},
		"template label":                 {withTemplateLabel, "must not have the"},
		"non-canonical in library":       {libraryEntry(TemplateLibraryNamespace, "b", "ubuntu-24.04", ""), `"ubuntu-24.04.yaml"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := v.ValidateCreate(t.Context(), tc.configMap)
			if tc.wantErr == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
