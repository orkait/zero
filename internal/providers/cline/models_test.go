package cline

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadCatalogModelsFromJSONDropsFreeProductSurfaceAndTagsPricing(t *testing.T) {
	t.Setenv("PATH", "")
	path := writeFile(t, "models.json", `{
		"deepseek": {
			"deepseek/should-not-load": {"id": "deepseek/should-not-load", "name": "Wrong collection"}
		},
		"cline-pass": {
			"cline-pass/qwen3.8-max": {
				"id": "cline-pass/qwen3.8-max",
				"name": "Qwen3.8 Max",
				"contextWindow": 1000000,
				"maxTokens": 131072,
				"capabilities": ["tools", "reasoning"],
				"pricing": {"input": 2, "output": 6}
			},
			"cline-free/hidden": {
				"id": "cline-free/hidden",
				"name": "Hidden Free"
			},
			"cline-pass/deepseek-v4-flash": {
				"id": "cline-pass/deepseek-v4-flash",
				"name": "DeepSeek V4 Flash",
				"pricing": {"input": 0.14, "output": 0.28}
			},
			"deepseek/deepseek-v4-flash": {
				"id": "deepseek/deepseek-v4-flash",
				"name": "DeepSeek V4 Flash",
				"pricing": {"input": 0, "output": 0}
			},
			"poolside/laguna-s-2.1:free": {
				"id": "poolside/laguna-s-2.1:free",
				"name": "Laguna S 2.1 (free)",
				"pricing": {"input": 0, "output": 0}
			}
		}
	}`)
	t.Setenv("CLINE_LLMS_PATH", path)
	ClearCatalogCache()
	t.Cleanup(ClearCatalogCache)

	models := LoadCatalogModels()
	got := catalogIDs(models)
	if !contains(got, "cline-pass/qwen3.8-max") {
		t.Fatalf("live catalog missing qwen3.8-max: %#v", got)
	}
	if contains(got, "deepseek/should-not-load") {
		t.Fatalf("loaded vendor collection instead of cline-pass: %#v", got)
	}
	if contains(got, "cline-free/hidden") {
		t.Fatalf("included cline-free product-surface model: %#v", got)
	}

	byID := map[string]CatalogModel{}
	for _, model := range models {
		byID[model.ID] = model
	}
	if byID["deepseek/deepseek-v4-flash"].Description != "DeepSeek V4 Flash (free)" {
		t.Fatalf("free label = %q", byID["deepseek/deepseek-v4-flash"].Description)
	}
	if byID["cline-pass/deepseek-v4-flash"].Description != "DeepSeek V4 Flash" {
		t.Fatalf("paid twin label = %q", byID["cline-pass/deepseek-v4-flash"].Description)
	}
	if byID["poolside/laguna-s-2.1:free"].Description != "Laguna S 2.1 (free)" {
		t.Fatalf("already-tagged free label = %q", byID["poolside/laguna-s-2.1:free"].Description)
	}
	if byID["cline-pass/qwen3.8-max"].ContextWindow != 1_000_000 {
		t.Fatalf("context window = %d", byID["cline-pass/qwen3.8-max"].ContextWindow)
	}
	if !byID["cline-pass/qwen3.8-max"].ToolCall || !byID["cline-pass/qwen3.8-max"].Reasoning {
		t.Fatalf("capabilities not mapped: %+v", byID["cline-pass/qwen3.8-max"])
	}
	wantOrder := []string{
		"cline-pass/qwen3.8-max",
		"cline-pass/deepseek-v4-flash",
		"deepseek/deepseek-v4-flash",
		"poolside/laguna-s-2.1:free",
	}
	if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("catalog order = %#v, want %#v", got, wantOrder)
	}
}

