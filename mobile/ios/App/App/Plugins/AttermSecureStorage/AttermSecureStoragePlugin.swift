import Foundation
import Capacitor

/// Capacitor plugin backing platform/secureStorage.ts on iOS. Stores
/// short string values in the iOS Keychain. Single-app keychain; no
/// access groups, no biometric prompt. AccessibleAfterFirstUnlock so
/// background reconnect works after the phone is unlocked at least once
/// per boot.
@objc(AttermSecureStoragePlugin)
public class AttermSecureStoragePlugin: CAPPlugin {

    private let service = "com.attson.atterm"

    /// set({ key, value }) — upsert.
    @objc func set(_ call: CAPPluginCall) {
        guard let key = call.getString("key"),
              let value = call.getString("value"),
              let data = value.data(using: .utf8) else {
            call.reject("MISSING_ARGS")
            return
        }

        let baseQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]

        // Try add first; on duplicate, update.
        var attrs = baseQuery
        attrs[kSecValueData as String] = data
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock

        let addStatus = SecItemAdd(attrs as CFDictionary, nil)
        if addStatus == errSecSuccess {
            call.resolve()
            return
        }
        if addStatus == errSecDuplicateItem {
            let update: [String: Any] = [
                kSecValueData as String: data,
                kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
            ]
            let updStatus = SecItemUpdate(baseQuery as CFDictionary, update as CFDictionary)
            if updStatus == errSecSuccess {
                call.resolve()
                return
            }
            call.reject("KEYCHAIN_ERROR", "SecItemUpdate failed", nil, ["status": Int(updStatus)])
            return
        }
        call.reject("KEYCHAIN_ERROR", "SecItemAdd failed", nil, ["status": Int(addStatus)])
    }

    /// get({ key }) -> { value: string | null }
    @objc func get(_ call: CAPPluginCall) {
        guard let key = call.getString("key") else {
            call.reject("MISSING_ARGS")
            return
        }

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecMatchLimit as String: kSecMatchLimitOne,
            kSecReturnData as String: true,
        ]

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)

        if status == errSecItemNotFound {
            call.resolve(["value": NSNull()])
            return
        }
        if status != errSecSuccess {
            call.reject("KEYCHAIN_ERROR", "SecItemCopyMatching failed", nil, ["status": Int(status)])
            return
        }
        guard let data = item as? Data,
              let str = String(data: data, encoding: .utf8) else {
            call.reject("KEYCHAIN_ERROR", "stored value is not utf-8 string", nil, nil)
            return
        }
        call.resolve(["value": str])
    }

    /// remove({ key }) — idempotent.
    @objc func remove(_ call: CAPPluginCall) {
        guard let key = call.getString("key") else {
            call.reject("MISSING_ARGS")
            return
        }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        let status = SecItemDelete(query as CFDictionary)
        if status == errSecSuccess || status == errSecItemNotFound {
            call.resolve()
            return
        }
        call.reject("KEYCHAIN_ERROR", "SecItemDelete failed", nil, ["status": Int(status)])
    }
}
