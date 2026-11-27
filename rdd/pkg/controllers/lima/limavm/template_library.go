// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing/fstest"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
)

const (
	// TemplateLibraryNamespace is the namespace where `template:` names resolve.
	// rdd writes Lima's stock templates into it.
	TemplateLibraryNamespace = "lima-templates"

	// TemplateLibraryLabel marks a ConfigMap as a library entry that a template can reference.
	TemplateLibraryLabel = "lima.rancherdesktop.io/template-library"

	// TemplatePathAnnotation holds a library ConfigMap's path, such as "_images/ubuntu-24.04.yaml".
	TemplatePathAnnotation = "lima.rancherdesktop.io/template-path"

	// TemplateLibraryManagedByValue is the app.kubernetes.io/managed-by value of the library
	// ConfigMaps that rdd writes from its bundled stock templates.
	TemplateLibraryManagedByValue = "rdd-lima-templates"

	// managedByLabel is the well-known label naming the tool that manages an object.
	managedByLabel = "app.kubernetes.io/managed-by"
)

// libraryConfigMapName derives a ConfigMap name from a library path. The readable
// part is the path without ".yaml", lowercased, with each '/' turned into '.' and
// any other character outside [a-z0-9.-] turned into '-'. A hash of the full path
// follows it, so two paths that read the same still get different names.
// "_images/ubuntu-24.04.yaml" becomes "images.ubuntu-24.04-<8 hex digits>".
func libraryConfigMapName(path string) string {
	sum := sha256.Sum256([]byte(path))
	hash := hex.EncodeToString(sum[:4])

	readable := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		case r >= 'A' && r <= 'Z':
			return r - 'A' + 'a'
		case r == '/':
			return '.'
		default:
			return '-'
		}
	}, strings.TrimSuffix(path, ".yaml"))
	// Each dot-separated label of a DNS-1123 subdomain starts and ends with an alphanumeric.
	var labels []string
	for label := range strings.SplitSeq(readable, ".") {
		if label = strings.Trim(label, "-"); label != "" {
			labels = append(labels, label)
		}
	}
	readable = strings.Join(labels, ".")

	// A DNS-1123 subdomain has at most 253 characters, and the hash takes 9 of them.
	const maxReadable = 253 - 1 - len("01234567")
	if len(readable) > maxReadable {
		readable = strings.TrimRight(readable[:maxReadable], "-.")
	}
	if readable == "" {
		return hash
	}
	return readable + "-" + hash
}

// configMapFS is an fs.FS of the library ConfigMaps in one namespace. Each
// ConfigMap's template data is a file at the path in its TemplatePathAnnotation.
// It lists the namespace once, on first use.
type configMapFS struct {
	ctx       context.Context
	reader    client.Reader
	namespace string

	once  sync.Once
	files fstest.MapFS
	err   error
}

var _ fs.FS = &configMapFS{}

// newConfigMapFS returns an fs.FS of the library ConfigMaps in namespace, read through reader.
func newConfigMapFS(ctx context.Context, reader client.Reader, namespace string) *configMapFS {
	return &configMapFS{ctx: ctx, reader: reader, namespace: namespace}
}

// Open implements fs.FS.
func (f *configMapFS) Open(name string) (fs.File, error) {
	f.once.Do(f.load)
	if f.err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
	}
	return f.files.Open(name)
}

func (f *configMapFS) load() {
	list := &corev1.ConfigMapList{}
	if err := f.reader.List(f.ctx, list, client.InNamespace(f.namespace), client.MatchingLabels{TemplateLibraryLabel: "true"}); err != nil {
		f.err = fmt.Errorf("failed to list template library ConfigMaps in namespace %q: %w", f.namespace, err)
		return
	}
	f.files = fstest.MapFS{}
	for i := range list.Items {
		configMap := &list.Items[i]
		path := configMap.Annotations[TemplatePathAnnotation]
		// The library validator rejects both cases, but a ConfigMap may predate it.
		if !fs.ValidPath(path) || path == "." {
			continue
		}
		if _, ok := f.files[path]; ok {
			continue
		}
		f.files[path] = &fstest.MapFile{Data: []byte(configMap.Data[v1alpha1.TemplateConfigMapKey]), Mode: 0o444}
	}
}
