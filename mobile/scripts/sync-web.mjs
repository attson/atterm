import { cp, mkdir, readdir, rm } from "node:fs/promises";
import { dirname, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const RUNTIME_EXCLUDE = [
  /^.*\.test\.mjs$/,
  /^package(?:-lock)?\.json$/,
  /^node_modules(?:\/|$)/,
];

function toPOSIX(path) {
  return path.split(sep).join("/");
}

function shouldCopy(relPath) {
  const normalized = toPOSIX(relPath);
  return !RUNTIME_EXCLUDE.some((pattern) => pattern.test(normalized));
}

async function copyRuntimeFiles(srcDir, destDir, currentDir, copied) {
  const entries = await readdir(currentDir, { withFileTypes: true });
  for (const entry of entries) {
    const srcPath = resolve(currentDir, entry.name);
    const relPath = relative(srcDir, srcPath);
    if (!shouldCopy(relPath)) continue;

    const destPath = resolve(destDir, relPath);
    if (entry.isDirectory()) {
      await mkdir(destPath, { recursive: true });
      await copyRuntimeFiles(srcDir, destDir, srcPath, copied);
      continue;
    }
    if (!entry.isFile()) continue;
    await mkdir(dirname(destPath), { recursive: true });
    await cp(srcPath, destPath, { force: true });
    copied.push(toPOSIX(relPath));
  }
}

export async function syncWebAssets(src, dest) {
  const srcDir = resolve(src);
  const destDir = resolve(dest);
  await rm(destDir, { recursive: true, force: true });
  await mkdir(destDir, { recursive: true });
  const copied = [];
  await copyRuntimeFiles(srcDir, destDir, srcDir, copied);
  copied.sort();
  return { src: srcDir, dest: destDir, copied };
}

async function main() {
  const scriptDir = dirname(fileURLToPath(import.meta.url));
  const mobileDir = dirname(scriptDir);
  const repoRoot = dirname(mobileDir);
  const result = await syncWebAssets(resolve(repoRoot, "web"), resolve(mobileDir, "www"));
  console.log(`synced ${result.copied.length} web assets to ${result.dest}`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
