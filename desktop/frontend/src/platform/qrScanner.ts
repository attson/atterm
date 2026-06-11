import { registerPlugin } from '@capacitor/core'

// AttermQRScanner is an inhouse Capacitor plugin (iOS-only). It replaces
// @capacitor-mlkit/barcode-scanning, which depends on Google MLKit and
// ships only through CocoaPods — incompatible with this project's
// Capacitor 8 SPM setup.
//
// The native side lives at
//   mobile/ios/App/App/Plugins/AttermQRScanner/AttermQRScannerPlugin.swift
// and is registered explicitly in MainViewController.capacitorDidLoad
// because Capacitor 8 does not autodiscover app-local plugins.
//
// Web / Wails builds get the default no-op proxy that registerPlugin
// returns, which throws "AttermQRScanner is not implemented on web" —
// callers catch PLUGIN_NOT_AVAILABLE / "not implemented" and degrade to
// the manual relay-URL entry.
export interface ScanResult {
  rawValue: string | null
  cancelled: boolean
}

export interface AttermQRScannerPlugin {
  requestPermissions(): Promise<{ camera: 'granted' | 'denied' }>
  scan(): Promise<ScanResult>
}

export const QRScanner = registerPlugin<AttermQRScannerPlugin>('AttermQRScanner')
