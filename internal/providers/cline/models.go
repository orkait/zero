package cline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

const llmsParentWalkDepth = 6

// CatalogModel is one Cline picker entry loaded from the installed @cline/llms
// package (or the bundled snapshot when that package cannot be resolved).
type CatalogModel struct {
	ID               string
	Description      string
	ContextWindow    int
	ToolCall         bool
	Reasoning        bool
	ReasoningEfforts []string
	InputModalities  []string
	InputCost        float64
	OutputCost       float64
	Source           string
}

type rawModel struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	ContextWindow    float64        `json:"contextWindow"`
	MaxTokens        float64        `json:"maxTokens"`
	Capabilities     []string       `json:"capabilities"`
	Pricing          *rawPricing    `json:"pricing"`
	ReasoningOptions []rawReasoning `json:"reasoningOptions"`
}

type rawPricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type rawReasoning struct {
	Type   string `json:"type"`
	Values []any  `json:"values"`
}

// Snapshot of cline-pass used only when the installed Cline package cannot be
// located. The live @cline/llms list is preferred.
var fallbackCatalogModels = []CatalogModel{
	{ID: "cline-pass/glm-5.2", Description: "GLM-5.2", ContextWindow: 1_048_576, Source: "fallback"},
	{ID: "cline-pass/deepseek-v4-pro", Description: "DeepSeek V4 Pro", ContextWindow: 1_048_576, Source: "fallback"},
	{ID: "cline-pass/deepseek-v4-flash", Description: "DeepSeek V4 Flash", Source: "fallback"},
	{ID: "cline-pass/kimi-k2.7-code", Description: "Kimi K2.7 Code", Source: "fallback"},
	{ID: "cline-pass/kimi-k2.6", Description: "Kimi K2.6", Source: "fallback"},
	{ID: "cline-pass/qwen3.7-plus", Description: "Qwen3.7 Plus", Source: "fallback"},
	{ID: "cline-pass/qwen3.7-max", Description: "Qwen3.7 Max", Source: "fallback"},
	{ID: "cline-pass/minimax-m3", Description: "MiniMax-M3", Source: "fallback"},
	{ID: "cline-pass/mimo-v2.5", Description: "MiMo-V2.5", Source: "fallback"},
	{ID: "cline-pass/mimo-v2.5-pro", Description: "MiMo-V2.5-Pro", Source: "fallback"},
	{ID: "cline-pass/kimi-k3", Description: "Kimi K3", Source: "fallback"},
	{ID: "stepfun/step-3.7-flash", Description: "Step 3.7 Flash (free)", Source: "fallback"},
	{ID: "poolside/laguna-m.1:free", Description: "Laguna M.1 (free)", Source: "fallback"},
	{ID: "deepseek/deepseek-v4-flash", Description: "DeepSeek V4 Flash (free)", Source: "fallback"},
}

var (
	catalogMu    sync.Mutex
	catalogCache []CatalogModel
	catalogReady bool
)

// LoadCatalogModels returns the Cline subscription catalog from the installed
// @cline/llms package. Cline has no live /models endpoint; the list is shipped
// as getGeneratedProviderModels()["cline-pass"]. Falls back to a bundled
// snapshot when the package cannot be resolved.
func LoadCatalogModels() []CatalogModel {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if catalogReady {
		return cloneCatalog(catalogCache)
	}
	raw := loadRawModels("cline-pass")
	if len(raw) == 0 {
		raw = loadRawModels("cline")
	}
	live := make([]CatalogModel, 0, len(raw))
	for _, model := range raw {
		entry, ok := toCatalogModel(model)
		if !ok {
			continue
		}
		live = append(live, entry)
	}
	if len(live) == 0 {
		catalogCache = cloneCatalog(fallbackCatalogModels)
	} else {
		catalogCache = live
	}
	catalogReady = true
	return cloneCatalog(catalogCache)
}

// ClearCatalogCache drops the in-process catalog so tests can change env/path.
func ClearCatalogCache() {
	catalogMu.Lock()
	catalogCache = nil
	catalogReady = false
	catalogMu.Unlock()
}

