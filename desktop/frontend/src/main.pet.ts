import { createApp } from "vue";
import PetApp from "./pet/PetApp.vue";

/**
 * Entry point for the companion window ("桌面挂件" / Desk Widget) — the `--pet` process.
 *
 * Deliberately does NOT go through bootstrapApp(): the pet has no platform
 * adapter, no session store, no i18n bundle, no router and no relay. Pulling
 * that graph in would drag the whole main-app bundle into a 252px window.
 */

// The OS window is transparent (see desktop/pet_window.go); the card inside
// PetApp is the only thing that paints. Any background here would show up as
// an opaque rectangle floating over the user's desktop.
const style = document.createElement("style");
style.textContent = `
  html, body {
    margin: 0;
    padding: 0;
    background: transparent;
    overflow: hidden;
  }
  #pet { background: transparent; }
`;
document.head.appendChild(style);

createApp(PetApp).mount("#pet");
