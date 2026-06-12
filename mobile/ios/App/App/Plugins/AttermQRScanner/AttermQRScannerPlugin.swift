import Foundation
import Capacitor
import AVFoundation
import UIKit

/// Capacitor plugin backing platform/qrScanner.ts on iOS. Presents a
/// fullscreen camera view and resolves the first QR code's rawValue.
///
/// Replaces @capacitor-mlkit/barcode-scanning, which depends on Google
/// MLKit and ships only via CocoaPods — incompatible with this app's
/// Capacitor 8 SPM setup. AVCaptureMetadataOutput natively supports
/// .qr since iOS 7, which covers our single use case (relay pairing).
///
/// Registered explicitly in MainViewController.capacitorDidLoad and
/// in the App target via project.pbxproj — Capacitor 8 does not
/// autodiscover app-local plugins.
@objc(AttermQRScannerPlugin)
public class AttermQRScannerPlugin: CAPPlugin, CAPBridgedPlugin {

    public let identifier = "AttermQRScannerPlugin"
    public let jsName = "AttermQRScanner"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "requestPermissions", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "scan", returnType: CAPPluginReturnPromise),
    ]

    private var scannerVC: AttermQRScannerViewController?

    /// requestPermissions() -> { camera: 'granted' | 'denied' }
    /// Overrides CAPPlugin.requestPermissions so Capacitor's permission
    /// proxy routes here instead of returning the base no-op stub.
    @objc public override func requestPermissions(_ call: CAPPluginCall) {
        let current = AVCaptureDevice.authorizationStatus(for: .video)
        switch current {
        case .authorized:
            call.resolve(["camera": "granted"])
        case .denied, .restricted:
            call.resolve(["camera": "denied"])
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { granted in
                DispatchQueue.main.async {
                    call.resolve(["camera": granted ? "granted" : "denied"])
                }
            }
        @unknown default:
            call.resolve(["camera": "denied"])
        }
    }

    /// scan() -> { rawValue: string | null, cancelled: bool }
    /// rawValue is null when the user cancels OR when no QR is detected
    /// before dismissal; cancelled disambiguates so the caller can skip
    /// the "no QR detected" error.
    @objc func scan(_ call: CAPPluginCall) {
        guard AVCaptureDevice.authorizationStatus(for: .video) == .authorized else {
            call.reject("CAMERA_DENIED")
            return
        }

        DispatchQueue.main.async { [weak self] in
            guard let self = self,
                  let presenter = self.bridge?.viewController else {
                call.reject("NO_PRESENTER")
                return
            }
            let vc = AttermQRScannerViewController()
            vc.modalPresentationStyle = .fullScreen
            vc.onResult = { [weak self] outcome in
                guard let self = self else { return }
                vc.dismiss(animated: true) {
                    self.scannerVC = nil
                    switch outcome {
                    case .scanned(let raw):
                        call.resolve(["rawValue": raw, "cancelled": false])
                    case .cancelled:
                        call.resolve(["rawValue": NSNull(), "cancelled": true])
                    case .error(let code):
                        call.reject(code)
                    }
                }
            }
            self.scannerVC = vc
            presenter.present(vc, animated: true)
        }
    }
}

/// Internal view controller. Owns the AVCaptureSession; presents a
/// minimal UI (✕ cancel + framed reticle) on top of the live preview.
class AttermQRScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {

    enum Outcome {
        case scanned(String)
        case cancelled
        case error(String)
    }

    var onResult: ((Outcome) -> Void)?

    private let session = AVCaptureSession()
    private var previewLayer: AVCaptureVideoPreviewLayer?
    private var didReportResult = false
    private let reticle = UIView()

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black

        guard let device = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else {
            reportOnce(.error("CAMERA_UNAVAILABLE"))
            return
        }
        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else {
            reportOnce(.error("CAMERA_OUTPUT_UNAVAILABLE"))
            return
        }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        preview.frame = view.bounds
        view.layer.addSublayer(preview)
        previewLayer = preview

        let cancelBtn = UIButton(type: .system)
        cancelBtn.setTitle("✕", for: .normal)
        cancelBtn.setTitleColor(.white, for: .normal)
        cancelBtn.titleLabel?.font = .systemFont(ofSize: 28, weight: .regular)
        cancelBtn.translatesAutoresizingMaskIntoConstraints = false
        cancelBtn.addTarget(self, action: #selector(onCancelTapped), for: .touchUpInside)
        view.addSubview(cancelBtn)
        NSLayoutConstraint.activate([
            cancelBtn.leadingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.leadingAnchor, constant: 16),
            cancelBtn.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 8),
            cancelBtn.widthAnchor.constraint(equalToConstant: 44),
            cancelBtn.heightAnchor.constraint(equalToConstant: 44),
        ])

        reticle.layer.borderColor = UIColor.white.cgColor
        reticle.layer.borderWidth = 2
        reticle.layer.cornerRadius = 8
        reticle.backgroundColor = .clear
        view.addSubview(reticle)
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer?.frame = view.bounds
        let size: CGFloat = min(view.bounds.width, view.bounds.height) * 0.65
        reticle.frame = CGRect(
            x: (view.bounds.width - size) / 2,
            y: (view.bounds.height - size) / 2,
            width: size,
            height: size
        )
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        if !session.isRunning {
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                self?.session.startRunning()
            }
        }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        if session.isRunning {
            session.stopRunning()
        }
    }

    @objc private func onCancelTapped() {
        reportOnce(.cancelled)
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput,
                        didOutput metadataObjects: [AVMetadataObject],
                        from connection: AVCaptureConnection) {
        guard !didReportResult,
              let first = metadataObjects.compactMap({ $0 as? AVMetadataMachineReadableCodeObject }).first,
              let raw = first.stringValue else {
            return
        }
        reportOnce(.scanned(raw))
    }

    private func reportOnce(_ outcome: Outcome) {
        if didReportResult { return }
        didReportResult = true
        onResult?(outcome)
    }
}
