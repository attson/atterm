---
description: Use the AT Term Relay Admin console to create invitations, manage users and roles, change runtime configuration, configure Feishu, inspect traffic, and check health.
---

# Admin console

After the first administrator registers with the bootstrap claim token, the signed-in interface shows **Admin**. It shares the normal client login session, but only the admin role can read or change management data.

## Invitations

Create one-time invitation codes under **Invitations** and store them immediately. The full token appears only in the creation response; later lists retain status and metadata. Send it directly to the intended user, never in a public issue, log, or screenshot.

## Users and roles

**Users** can filter accounts, disable or restore a user, and grant or revoke admin. The last administrator cannot be demoted; promote another account first.

A password reset gives the user a new `account_key`, so previously sealed E2EE content cannot be recovered. Confirm this outcome with the user before resetting. AT Term deliberately has no recovery link that bypasses this boundary.

## Config

**Config** manages:

- The exact browser Origin allowlist. An iOS Capacitor client needs `capacitor://localhost`.
- Request rate and active WebSocket connection limits per IP + token.
- DEBUG scopes and an automatic stop time. The Payload scope can expose terminal input/output, must be enabled separately, and should be disabled as soon as diagnosis ends.

Origin and debug changes apply live. VAPID subject and environment-defined process-local rate/connection limits require restarting that instance. With a shared database, instances poll DB-backed configuration; process-local settings remain per instance.

## Feishu

**Feishu** enables the Relay-managed bridge, sets the Open Platform base URL, and generates or accepts a base64 32-byte master key. GET responses show only the final four characters; the full key is never returned or logged.

Rotating the key makes existing Feishu bindings unreadable, so replacement is rejected unless force rotation is explicit. Enable, disable, and ordinary configuration changes apply live.

## Traffic and health

**Traffic** shows per-user connections and traffic composition for diagnosis without exposing E2EE terminal plaintext.

Use these operational probes:

- `/healthz` for basic liveness.
- `/admin/health` for the administrator health page.
- `/admin/api/health` for authenticated structured health data.

After deployment, verify that a normal user cannot see Admin, an administrator can read configuration, an invalid Origin is rejected, and valid Web/desktop clients can still log in and attach. See [Deploy Relay](/en/guide/deploy-relay) and [End-to-end encryption](/en/guide/e2ee).
