#import <Foundation/Foundation.h>
#import <Capacitor/Capacitor.h>

// CAP_PLUGIN registers the plugin class with Capacitor's runtime so the
// JS side can find it via Capacitor.registerPlugin<...>('AttermSecureStorage').
// All three methods are declared with CAP_PLUGIN_METHOD so the bridge
// knows their signatures.
CAP_PLUGIN(AttermSecureStoragePlugin, "AttermSecureStorage",
    CAP_PLUGIN_METHOD(set, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(get, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(remove, CAPPluginReturnPromise);
)
