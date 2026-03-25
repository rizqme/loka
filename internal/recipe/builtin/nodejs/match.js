// Detect Node.js / Bun projects and resolve runtime + package manager
(function() {
    if (!FileExists("package.json")) return false;

    // Detect runtime: bun > yarn > pnpm > npm
    if (FileExists("bun.lockb") && CommandExists("bun")) {
        SetVar("runtime", "bun");
        SetVar("image", "oven/bun:latest");
        SetVar("pm", "bun");
        SetVar("pm_install", "bun install");
        SetVar("pm_run", "bun run");
        SetVar("pm_start", "bun start");
    } else if (FileExists("yarn.lock") && CommandExists("yarn")) {
        SetVar("runtime", "node");
        SetVar("image", "node:20-slim");
        SetVar("pm", "yarn");
        SetVar("pm_install", "yarn install");
        SetVar("pm_run", "yarn");
        SetVar("pm_start", "yarn start");
    } else if (FileExists("pnpm-lock.yaml") && CommandExists("pnpm")) {
        SetVar("runtime", "node");
        SetVar("image", "node:20-slim");
        SetVar("pm", "pnpm");
        SetVar("pm_install", "pnpm install");
        SetVar("pm_run", "pnpm");
        SetVar("pm_start", "pnpm start");
    } else {
        SetVar("runtime", "node");
        SetVar("image", "node:20-slim");
        SetVar("pm", "npm");
        SetVar("pm_install", "npm install");
        SetVar("pm_run", "npm run");
        SetVar("pm_start", "npm start");
    }

    // Detect available scripts
    SetVar("has_build", HasScript("build") ? "true" : "false");
    SetVar("has_start", HasScript("start") ? "true" : "false");

    // Set image from var
    SetImage(GetVar("image"));

    return true;
})()
