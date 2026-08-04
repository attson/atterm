import { bindings } from "./_bindings";
import type { UpdateState, VersionLine } from "./_bindings";

export type { UpdateState, VersionLine } from "./_bindings";

export function getUpdateState(): Promise<UpdateState> {
  return bindings().GetUpdateState();
}

export function checkUpdate(): Promise<void> {
  return bindings().CheckUpdate();
}

export function startDownload(): Promise<void> {
  return bindings().StartDownload();
}

export function downloadVersion(tag: string): Promise<void> {
  return bindings().DownloadVersion(tag);
}

export function cancelDownload(): Promise<void> {
  return bindings().CancelDownload();
}

export function forceRedownload(tag: string): Promise<void> {
  return bindings().ForceRedownload(tag);
}

export function installUpdate(): Promise<void> {
  return bindings().InstallUpdate();
}

export function getAutoCheckUpdates(): Promise<boolean> {
  return bindings().GetAutoCheckUpdates();
}

export function setAutoCheckUpdates(enabled: boolean): Promise<void> {
  return bindings().SetAutoCheckUpdates(enabled);
}

export function getUpdateGHProxyURL(): Promise<string> {
  return bindings().GetUpdateGHProxyURL();
}

export function setUpdateGHProxyURL(proxyURL: string): Promise<void> {
  return bindings().SetUpdateGHProxyURL(proxyURL);
}
