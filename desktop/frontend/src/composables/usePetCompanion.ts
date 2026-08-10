import { watch, type Ref } from "vue";
import { usePlatform } from "../platform";
import { usePluginConfigStore } from "../plugins/configStore";
import { projectPetState, type PetSessionSource } from "../lib/petState";
import { errText, logWarn } from "../lib/log";
import { getUserHomeDir } from "../lib/api";

/**
 * usePetCompanion drives the companion window ("桌面挂件" / Desk Widget): it starts and stops
 * the child process from the plugin's enabled flag, and pushes a projected
 * snapshot whenever the merged session list changes.
 *
 * The pet is a "companion-window" plugin — PluginHost mounts nothing for it
 * (there is no component in this window's tree), so this composable is the
 * whole host. See
 * docs/superpowers/specs/2026-08-10-ai-pet-companion-window-design.md.
 *
 * Pushing from here rather than letting the pet connect to the relay itself is
 * what keeps the relay token and account_key inside this process (red line
 * #21): by the time the list reaches here it is already merged across local +
 * remote streams and already unsealed.
 */
export function usePetCompanion(opts: {
  /** Merged session list — the same one the sidebar renders. */
  sessions: Ref<readonly PetSessionSource[]>;
  /** This machine's host id, so remote rows can be labelled. */
  localHostId: Ref<string>;
}): void {
  const platform = usePlatform();
  const store = usePluginConfigStore();

  // Web and Capacitor have no second-OS-window concept, so they leave the
  // bridge undefined. Bail out rather than guarding at every call site.
  if (!platform.pet) return;
  const pet = platform.pet;

  let running = false;
  // Cached once: the home directory only changes across logins, and the
  // projection needs it to render cwds as `~/…` the way the sidebar does.
  let home = "";

  async function reconcile(enabled: boolean) {
    if (enabled === running) return;
    try {
      if (enabled) {
        if (!home) {
          // Non-fatal: without it paths render absolute instead of `~/…`.
          home = await getUserHomeDir().catch(() => "");
        }
        await pet.start();
        running = true;
        // Push immediately so the window has content the moment it appears
        // instead of showing "连接中…" until the next session-list change.
        await push();
      } else {
        await pet.stop();
        running = false;
      }
    } catch (err) {
      logWarn("pet", "lifecycle change failed", { enabled, error: errText(err) });
      if (enabled) {
        // Mirror PluginHost's load-failure policy: turn the plugin back off so
        // the user is not stuck retrying a broken spawn on every reconcile.
        running = false;
        try {
          await store.setEnabled("pet", false);
        } catch (e) {
          logWarn("pet", "disable-after-start-failure also failed", {
            error: errText(e),
          });
        }
      }
    }
  }

  async function push() {
    if (!running) return;
    const state = projectPetState(opts.sessions.value, {
      localHostId: opts.localHostId.value,
      home,
      aiOnly: store.cfg?.pet?.aiOnly ?? false,
    });
    try {
      await pet.pushState(JSON.stringify(state));
    } catch (err) {
      // A dead pipe means the pet process is gone; stop pushing until the
      // user re-enables it. Go's reap() has already cleared its own state.
      running = false;
      logWarn("pet", "state push failed", { error: errText(err) });
    }
  }

  watch(
    () => store.isPluginEnabled("pet"),
    (enabled) => {
      void reconcile(enabled);
    },
    { immediate: true },
  );

  watch(opts.sessions, () => void push(), { deep: true });

  // Re-push when the AI-only filter flips, so the widget updates on the
  // setting change itself rather than waiting for the next session event.
  watch(
    () => store.cfg?.pet?.aiOnly,
    () => void push(),
  );
}