func cloneCatalog(models []CatalogModel) []CatalogModel {
	if len(models) == 0 {
		return nil
	}
	out := make([]CatalogModel, len(models))
	copy(out, models)
	for i := range out {
		out[i].ReasoningEfforts = append([]string{}, out[i].ReasoningEfforts...)
		out[i].InputModalities = append([]string{}, out[i].InputModalities...)
	}
	return out
}

func loadRawModels(providerID string) []rawModel {
	entry := findClineLlmsEntry()
	if entry == "" {
		return nil
	}
	return loadGeneratedModels(entry, providerID)
}

func findClineLlmsEntry() string {
	if override := strings.TrimSpace(os.Getenv("CLINE_LLMS_PATH")); override != "" && fileExists(override) {
		return override
	}
	bin := findClineBinary()
	if bin == "" {
		return ""
	}
	dir := filepath.Dir(bin)
	for range llmsParentWalkDepth {
		for _, candidate := range []string{
			filepath.Join(dir, "node_modules", "@cline", "llms", "dist", "index.js"),
			filepath.Join(dir, "node_modules", "cline", "node_modules", "@cline", "llms", "dist", "index.js"),
		} {
			if fileExists(candidate) {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func findClineBinary() string {
	for _, name := range []string{"cline", "cline.cmd", "cline.exe"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return path
	}
	return ""
}

func loadGeneratedModels(entry, providerID string) []rawModel {
	info, err := os.Stat(entry)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		for _, candidate := range []string{
			filepath.Join(entry, "dist", "models.js"),
			filepath.Join(entry, "dist", "index.js"),
			filepath.Join(entry, "models.js"),
			filepath.Join(entry, "index.js"),
		} {
			if models := loadGeneratedModels(candidate, providerID); len(models) > 0 {
				return models
			}
		}
		return nil
	}

	lower := strings.ToLower(entry)
	if strings.HasSuffix(lower, ".json") {
		data, err := os.ReadFile(entry)
		if err != nil {
			return nil
		}
		return orderedModelsFromJSON(data, providerID)
	}

	for _, candidate := range jsCatalogCandidates(entry) {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if models := orderedModelsFromGeneratedJS(data, providerID); len(models) > 0 {
			return models
		}
	}
	if strings.HasSuffix(lower, ".js") {
		return orderedModelsFromNode(entry, providerID)
	}
	return nil
}

func jsCatalogCandidates(entry string) []string {
	dir := filepath.Dir(entry)
	base := strings.ToLower(filepath.Base(entry))
	candidates := make([]string, 0, 3)
	if base == "index.js" {
		candidates = append(candidates, filepath.Join(dir, "models.js"), entry)
	} else {
		candidates = append(candidates, entry, filepath.Join(dir, "models.js"))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate] || !fileExists(candidate) {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func orderedModelsFromJSON(data []byte, providerID string) []rawModel {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil
	}
	var (
		direct    []rawModel
		found     bool
		providers json.RawMessage
	)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := keyTok.(string)
		switch key {
		case providerID:
			direct = decodeOrderedModelObject(dec)
			found = true
		case "providers":
			if err := dec.Decode(&providers); err != nil {
				return nil
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil
			}
		}
	}
	if found {
		return direct
	}
	if len(providers) > 0 {
		return orderedModelsFromJSON(providers, providerID)
	}
	return nil
}

func decodeOrderedModelObject(dec *json.Decoder) []rawModel {
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil
	}
	out := []rawModel{}
	for dec.More() {
		if _, err := dec.Token(); err != nil {
			return nil
		}
		var model rawModel
		if err := dec.Decode(&model); err != nil {
			return nil
		}
		out = append(out, model)
	}
	_, _ = dec.Token()
	return out
}

func orderedModelsFromGeneratedJS(data []byte, providerID string) []rawModel {
	obj, ok := extractJSObjectAfterKey(string(data), providerID)
	if !ok {
		return nil
	}
	converted, err := jsLiteralToJSON(obj)
	if err != nil {
		return nil
	}
	return decodeOrderedModelObject(json.NewDecoder(bytes.NewReader(converted)))
}

func orderedModelsFromNode(entry, providerID string) []rawModel {
	node, err := exec.LookPath("node")
	if err != nil {
		node, err = exec.LookPath("nodejs")
		if err != nil {
			return nil
		}
	}
	abs, err := filepath.Abs(entry)
	if err != nil {
		return nil
	}
	moduleURL, err := json.Marshal(fileURL(abs))
	if err != nil {
		return nil
	}
	script := fmt.Sprintf(
		"import { getGeneratedProviderModels } from %s; process.stdout.write(JSON.stringify(getGeneratedProviderModels() ?? {}));",
		moduleURL,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--input-type=module", "-e", script)
	cmd.Env = append(os.Environ(), "NODE_NO_WARNINGS=1")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return orderedModelsFromJSON(out, providerID)
}

func fileURL(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return "file://" + slash
}

func extractJSObjectAfterKey(src, key string) (string, bool) {
	needle := `"` + key + `"`
	rest := src
	for {
		idx := strings.Index(rest, needle)
		if idx < 0 {
			return "", false
		}
		after := strings.TrimLeft(rest[idx+len(needle):], " \t\n\r")
		if strings.HasPrefix(after, ":") {
			after = strings.TrimLeft(after[1:], " \t\n\r")
			if strings.HasPrefix(after, "{") {
				obj, err := sliceJSObject(after)
				if err == nil {
					return obj, true
				}
			}
		}
		rest = rest[idx+len(needle):]
	}
}

func sliceJSObject(src string) (string, error) {
	if src == "" || src[0] != '{' {
		return "", fmt.Errorf("not an object")
	}
	depth := 0
	inString := false
	escape := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[:i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unclosed object")
}

type jsParser struct {
	s string
	i int
}

func jsLiteralToJSON(src string) ([]byte, error) {
	p := &jsParser{s: src}
	var out bytes.Buffer
	if err := p.value(&out); err != nil {
		return nil, err
	}
	p.skipWS()
	if p.i < len(p.s) {
		return nil, fmt.Errorf("trailing junk in JS object literal")
	}
	return out.Bytes(), nil
}

func (p *jsParser) value(out *bytes.Buffer) error {
	p.skipWS()
	if p.i >= len(p.s) {
		return fmt.Errorf("unexpected end of JS literal")
	}
	switch p.s[p.i] {
	case '{':
		return p.object(out)
	case '[':
		return p.array(out)
	case '"':
		return p.quotedString(out)
	case 't', 'f', 'n':
		return p.keyword(out)
	default:
		return p.number(out)
	}
}

func (p *jsParser) object(out *bytes.Buffer) error {
	p.i++
	out.WriteByte('{')
	first := true
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			return fmt.Errorf("unclosed JS object")
		}
		if p.s[p.i] == '}' {
			p.i++
			out.WriteByte('}')
			return nil
		}
		if !first {
			if p.s[p.i] != ',' {
				return fmt.Errorf("expected comma in JS object")
			}
			p.i++
			p.skipWS()
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				out.WriteByte('}')
				return nil
			}
			out.WriteByte(',')
		}
		first = false
		p.skipWS()
		if p.i < len(p.s) && p.s[p.i] == '"' {
			if err := p.quotedString(out); err != nil {
				return err
			}
		} else {
			key, err := p.ident()
			if err != nil {
				return err
			}
			out.WriteByte('"')
			out.WriteString(key)
			out.WriteByte('"')
		}
		p.skipWS()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return fmt.Errorf("expected ':' after JS object key")
		}
		p.i++
		out.WriteByte(':')
		if err := p.value(out); err != nil {
			return err
		}
	}
}

