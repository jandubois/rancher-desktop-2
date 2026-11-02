# Builds the Cluster Explorer dashboard and stages it for rdd to embed.

# Rancher pairs this tag's shell with the steve version in rancher/rancher's
# go.mod at the same tag, so rdd/go.mod pins that version.
RANCHER_TAG := v2.15.3
# This commit on rancher-sandbox/dashboard's i18n-fixes-2.15 branch is
# RANCHER_TAG plus our shell fixes. We pin the commit because a branch archive
# does not have a stable checksum. yarn.lock holds a hash of this shell, so
# `yarn install --immutable` fails on a new commit. After `make` stops there,
# run a plain `yarn install` and commit the updated lock with the new pin.
SHELL_COMMIT := d79889ccfab793c708c56ea973b38864396bdbcf
RANCHER_SHA256 := 625f4e637d18ca912690774a722b8820e5638dec0125283f2208d3230db52044

ARCHIVE := rancher-dashboard.tar.gz
ARCHIVE_ROOT := dashboard-$(SHELL_COMMIT)
OUTPUT := dist/embed
STAGED := ../rdd/pkg/steve/dashboard

# The git tree keeps the components under pkg/rancher-components. Rancher's
# publish-shell.sh copies them into its npm package, so the recipe does the same.
all:
	curl --fail --location --silent --show-error --output $(ARCHIVE) \
		https://github.com/rancher-sandbox/dashboard/archive/$(SHELL_COMMIT).tar.gz
	echo "$(RANCHER_SHA256)  $(ARCHIVE)" | shasum --algorithm 256 --check
	rm -rf vendor-shell
	mkdir -p vendor-shell
	tar --extract --gzip --file=$(ARCHIVE) --directory=vendor-shell --strip-components=2 \
		$(ARCHIVE_ROOT)/shell
	rm -rf vendor-shell/rancher-components
	mkdir -p vendor-shell/rancher-components
	tar --extract --gzip --file=$(ARCHIVE) --directory=vendor-shell/rancher-components \
		--strip-components=5 $(ARCHIVE_ROOT)/pkg/rancher-components/src/components
	yarn install --immutable
	rm -rf dist
	RANCHER_ENV=desktop API=http://127.0.0.1:6120 RESOURCE_BASE=http://127.0.0.1:6120 \
		OUTPUT_DIR=$(OUTPUT) COMMIT=$(RANCHER_TAG) VERSION=$(RANCHER_TAG) yarn build
	find $(OUTPUT) -name '*.map' -delete
	rm -rf $(STAGED)
	cp -R $(OUTPUT) $(STAGED)
.PHONY: all
