import * as vscode from "vscode";

export type ProxyState = "downloading" | "starting" | "running" | "stopped" | "error";

export class ProxyStatusBar {
  private readonly item: vscode.StatusBarItem;

  constructor() {
    this.item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    this.item.show();
    this.setStopped();
  }

  setDownloading(): void {
    this.set("downloading", "$(cloud-download) Claude Proxy: Downloading…", undefined, "claudeProxy.showLogs");
  }

  setStarting(): void {
    this.set("starting", "$(sync~spin) Claude Proxy: Starting…", undefined, "claudeProxy.showLogs");
  }

  setRunning(port: number): void {
    this.set("running", `$(check) Claude Proxy :${port}`, `Running on port ${port} — click to open dashboard`, "claudeProxy.openDashboard");
  }

  setStopped(): void {
    this.set("stopped", "$(circle-slash) Claude Proxy: Stopped", "Click to start", "claudeProxy.start");
  }

  setError(message: string): void {
    this.set("error", "$(warning) Claude Proxy: Error", message, "claudeProxy.showLogs");
  }

  dispose(): void {
    this.item.dispose();
  }

  private set(_state: ProxyState, text: string, tooltip: string | undefined, command: string): void {
    this.item.text = text;
    this.item.tooltip = tooltip;
    this.item.command = command;
  }
}
