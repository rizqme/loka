// Detect Next.js projects and resolve runtime + package manager
(function() {
    if (!FileExists("package.json")) return false;
    var pkg = ReadJSON("package.json");
    if (!pkg) return false;
    var deps = pkg.dependencies || {};
    var devDeps = pkg.devDependencies || {};
    if (!deps["next"] && !devDeps["next"]) return false;

    // Detect runtime + package manager
    if (FileExists("bun.lockb") && CommandExists("bun")) {
        SetVar("image", "oven/bun:latest");
        SetVar("pm_install", "bun install");
        SetVar("pm_run", "bun run");
        SetVar("pm_start", "bun start");
    } else if (FileExists("yarn.lock") && CommandExists("yarn")) {
        SetVar("image", "node:20-slim");
        SetVar("pm_install", "yarn install");
        SetVar("pm_run", "yarn");
        SetVar("pm_start", "yarn start");
    } else if (FileExists("pnpm-lock.yaml") && CommandExists("pnpm")) {
        SetVar("image", "node:20-slim");
        SetVar("pm_install", "pnpm install");
        SetVar("pm_run", "pnpm");
        SetVar("pm_start", "pnpm start");
    } else {
        SetVar("image", "node:20-slim");
        SetVar("pm_install", "npm install");
        SetVar("pm_run", "npm run");
        SetVar("pm_start", "npm start");
    }

    SetImage(GetVar("image"));
    return true;
})()
