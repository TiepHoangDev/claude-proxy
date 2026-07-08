import * as fs from "fs";
import * as vscode from "vscode";
import { resolveLatestAsset } from "./github";

interface InstalledAsset {
  tag: string;
  name: string;
}

const INSTALLED_ASSET_KEY = "installedAsset";

/**
 * Ensures a claude-proxy binary is present under the extension's global
 * storage and returns its path. Uses the cached globalState entry to avoid
 * hitting the GitHub API on every activation; pass forceCheck to re-resolve
 * the latest release (e.g. from the "check for update" command).
 */
export async function ensureBinary(context: vscode.ExtensionContext, forceCheck = false): Promise<string> {
  const devPath = vscode.workspace.getConfiguration("claudeProxy").get<string>("devBinary");
  if (devPath && fs.existsSync(devPath)) {
    if (process.platform !== "win32") {
      await fs.promises.chmod(devPath, 0o755);
    }
    return devPath;
  }

  const binDir = vscode.Uri.joinPath(context.globalStorageUri, "bin");
  await vscode.workspace.fs.createDirectory(binDir);

  const installed = context.globalState.get<InstalledAsset>(INSTALLED_ASSET_KEY);
  if (installed && !forceCheck) {
    const localPath = vscode.Uri.joinPath(binDir, installed.name).fsPath;
    if (fs.existsSync(localPath)) {
      return localPath;
    }
  }

  const asset = await resolveLatestAsset();
  const destPath = vscode.Uri.joinPath(binDir, asset.name).fsPath;

  if (fs.existsSync(destPath) && installed?.tag === asset.tag) {
    await context.globalState.update(INSTALLED_ASSET_KEY, { tag: asset.tag, name: asset.name });
    return destPath;
  }

  await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: `Downloading claude-proxy ${asset.tag}...` },
    async (progress) => {
      const res = await fetch(asset.url);
      if (!res.ok || !res.body) {
        throw new Error(`Failed to download ${asset.name}: HTTP ${res.status}`);
      }
      const total = Number(res.headers.get("content-length") ?? 0);
      const reader = res.body.getReader();
      const chunks: Uint8Array[] = [];
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
        if (total) {
          progress.report({ increment: (value.length / total) * 100 });
        }
      }
      const tmpPath = `${destPath}.download`;
      await fs.promises.writeFile(tmpPath, Buffer.concat(chunks));
      await fs.promises.rename(tmpPath, destPath);
    }
  );

  if (process.platform !== "win32") {
    await fs.promises.chmod(destPath, 0o755);
  }

  await context.globalState.update(INSTALLED_ASSET_KEY, { tag: asset.tag, name: asset.name });
  return destPath;
}
