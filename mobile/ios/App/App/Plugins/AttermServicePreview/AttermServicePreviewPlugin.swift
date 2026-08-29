import Foundation
import Capacitor
import CryptoKit
import Network

@objc(AttermServicePreviewPlugin)
public class AttermServicePreviewPlugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "AttermServicePreviewPlugin"
    public let jsName = "AttermServicePreview"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "start", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "stop", returnType: CAPPluginReturnPromise),
    ]

    private let lock = NSLock()
    private var previews: [UUID: NativeServicePreview] = [:]

    @objc func start(_ call: CAPPluginCall) {
        guard let serviceText = call.getString("serviceId"),
              let serviceID = UUID(uuidString: serviceText),
              let ticket = call.getString("clientTicket"), !ticket.isEmpty,
              let relayText = call.getString("relayUrl"),
              let token = call.getString("token"), !token.isEmpty,
              let txText = call.getString("clientToHostKey"),
              let rxText = call.getString("hostToClientKey"),
              let txKey = Data(base64Encoded: txText), txKey.count == 32,
              let rxKey = Data(base64Encoded: rxText), rxKey.count == 32 else {
            call.reject("INVALID_ARGUMENTS")
            return
        }
        // iOS offers no app-level trust hook for WKWebView, and teaching this
        // native NSURLSession to trust arbitrary self-signed certificates
        // would silently broaden the desktop's explicit test-only escape
        // hatch. Phase 1 supports normal wss:// and explicit cleartext ws://,
        // but not certificate-verification bypass.
        if call.getBool("allowInsecure") == true && relayText.lowercased().hasPrefix("wss://") {
            call.reject("INSECURE_RELAY_UNSUPPORTED")
            return
        }
        guard var components = URLComponents(string: relayText) else {
            call.reject("INVALID_RELAY_URL")
            return
        }
        let basePath = components.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        components.path = basePath.isEmpty ? "/service-client" : "/\(basePath)/service-client"
        guard let relayURL = components.url,
              relayURL.scheme == "ws" || relayURL.scheme == "wss" else {
            call.reject("INVALID_RELAY_URL")
            return
        }

        lock.lock()
        let duplicate = previews[serviceID] != nil
        lock.unlock()
        if duplicate {
            call.reject("SERVICE_ALREADY_RUNNING")
            return
        }

        let preview = NativeServicePreview(
            serviceID: serviceID,
            ticket: ticket,
            relayURL: relayURL,
            token: token,
            txKey: SymmetricKey(data: txKey),
            rxKey: SymmetricKey(data: rxKey)
        )
        lock.lock()
        previews[serviceID] = preview
        lock.unlock()
        preview.start { [weak self, weak preview] result in
            switch result {
            case .success(let url):
                call.resolve(["id": serviceID.uuidString.lowercased(), "url": url])
            case .failure(let error):
                self?.lock.lock()
                if self?.previews[serviceID] === preview {
                    self?.previews.removeValue(forKey: serviceID)
                }
                self?.lock.unlock()
                preview?.stop()
                call.reject("SERVICE_START_FAILED", error.localizedDescription)
            }
        }
    }

    @objc func stop(_ call: CAPPluginCall) {
        guard let text = call.getString("id"), let id = UUID(uuidString: text) else {
            call.reject("INVALID_ARGUMENTS")
            return
        }
        lock.lock()
        let preview = previews.removeValue(forKey: id)
        lock.unlock()
        preview?.stop()
        call.resolve()
    }

    deinit {
        lock.lock()
        let running = Array(previews.values)
        previews.removeAll()
        lock.unlock()
        running.forEach { $0.stop() }
    }
}

private final class NativeServicePreview {
    private static let headerSize = 24
    private static let maxPlaintext = 64 * 1024
    private static let maxPacket = headerSize + maxPlaintext + 16

