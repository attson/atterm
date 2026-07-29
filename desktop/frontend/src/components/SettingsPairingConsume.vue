<script lang="ts">
function b64UrlNoPadToBytes(s: string): Uint8Array {
  const norm = s.replace(/-/g, "+").replace(/_/g, "/");
  const pad = norm.length % 4 === 0 ? "" : "=".repeat(4 - (norm.length % 4));
  const bin = atob(norm + pad);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function parseScanned(raw: string, allowInsecure: boolean): { origin: string; token: string; wrapKey?: Uint8Array } | string {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return "pair_invalid_url";
  }
  if (u.protocol !== "https:" && !(u.protocol === "http:" && allowInsecure)) {
    return "pair_invalid_scheme";
  }
  const token = u.searchParams.get("t");
  if (!token || !u.host) return "pair_invalid_url";
  const k = u.searchParams.get("k");
  let wrapKey: Uint8Array | undefined;
  if (k) {
    let bytes: Uint8Array;
    try {
      bytes = b64UrlNoPadToBytes(k);
    } catch {
      return "pair_invalid_url";
    }
    if (bytes.length !== 32) return "pair_invalid_url";
    wrapKey = bytes;
  }
  return { origin: u.origin, token, wrapKey };
}
</script>

<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { usePlatform } from "../platform";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{ scannedUrl: string; allowInsecure?: boolean }>();
const emit = defineEmits<{ (e: "connected"): void; (e: "cancel"): void }>();

const platform = usePlatform();
const { t } = useI18n();

const status = ref<"pending" | "error">("pending");
const errorCode = ref("");
const elapsedMs = ref(0);
const step = ref<"parsing" | "requesting" | "saving">("parsing");
let rafHandle = 0;
let started = 0;
let cancelled = false;

const errorMessage = computed(() => {
  const code = errorCode.value;
  switch (code) {
    case "pair_invalid_url": return t("mobile.pairing.errInvalidUrl");
    case "pair_invalid_scheme": return t("mobile.pairing.errInvalidScheme");
    case "platform_unsupported": return t("mobile.pairing.errPlatformUnsupported");
    case "cannot_reach_relay": return t("mobile.pairing.errCannotReachRelay");
    case "pair_timeout": return t("mobile.pairing.errTimeout");
    case "pair_invalid": return t("mobile.pairing.errPairInvalid");
    case "cancelled": return t("mobile.pairing.errCancelled");
    default: return t("mobile.pairing.errGeneric", { message: code });
  }
});

const elapsedLabel = computed(() => (elapsedMs.value / 1000).toFixed(1) + "s");
const stepLabel = computed(() => {
  switch (step.value) {
    case "parsing": return t("mobile.pairing.stepParsing");
    case "requesting": return t("mobile.pairing.stepRequesting");
    case "saving": return t("mobile.pairing.stepSaving");
    default: return "";
  }
});

function tickElapsed(): void {
  if (cancelled || status.value !== "pending") return;
  elapsedMs.value = Date.now() - started;
  rafHandle = requestAnimationFrame(tickElapsed);
}

function onCancelClicked(): void {
  cancelled = true;
  errorCode.value = "cancelled";
  status.value = "error";
  if (rafHandle) {
    cancelAnimationFrame(rafHandle);
    rafHandle = 0;
  }
}

async function run() {
  const parsed = parseScanned(props.scannedUrl, !!props.allowInsecure);
  if (typeof parsed === "string") {
    errorCode.value = parsed;
    status.value = "error";
    return;
  }

  started = Date.now();
  elapsedMs.value = 0;
  rafHandle = requestAnimationFrame(tickElapsed);

  try {
    if (!platform.relay.consumePairing) throw new Error("platform_unsupported");
    step.value = "requesting";
    const result = await Promise.race([
      platform.relay.consumePairing(parsed.origin, parsed.token, parsed.wrapKey),
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error("pair_timeout")), 16_000),
      ),
    ]);
    if (cancelled) return;
    step.value = "saving";
    await platform.relay.save({
      url: result.relay_url,
      token: result.session_token,
      session_expires_at: result.expires_at,
      allow_insecure_relay: !!props.allowInsecure,
      remote_permission: "full",
      last_email: result.user?.email ?? "",
      connected: false,
      realmId: result.realm_id,
      homeInstanceURL: result.home_instance_url,
    });
    if (cancelled) return;
    emit("connected");
  } catch (e) {
    if (cancelled) return;
    errorCode.value = e instanceof Error ? e.message : String(e);
    status.value = "error";
  } finally {
    if (rafHandle) {
      cancelAnimationFrame(rafHandle);
      rafHandle = 0;
    }
  }
}

onMounted(run);
onBeforeUnmount(() => {
  cancelled = true;
  if (rafHandle) {
    cancelAnimationFrame(rafHandle);
    rafHandle = 0;
  }
});
</script>

<template>
  <div class="pair-consume">
    <div v-if="status === 'pending'" class="pending">
      <p>{{ t("mobile.pairing.connecting") }}</p>
      <p class="step" data-testid="pair-step">{{ stepLabel }}</p>
      <p class="elapsed" data-testid="pair-elapsed">{{ elapsedLabel }}</p>
      <button type="button" class="cancel" data-testid="pair-cancel" @click="onCancelClicked">
        {{ t("common.cancel") }}
      </button>
    </div>
    <div v-else class="pair-error" data-testid="pair-error">
      <p>{{ t("mobile.pairing.failed") }}</p>
      <p class="code">{{ errorMessage }}</p>
      <button type="button" @click="emit('cancel')">{{ t("mobile.pairing.back") }}</button>
    </div>
  </div>
</template>

<style scoped>
.pair-consume {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.03);
}
.pending,
.pair-error {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--fg-dim);
  font-size: 13px;
}
.pending p,
.pair-error p {
  margin: 0;
}
.step,
.elapsed,
.code {
  font-family: var(--font-mono);
  font-size: 12px;
}
.code {
  color: var(--bad);
}
.cancel {
  align-self: flex-start;
}
.pair-error button {
  align-self: flex-start;
}
</style>
