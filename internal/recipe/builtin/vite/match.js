// Detect Vite projects and resolve runtime + package manager
(function() {
    var configs = ListFiles("vite.config.*");
    if (configs.length === 0) return false;

    // Detect runtime + package manager
    if (FileExists("bun.lockb") && CommandExists("bun")) {
        SetVar("image", "oven/bun:latest");
        SetVar("pm_install", "bun install");
        SetVar("pm_run", "bun run");
        SetVar("npx", "bunx");
    } else if (FileExists("yarn.lock") && CommandExists("yarn")) {
        SetVar("image", "node:20-slim");
        SetVar("pm_install", "yarn install");
        SetVar("pm_run", "yarn");
        SetVar("npx", "yarn dlx");
    } else if (FileExists("pnpm-lock.yaml") && CommandExists("pnpm")) {
        SetVar("image", "node:20-slim");
        SetVar("pm_install", "pnpm install");
        SetVar("pm_run", "pnpm");
        SetVar("npx", "pnpx");
    } else {
        SetVar("image", "node:20-slim");
        SetVar("pm_install", "npm install");
        SetVar("pm_run", "npm run");
        SetVar("npx", "npx");
    }

    SetImage(GetVar("image"));
    return true;
})()