    private let serviceID: UUID
    private let ticket: String
    private let relayURL: URL
    private let token: String
    private let txKey: SymmetricKey
    private let rxKey: SymmetricKey
    private let queue = DispatchQueue(label: "com.attson.atterm.service-preview")

    private var session: URLSession?
    private var webSocket: URLSessionWebSocketTask?
    private var listener: NWListener?
    private var connections: [UInt32: NWConnection] = [:]
    private var nextConnectionID: UInt32 = 0
    private var txSequence: UInt64 = 0
    private var rxSequence: UInt64 = 0
    private var outgoing: [Data] = []
    private var sending = false
    private var stopped = false
    private var registrationReady = false
    private var listenerPort: NWEndpoint.Port?
    private var startCompletion: ((Result<String, Error>) -> Void)?

    init(serviceID: UUID, ticket: String, relayURL: URL, token: String, txKey: SymmetricKey, rxKey: SymmetricKey) {
        self.serviceID = serviceID
        self.ticket = ticket
        self.relayURL = relayURL
        self.token = token
        self.txKey = txKey
        self.rxKey = rxKey
    }

    func start(completion: @escaping (Result<String, Error>) -> Void) {
        queue.async {
            guard !self.stopped else {
                completion(.failure(PreviewError.stopped))
                return
            }
            self.startCompletion = completion
            var request = URLRequest(url: self.relayURL)
            request.setValue("Bearer \(self.token)", forHTTPHeaderField: "Authorization")
            let session = URLSession(configuration: .default)
            let socket = session.webSocketTask(with: request)
            self.session = session
            self.webSocket = socket
            socket.resume()

            let registration = ["service_id": self.serviceID.uuidString.lowercased(), "ticket": self.ticket]
            guard let registrationData = try? JSONSerialization.data(withJSONObject: registration) else {
                self.failStart(PreviewError.invalidRegistration)
                return
            }
            socket.send(.data(registrationData)) { error in
                self.queue.async {
                    if let error = error {
                        self.failStart(error)
                        return
                    }
                    socket.receive { result in
                        self.queue.async {
                            guard case .success(let message) = result,
                                  self.registrationAckOK(message) else {
                                self.failStart(PreviewError.registrationRejected)
                                return
                            }
                            self.registrationReady = true
                            self.finishStartIfReady()
                            self.receiveNext()
                        }
                    }
                }
            }

            do {
                let parameters = NWParameters.tcp
                parameters.requiredLocalEndpoint = .hostPort(host: .ipv4(.loopback), port: .any)
                let listener = try NWListener(using: parameters, on: .any)
                self.listener = listener
                listener.newConnectionHandler = { connection in
                    self.queue.async { self.accept(connection) }
                }
                listener.stateUpdateHandler = { state in
                    self.queue.async {
                        switch state {
                        case .ready:
                            self.listenerPort = listener.port
                            self.finishStartIfReady()
                        case .failed(let error):
                            self.failStart(error)
                        default:
                            break
                        }
                    }
                }
                listener.start(queue: self.queue)
            } catch {
                self.failStart(error)
                return
            }
        }
    }

