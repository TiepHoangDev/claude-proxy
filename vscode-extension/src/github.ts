const REPO = "TiepHoangDev/claude-proxy";

export interface ResolvedAsset {
  tag: string;
  url: string;
  name: string;
}

/** Maps this Node process's platform/arch to the release asset name built by .github/workflows/release.yml. */
export function assetFileName(): string {
  const osMap: Record<string, string> = { win32: "windows", linux: "linux" };
  const archMap: Record<string, string> = { x64: "amd64" };
  const os = osMap[process.platform];
  const arch = archMap[process.arch];
  if (!os || !arch) {
    throw new Error(
      `Unsupported platform: ${process.platform}/${process.arch} (only Windows/Linux amd64 are published for now)`
    );
  }
  return `claude-proxy-${os}-${arch}${process.platform === "win32" ? ".exe" : ""}`;
}

export async function resolveLatestAsset(): Promise<ResolvedAsset> {
  const res = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, {
    headers: { Accept: "application/vnd.github+json" },
  });
  if (!res.ok) {
    throw new Error(`GitHub API error ${res.status} while resolving latest release`);
  }
  const release = (await res.json()) as {
    tag_name: string;
    assets: { name: string; browser_download_url: string }[];
  };
  const name = assetFileName();
  const asset = release.assets?.find((a) => a.name === name);
  if (!asset) {
    throw new Error(`No release asset named "${name}" found in release ${release.tag_name}`);
  }
  return { tag: release.tag_name, url: asset.browser_download_url, name };
}