func (p *jsParser) array(out *bytes.Buffer) error {
	p.i++
	out.WriteByte('[')
	first := true
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			return fmt.Errorf("unclosed JS array")
		}
		if p.s[p.i] == ']' {
			p.i++
			out.WriteByte(']')
			return nil
		}
		if !first {
			if p.s[p.i] != ',' {
				return fmt.Errorf("expected comma in JS array")
			}
			p.i++
			p.skipWS()
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				out.WriteByte(']')
				return nil
			}
			out.WriteByte(',')
		}
		first = false
		if err := p.value(out); err != nil {
			return err
		}
	}
}

func (p *jsParser) quotedString(out *bytes.Buffer) error {
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return fmt.Errorf("expected string")
	}
	start := p.i
	p.i++
	escape := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			out.WriteString(p.s[start:p.i])
			return nil
		}
	}
	return fmt.Errorf("unterminated string")
}

func (p *jsParser) ident() (string, error) {
	if p.i >= len(p.s) {
		return "", fmt.Errorf("expected identifier")
	}
	c := rune(p.s[p.i])
	if c != '_' && c != '$' && !unicode.IsLetter(c) {
		return "", fmt.Errorf("expected identifier")
	}
	start := p.i
	p.i++
	for p.i < len(p.s) {
		c := rune(p.s[p.i])
		if c != '_' && c != '$' && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			break
		}
		p.i++
	}
	return p.s[start:p.i], nil
}

