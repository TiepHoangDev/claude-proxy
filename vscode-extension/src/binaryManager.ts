import * as fs from "fs";
import * as vscode from "vscode";
import { resolveLatestAsset } from "./github";

interface InstalledAsset {
  tag: string;
  name: string;
}

const INSTALLED_ASSET_KEY = "installedAsset";
const ACTIVATED_EXTENSION_VERSION_KEY = "activatedExtensionVersion";

/**
 * Ensures a claude-proxy binary is present under the extension's global
 * storage and returns its path. Uses the cached globalState entry to avoid
 * hitting the GitHub API on every activation; pass forceCheck to re-resolve
 * the latest release (e.g. from the "check for update" command). Also force
 * a re-check the first time a newly-updated extension version activates, so
 * a VS Code extension update pulls the matching binary release automatically.
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

  const extensionVersion = context.extension.packageJSON.version as string;
  const lastActivatedVersion = context.globalState.get<string>(ACTIVATED_EXTENSION_VERSION_KEY);
  if (lastActivatedVersion !== extensionVersion) {
    forceCheck = true;
  }

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
    await context.globalState.update(ACTIVATED_EXTENSION_VERSION_KEY, extensionVersion);
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
      try {
        await fs.promises.unlink(destPath);
      } catch {
        // file didn't exist or was locked — ignore
      }
      await fs.promises.rename(tmpPath, destPath);
    }
  );

  if (process.platform !== "win32") {
    await fs.promises.chmod(destPath, 0o755);
  }

  await context.globalState.update(INSTALLED_ASSET_KEY, { tag: asset.tag, name: asset.name });
  await context.globalState.update(ACTIVATED_EXTENSION_VERSION_KEY, extensionVersion);
  return destPath;
}
