package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBasic(t *testing.T) {
	dir := t.TempDir()
	content := `
services:
  web:
    image: node:20
    ports:
      - "3000:3000"
    environment:
      NODE_ENV: production
  db:
    image: postgres:15
    ports:
      - "5432"
    environment:
      - POSTGRES_PASSWORD=secret
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	cf, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cf.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cf.Services))
	}

	components := cf.ToComponents()
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}

	var web, db *Component
	for i := range components {
		switch components[i].Name {
		case "web":
			web = &components[i]
		case "db":
			db = &components[i]
		}
	}
	if web == nil || db == nil {
		t.Fatal("expected web and db components")
	}
	if web.Image != "node:20" {
		t.Errorf("web image = %q, want node:20", web.Image)
	}
	if web.Port != 3000 || web.HostPort != 3000 {
		t.Errorf("web port = %d:%d, want 3000:3000", web.HostPort, web.Port)
	}
	if web.Env["NODE_ENV"] != "production" {
		t.Errorf("web NODE_ENV = %q, want production", web.Env["NODE_ENV"])
	}
	if db.Port != 5432 || db.HostPort != 0 {
		t.Errorf("db port = %d:%d, want 0:5432 (internal)", db.HostPort, db.Port)
	}
	if db.Env["POSTGRES_PASSWORD"] != "secret" {
		t.Errorf("db POSTGRES_PASSWORD = %q, want secret", db.Env["POSTGRES_PASSWORD"])
	}
}

func TestDeployOrder(t *testing.T) {
	dir := t.TempDir()
	content := `
services:
  web:
    image: node:20
    depends_on:
      - db
      - redis
  db:
    image: postgres:15
  redis:
    image: redis:7
`
	path := filepath.Join(dir, "compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	cf, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	order := cf.DeployOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 in order, got %d", len(order))
	}

	// web should come after db and redis.
	webIdx := -1
	dbIdx := -1
	redisIdx := -1
	for i, name := range order {
		switch name {
		case "web":
			webIdx = i
		case "db":
			dbIdx = i
		case "redis":
			redisIdx = i
		}
	}
	if webIdx < dbIdx || webIdx < redisIdx {
		t.Errorf("web (%d) should come after db (%d) and redis (%d)", webIdx, dbIdx, redisIdx)
	}
}

func TestFindComposeFile(t *testing.T) {
	dir := t.TempDir()
	if FindComposeFile(dir) != "" {
		t.Error("expected no compose file in empty dir")
	}
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}"), 0644)
	if FindComposeFile(dir) == "" {
		t.Error("expected to find docker-compose.yml")
	}
}

func TestParseCommandFormats(t *testing.T) {
	if parseCommand("npm start") != "npm start" {
		t.Error("string command failed")
	}
	if parseCommand([]interface{}{"node", "server.js"}) != "node server.js" {
		t.Error("list command failed")
	}
	if parseCommand(nil) != "" {
		t.Error("nil command should return empty")
	}
}
