# Optional local cross-compiler. Production uses the develop GHCR workflow.
IMAGE ?= stash-compiler:local

.PHONY: build build-no-cache
build:
	docker build -t "$(IMAGE)" .

build-no-cache:
	docker build --no-cache -t "$(IMAGE)" .
