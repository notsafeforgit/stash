# Optional local cross-compiler

The supported publisher compiles Linux amd64 directly with the Go version in
`go.mod`. It does not consume a prebuilt compiler image. Other cross-compilation
targets are local development helpers and are not release-tested platforms.

To use the retained toolchain, build it from this checkout:

```bash
make -C docker/compiler build
make pre-ui generate ui
make start-compiler-container
docker exec -t build make build-cc-all
make remove-compiler-container
```

The default image is `stash-compiler:local`. Set `IMAGE` when building and
`COMPILER_IMAGE` when starting the container to use another local tag. The
container runs as root and may create root-owned files in the checkout.

There is no compiler-image publication workflow. Production application images
come from the [develop publisher](../../.github/workflows/ghcr-publish.yml), after
the shared validation gate and runtime smoke tests.