func (p *jsParser) keyword(out *bytes.Buffer) error {
	for _, word := range []string{"true", "false", "null"} {
		if strings.HasPrefix(p.s[p.i:], word) {
			end := p.i + len(word)
			if end < len(p.s) {
				next := rune(p.s[end])
				if unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' {
					continue
				}
			}
			out.WriteString(word)
			p.i = end
			return nil
		}
	}
	return fmt.Errorf("expected true, false, or null")
}

func (p *jsParser) number(out *bytes.Buffer) error {
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '-' || p.s[p.i] == '+') {
		p.i++
	}
	seenDigit := false
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		seenDigit = true
		p.i++
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			seenDigit = true
			p.i++
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		exp := false
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			exp = true
			p.i++
		}
		if !exp {
			return fmt.Errorf("invalid exponent")
		}
	}
	if !seenDigit || start == p.i {
		return fmt.Errorf("expected number")
	}
	n := p.s[start:p.i]
	if strings.HasPrefix(n, "+") {
		n = n[1:]
	}
	out.WriteString(n)
	return nil
}

func (p *jsParser) skipWS() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func toCatalogModel(model rawModel) (CatalogModel, bool) {
	id := strings.TrimSpace(model.ID)
	if id == "" {
		return CatalogModel{}, false
	}
	if strings.HasPrefix(strings.ToLower(id), "cline-free/") {
		return CatalogModel{}, false
	}
	name := strings.TrimSpace(model.Name)
	if name == "" {
		name = strings.TrimSpace(model.Description)
	}
	if name == "" {
		name = id
	}
	if isFreeModel(model) && !strings.Contains(strings.ToLower(name), "free") {
		name += " (free)"
	}
	entry := CatalogModel{
		ID:            id,
		Description:   name,
		ContextWindow: int(model.ContextWindow),
		InputCost:     0,
		OutputCost:    0,
		Source:        "cline-llms",
	}
	if model.Pricing != nil {
		entry.InputCost = model.Pricing.Input
		entry.OutputCost = model.Pricing.Output
	}
	for _, cap := range model.Capabilities {
		switch strings.ToLower(strings.TrimSpace(cap)) {
		case "tools":
			entry.ToolCall = true
		case "reasoning", "reasoning-effort":
			entry.Reasoning = true
		case "images":
			entry.InputModalities = appendUniqueFold(entry.InputModalities, "image")
		case "video":
			entry.InputModalities = appendUniqueFold(entry.InputModalities, "video")
		}
	}
	for _, option := range model.ReasoningOptions {
		if strings.EqualFold(option.Type, "effort") {
			entry.Reasoning = true
			for _, value := range option.Values {
				text, ok := value.(string)
				if !ok {
					continue
				}
				text = strings.TrimSpace(text)
				if text == "" {
					continue
				}
				entry.ReasoningEfforts = appendUniqueFold(entry.ReasoningEfforts, text)
			}
		}
		if strings.EqualFold(option.Type, "toggle") {
			entry.Reasoning = true
		}
	}
	return entry, true
}

func isFreeModel(model rawModel) bool {
	return model.Pricing != nil && model.Pricing.Input == 0 && model.Pricing.Output == 0
}

func appendUniqueFold(values []string, add string) []string {
	for _, value := range values {
		if strings.EqualFold(value, add) {
			return values
		}
	}
	return append(values, add)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
