// Detect Python projects and resolve pip command
(function() {
    if (!FileExists("requirements.txt") && !FileExists("pyproject.toml")) {
        return false;
    }

    // Find the right pip command
    if (CommandExists("pip3")) {
        SetVar("pip", "pip3");
    } else if (CommandExists("pip")) {
        SetVar("pip", "pip");
    } else if (CommandExists("python3")) {
        SetVar("pip", "python3 -m pip");
    } else if (CommandExists("python")) {
        SetVar("pip", "python -m pip");
    } else {
        SetVar("pip", "pip");
    }

    // Find the right python command
    if (CommandExists("python3")) {
        SetVar("python", "python3");
    } else {
        SetVar("python", "python");
    }

    // Detect framework for start command
    if (FileExists("manage.py")) {
        SetVar("framework", "django");
        SetStartCommand("{{python}} manage.py runserver 0.0.0.0:8000");
    } else if (FileExists("app.py") || FileExists("wsgi.py")) {
        SetVar("framework", "flask");
    }

    return true;
})()
