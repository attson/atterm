import Capacitor

// MainViewController is the app's root bridge controller. Capacitor 8 does not
// autodiscover app-local plugins (only distributed Swift packages), so any
// plugin defined inside the App target must be registered explicitly here.
// Main.storyboard points its sole scene at this class instead of the default
// CAPBridgeViewController.
class MainViewController: CAPBridgeViewController {
    override open func capacitorDidLoad() {
        bridge?.registerPluginInstance(AttermSecureStoragePlugin())
        bridge?.registerPluginInstance(AttermQRScannerPlugin())
        bridge?.registerPluginInstance(AttermServicePreviewPlugin())
    }
}
