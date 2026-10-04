# Native plugin examples

From the base stash source directory:
```
go build -tags=plugin_example -o plugin_goraw.exe ./pkg/plugin/examples/goraw/...
go build -tags=plugin_example -o plugin_gorpc.exe ./pkg/plugin/examples/gorpc/...
```

Place the resulting binaries together with the yml files in the `plugins` subdirectory of your stash directory.

Every example declares `apiVersion: 3`. Raw Python, Go RPC and server-side
JavaScript execution remain supported; they are independent of browser UI code.

For a browser module, copy the `browser` directory into the configured plugins
directory, reload plugins, then reload the UI. It registers an example page and
navigation entry through the shared host. No separate React bundle is required.
See the [browser host guide](../../../../ui/v3/docs/plugin-host.md).
The old DOM-patching React example is preserved at `v2.5-compatible-final`.
