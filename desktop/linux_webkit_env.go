package main

const (
	linuxWebKitDisableDMABufRenderer = "WEBKIT_DISABLE_DMABUF_RENDERER"
	linuxWebKitDMABufDisableGBM      = "WEBKIT_DMABUF_RENDERER_DISABLE_GBM"
)

var linuxWebKitDMABufEnvKeys = []string{
	linuxWebKitDisableDMABufRenderer,
	linuxWebKitDMABufDisableGBM,
}

type envLookupFunc func(string) (string, bool)
type envSetFunc func(string, string) error

func applyLinuxWebKitEnvironment(lookup envLookupFunc, set envSetFunc) {
	// Some WebKitGTK GPU stacks abort before Wails can surface an error; keep
	// user-provided renderer overrides, otherwise default to the safer path.
	for _, key := range linuxWebKitDMABufEnvKeys {
		if _, ok := lookup(key); ok {
			return
		}
	}
	for _, key := range linuxWebKitDMABufEnvKeys {
		_ = set(key, "1")
	}
}
