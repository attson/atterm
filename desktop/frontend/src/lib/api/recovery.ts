import { bindings } from "./_bindings";
import type {
  RecoveryAIInfo,
  RecoveryPaneSnapshot,
  RecoverySnapshot,
  RecoveryTabSnapshot,
} from "./_bindings";

export type {
  RecoveryAIInfo,
  RecoveryPaneSnapshot,
  RecoverySnapshot,
  RecoveryTabSnapshot,
} from "./_bindings";

export function loadRecoverySnapshot(): Promise<RecoverySnapshot> {
  return bindings().LoadRecoverySnapshot();
}

export function saveRecoverySnapshot(snap: RecoverySnapshot): Promise<void> {
  return bindings().SaveRecoverySnapshot(JSON.stringify(snap));
}

export function discardRecoverySnapshot(): Promise<void> {
  return bindings().DiscardRecoverySnapshot();
}

export function getRecoveryDialogEnabled(): Promise<boolean> {
  return bindings().GetRecoveryDialogEnabled();
}

export function setRecoveryDialogEnabled(enabled: boolean): Promise<void> {
  return bindings().SetRecoveryDialogEnabled(enabled);
}