func TestLoadCatalogModelsParsesGeneratedModelsJS(t *testing.T) {
	t.Setenv("PATH", "")
	dir := t.TempDir()
	writeNamedFile(t, filepath.Join(dir, "models.js"), `var pt={version:1,providers:{"other":{"x":{id:"x"}},"cline-pass":{"cline-pass/kimi-k3":{id:"cline-pass/kimi-k3",name:"Kimi K3",contextWindow:1e6,maxTokens:65536,capabilities:["tools","reasoning"],pricing:{input:1,output:2}},"cline-free/nope":{id:"cline-free/nope",name:"Nope"}}}}`)
	index := filepath.Join(dir, "index.js")
	writeNamedFile(t, index, `export function getGeneratedProviderModels() { throw new Error("should parse models.js, not execute index.js") }`)
	t.Setenv("CLINE_LLMS_PATH", index)
	ClearCatalogCache()
	t.Cleanup(ClearCatalogCache)

	models := LoadCatalogModels()
	got := catalogIDs(models)
	if !contains(got, "cline-pass/kimi-k3") {
		t.Fatalf("models.js catalog missing kimi-k3: %#v", got)
	}
	if contains(got, "cline-free/nope") {
		t.Fatalf("included cline-free model from models.js: %#v", got)
	}
	var kimi CatalogModel
	for _, model := range models {
		if model.ID == "cline-pass/kimi-k3" {
			kimi = model
			break
		}
	}
	if kimi.Source != "cline-llms" {
		t.Fatalf("kimi-k3 source = %q, want cline-llms (parsed models.js)", kimi.Source)
	}
	if kimi.ContextWindow != 1_000_000 {
		t.Fatalf("scientific notation context window = %d", kimi.ContextWindow)
	}
}

func TestLoadCatalogModelsResolvesLlmsFromClineBinaryOnPATH(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, clineBinaryName())
	writeNamedFile(t, binary, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	entryDir := filepath.Join(root, "node_modules", "@cline", "llms", "dist")
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNamedFile(t, filepath.Join(entryDir, "index.js"), "export function getGeneratedProviderModels() { return {} }\n")
	writeNamedFile(t, filepath.Join(entryDir, "models.js"), `pt={version:1,providers:{"cline-pass":{"nvidia/nemotron-3.5-lightning":{id:"nvidia/nemotron-3.5-lightning",name:"Nemotron 3.5 Lightning",pricing:{input:0,output:0}}}}}`)

	t.Setenv("PATH", binDir)
	t.Setenv("CLINE_LLMS_PATH", "")
	ClearCatalogCache()
	t.Cleanup(ClearCatalogCache)

	models := LoadCatalogModels()
	if !contains(catalogIDs(models), "nvidia/nemotron-3.5-lightning") {
		t.Fatalf("did not resolve @cline/llms from PATH: %#v", catalogIDs(models))
	}
}

func TestLoadCatalogModelsFallsBackWhenPackageUnresolvable(t *testing.T) {
	t.Setenv("CLINE_LLMS_PATH", filepath.Join(t.TempDir(), "missing", "index.js"))
	t.Setenv("PATH", "")
	ClearCatalogCache()
	t.Cleanup(ClearCatalogCache)

	models := LoadCatalogModels()
	if len(models) < 10 {
		t.Fatalf("fallback size = %d, want at least 10", len(models))
	}
	got := catalogIDs(models)
	if !contains(got, "cline-pass/glm-5.2") || !contains(got, "cline-pass/deepseek-v4-pro") {
		t.Fatalf("fallback missing snapshot models: %#v", got)
	}
	for _, model := range models {
		if !strings.Contains(model.ID, "/") {
			t.Fatalf("un-namespaced fallback id %q", model.ID)
		}
		if strings.HasPrefix(strings.ToLower(model.ID), "cline-free/") {
			t.Fatalf("fallback included cline-free id %q", model.ID)
		}
		if strings.Contains(strings.ToLower(model.Description), "(free) (free)") {
			t.Fatalf("double-tagged free label %q", model.Description)
		}
	}
}

func TestLoadCatalogModelsPrefersInstalledLlmsOverFallback(t *testing.T) {
	if _, err := exec.LookPath("cline"); err != nil {
		t.Skip("cline is not on PATH")
	}
	t.Setenv("CLINE_LLMS_PATH", "")
	ClearCatalogCache()
	t.Cleanup(ClearCatalogCache)

	models := LoadCatalogModels()
	live := 0
	for _, model := range models {
		if model.Source == "cline-llms" {
			live++
		}
	}
	if live == 0 {
		t.Fatalf("installed Cline did not yield a live @cline/llms catalog; got %#v", catalogIDs(models))
	}
}

func clineBinaryName() string {
	if runtime.GOOS == "windows" {
		return "cline.exe"
	}
	return "cline"
}

func writeFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeNamedFile(t, path, contents)
	return path
}

func writeNamedFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func catalogIDs(models []CatalogModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
