// Detect Vite projects (vite.config.*, or vite/vitepress in dependencies)
(function() {
    var configs = ListFiles("vite.config.*");
    var hasViteDep = false;
    var pkg = ReadJSON("package.json");
    if (pkg) {
        var deps = pkg.dependencies || {};
        var devDeps = pkg.devDependencies || {};
        hasViteDep = !!deps["vite"] || !!devDeps["vite"] ||
                     !!deps["vitepress"] || !!devDeps["vitepress"];
    }
    if (configs.length === 0 && !hasViteDep) return false;

    // Detect package manager
    if (FileExists("bun.lockb") && CommandExists("bun")) {
        SetVar("pm_install", "bun install");
        SetVar("pm_run", "bun run");
    } else if (FileExists("yarn.lock") && CommandExists("yarn")) {
        SetVar("pm_install", "yarn install");
        SetVar("pm_run", "yarn");
    } else if (FileExists("pnpm-lock.yaml") && CommandExists("pnpm")) {
        SetVar("pm_install", "pnpm install");
        SetVar("pm_run", "pnpm");
    } else {
        SetVar("pm_install", "npm install");
        SetVar("pm_run", "npm run");
    }

    return true;
})()