    private func registrationAckOK(_ message: URLSessionWebSocketTask.Message) -> Bool {
        let data: Data
        switch message {
        case .data(let value): data = value
        case .string(let value): data = Data(value.utf8)
        @unknown default: return false
        }
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return false }
        return object["ok"] as? Bool == true
    }

    func stop() {
        queue.async { self.stopLocked() }
    }

    private func finishStartIfReady() {
        guard registrationReady, let port = listenerPort, let completion = startCompletion else { return }
        startCompletion = nil
        completion(.success("http://127.0.0.1:\(port.rawValue)/"))
    }

    private func failStart(_ error: Error) {
        if let completion = startCompletion {
            startCompletion = nil
            completion(.failure(error))
        }
        stopLocked()
    }

    private func stopLocked() {
        guard !stopped else { return }
        stopped = true
        listener?.cancel()
        listener = nil
        webSocket?.cancel(with: .goingAway, reason: nil)
        webSocket = nil
        session?.invalidateAndCancel()
        session = nil
        for connection in connections.values { connection.cancel() }
        connections.removeAll()
        outgoing.removeAll()
        if let completion = startCompletion {
            startCompletion = nil
            completion(.failure(PreviewError.stopped))
        }
    }

    private func accept(_ connection: NWConnection) {
        guard !stopped, connections.count < 16, let id = allocateConnectionID() else {
            connection.cancel()
            return
        }
        connections[id] = connection
        connection.stateUpdateHandler = { state in
            if case .failed = state {
                self.queue.async { self.closeConnection(id, sendClose: true) }
            }
        }
        connection.start(queue: queue)
        enqueue(kind: 1, connectionID: id, plaintext: Data())
        receiveLocal(id: id, connection: connection)
    }

    private func allocateConnectionID() -> UInt32? {
        for _ in 0..<32 {
            nextConnectionID &+= 1
            if nextConnectionID == 0 { nextConnectionID = 1 }
            if connections[nextConnectionID] == nil { return nextConnectionID }
        }
        return nil
    }

    private func receiveLocal(id: UInt32, connection: NWConnection) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: Self.maxPlaintext) { data, _, complete, error in
            self.queue.async {
                guard self.connections[id] === connection, !self.stopped else { return }
                if let data = data, !data.isEmpty {
                    self.enqueue(kind: 2, connectionID: id, plaintext: data)
                }
                if complete || error != nil {
                    self.closeConnection(id, sendClose: true)
                } else {
                    self.receiveLocal(id: id, connection: connection)
                }
            }
        }
    }

    private func receiveNext() {
        guard !stopped, let socket = webSocket else { return }
        socket.receive { result in
            self.queue.async {
                guard !self.stopped else { return }
                switch result {
                case .failure:
                    self.stopLocked()
                case .success(let message):
                    guard case .data(let packet) = message,
                          let opened = try? self.open(packet: packet) else {
                        self.stopLocked()
                        return
                    }
                    self.handle(kind: opened.kind, connectionID: opened.connectionID, plaintext: opened.plaintext)
                    self.receiveNext()
                }
            }
        }
    }

    private func handle(kind: UInt8, connectionID: UInt32, plaintext: Data) {
        switch kind {
        case 2:
            guard let connection = connections[connectionID] else {
                stopLocked()
                return
            }
            connection.send(content: plaintext, completion: .contentProcessed { error in
                if error != nil {
                    self.queue.async { self.closeConnection(connectionID, sendClose: true) }
                }
            })
        case 3:
            guard plaintext.isEmpty else { stopLocked(); return }
            closeConnection(connectionID, sendClose: false)
        default:
            // Only the client creates connection ids. A host OPEN is a
            // protocol violation, not a second kind of socket request.
            stopLocked()
        }
    }

    private func closeConnection(_ id: UInt32, sendClose: Bool) {
        guard let connection = connections.removeValue(forKey: id) else { return }
        connection.cancel()
        if sendClose { enqueue(kind: 3, connectionID: id, plaintext: Data()) }
    }

    private func enqueue(kind: UInt8, connectionID: UInt32, plaintext: Data) {
        guard !stopped, let packet = try? seal(kind: kind, connectionID: connectionID, plaintext: plaintext) else {
            stopLocked()
            return
        }
        // Bound memory without dropping a packet. Closing the whole service
        // makes the truncated TCP stream visible to its caller.
        guard outgoing.count < 128 else { stopLocked(); return }
        outgoing.append(packet)
        drainOutgoing()
    }

    private func drainOutgoing() {
        guard !sending, !outgoing.isEmpty, let socket = webSocket else { return }
        sending = true
        socket.send(.data(outgoing[0])) { error in
            self.queue.async {
                self.sending = false
                guard error == nil, !self.stopped else { self.stopLocked(); return }
                self.outgoing.removeFirst()
                self.drainOutgoing()
            }
        }
    }

    private func seal(kind: UInt8, connectionID: UInt32, plaintext: Data) throws -> Data {
        guard (1...3).contains(kind), connectionID != 0, plaintext.count <= Self.maxPlaintext else {
            throw PreviewError.invalidPacket
        }
        guard txSequence < UInt64.max else { throw PreviewError.invalidPacket }
        txSequence += 1
        var header = makeHeader(kind: kind, connectionID: connectionID, sequence: txSequence, sealedLength: UInt32(plaintext.count + 16))
        let nonce = try AES.GCM.Nonce(data: nonceData(txSequence))
        let sealed = try AES.GCM.seal(plaintext, using: txKey, nonce: nonce, authenticating: aad(header))
        header.append(sealed.ciphertext)
        header.append(sealed.tag)
        return header
    }

    private func open(packet: Data) throws -> (kind: UInt8, connectionID: UInt32, plaintext: Data) {
        guard packet.count >= Self.headerSize + 16, packet.count <= Self.maxPacket else { throw PreviewError.invalidPacket }
        let bytes = [UInt8](packet.prefix(Self.headerSize))
        guard bytes[0...3].elementsEqual([65, 84, 83, 80]), bytes[4] == 1,
              (1...3).contains(bytes[5]), bytes[6] == 0, bytes[7] == 0 else { throw PreviewError.invalidPacket }
        let connectionID = readUInt32(bytes, 8)
        let sequence = readUInt64(bytes, 12)
        let sealedLength = Int(readUInt32(bytes, 20))
        guard connectionID != 0, sequence > rxSequence, sealedLength == packet.count - Self.headerSize else { throw PreviewError.invalidPacket }
        let nonce = try AES.GCM.Nonce(data: nonceData(sequence))
        let body = packet.dropFirst(Self.headerSize)
        let ciphertext = Data(body.dropLast(16))
        let tag = Data(body.suffix(16))
        let box = try AES.GCM.SealedBox(nonce: nonce, ciphertext: ciphertext, tag: tag)
        let plaintext = try AES.GCM.open(box, using: rxKey, authenticating: aad(Data(bytes)))
        rxSequence = sequence
        return (bytes[5], connectionID, plaintext)
    }

    private func makeHeader(kind: UInt8, connectionID: UInt32, sequence: UInt64, sealedLength: UInt32) -> Data {
        var data = Data([65, 84, 83, 80, 1, kind, 0, 0])
        appendBE(connectionID, to: &data)
        appendBE(sequence, to: &data)
        appendBE(sealedLength, to: &data)
        return data
    }

    private func aad(_ header: Data) -> Data {
        var tuple = serviceID.uuid
        var data = withUnsafeBytes(of: &tuple) { Data($0) }
        data.append(header.prefix(Self.headerSize))
        return data
    }

    private func nonceData(_ sequence: UInt64) -> Data {
        var data = Data(repeating: 0, count: 4)
        appendBE(sequence, to: &data)
        return data
    }

    private func appendBE<T: FixedWidthInteger>(_ value: T, to data: inout Data) {
        var big = value.bigEndian
        withUnsafeBytes(of: &big) { data.append(contentsOf: $0) }
    }

    private func readUInt32(_ bytes: [UInt8], _ offset: Int) -> UInt32 {
        bytes[offset..<offset + 4].reduce(0) { ($0 << 8) | UInt32($1) }
    }

    private func readUInt64(_ bytes: [UInt8], _ offset: Int) -> UInt64 {
        bytes[offset..<offset + 8].reduce(0) { ($0 << 8) | UInt64($1) }
    }
}

private enum PreviewError: LocalizedError {
    case invalidRegistration
    case registrationRejected
    case invalidPacket
    case stopped

    var errorDescription: String? {
        switch self {
        case .invalidRegistration: return "invalid service registration"
        case .registrationRejected: return "service registration rejected"
        case .invalidPacket: return "invalid service packet"
        case .stopped: return "service preview stopped"
        }
    }
}
