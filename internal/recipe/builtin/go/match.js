// Detect Go projects
(function() {
    if (!FileExists("go.mod")) return false;

    // Read module name for service naming
    var mod = ReadFile("go.mod");
    var match = mod.match(/^module\s+(.+)/m);
    if (match) {
        SetVar("module", match[1].trim());
    }

    // Detect build output name
    SetVar("binary", "app");

    return true;
})()
