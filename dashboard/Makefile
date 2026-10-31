# Builds the Cluster Explorer dashboard and stages it for rdd to embed.

# Rancher pairs this tag's shell with the steve version in rancher/rancher's
# go.mod at the same tag, so rdd/go.mod pins that version.
RANCHER_TAG := v2.15.3
RANCHER_SHA256 := 6180fd3aed247f4981e77874a2960bb7e9abeb149cef08f3feb5e82d9101253d

ARCHIVE := rancher-dashboard.tar.gz
# GitHub names the archive's top directory without the tag's leading "v".
ARCHIVE_ROOT := dashboard-$(RANCHER_TAG:v%=%)
OUTPUT := dist/embed
STAGED := ../rdd/pkg/steve/dashboard

# The tag's git tree keeps the components under pkg/rancher-components. Rancher's
# publish-shell.sh copies them into its npm package, so the recipe does the same.
all:
	curl --fail --location --silent --show-error --output $(ARCHIVE) \
		https://github.com/rancher/dashboard/archive/refs/tags/$(RANCHER_TAG).tar.gz
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
