package checks

// Task 3: the triage prompt, the instructions and version 0.4.0.

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const task3 = "Task 3 adds the triage prompt."

var (
	m3     = module("t3")
	ipv4RE = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
)

// stdioFacts is what the server says about itself over stdio: version,
// instructions, prompt names. One stdio session serves the three tests that
// read them, like a module-scoped fixture.
type stdioFacts struct {
	version      string
	instructions string
	prompts      []string
}

var (
	overStdioOnce sync.Once
	overStdioErr  string
	overStdioSeen stdioFacts
)

func overStdio(t *testing.T) stdioFacts {
	t.Helper()
	overStdioOnce.Do(func() {
		session, failure := connectStdio("--reports", filepath.Join(workDir, "reports-t3-stdio"))
		if failure != "" {
			overStdioErr = failure
			return
		}
		defer session.Close()
		init := session.InitializeResult()
		overStdioSeen.version = init.ServerInfo.Version
		overStdioSeen.instructions = init.Instructions
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		listed, err := session.ListPrompts(ctx, nil)
		if err != nil {
			overStdioErr = "prompts/list over stdio failed: " + err.Error()
			return
		}
		for _, prompt := range listed.Prompts {
			overStdioSeen.prompts = append(overStdioSeen.prompts, prompt.Name)
		}
	})
	if overStdioErr != "" {
		t.Fatal(overStdioErr)
	}
	return overStdioSeen
}

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func TestServerVersionIsAtLeast040(t *testing.T) {
	version := overStdio(t).version
	m := versionRE.FindStringSubmatch(version)
	if m == nil {
		t.Fatalf("the server version must look like 0.4.0, got %q", version)
	}
	var parts [3]int
	for i := range parts {
		parts[i], _ = strconv.Atoi(m[i+1])
	}
	if slices.Compare(parts[:], []int{0, 4, 0}) < 0 {
		t.Fatalf("the server version must be 0.4.0 or later, got %q. %s", version, task3)
	}
}

func TestInstructionsAddThePromptSentence(t *testing.T) {
	text := overStdio(t).instructions
	want := p1Instructions + " " + p2Sentence("15 minutes") + " " + p3Sentence + " " + p4Sentence
	if text != want {
		t.Fatalf("instructions must be the practice 3 text, a space, and the prompt sentence of CONTRACT.md, section 8.\n"+
			"expected: %q\ngot:      %q", want, text)
	}
	if n := utf8.RuneCountInString(text); n > 1000 {
		t.Fatalf("instructions must be at most 1,000 characters, got %d", n)
	}
}

func TestThePromptIsThereOverStdio(t *testing.T) {
	prompts := overStdio(t).prompts
	if !slices.Contains(prompts, expected.Prompt.Name) {
		t.Fatalf("prompts/list over stdio must hold %s; got %v. %s", expected.Prompt.Name, prompts, task3)
	}
}

func triageOf(t *testing.T, s *httpServer, token string) *mcp.Prompt {
	t.Helper()
	prompts := s.prompts(t, token)
	var names []string
	for _, prompt := range prompts {
		if prompt.Name == expected.Prompt.Name {
			return prompt
		}
		names = append(names, prompt.Name)
	}
	t.Fatalf("prompts/list over HTTP must hold %s; got %v. %s", expected.Prompt.Name, names, task3)
	return nil
}

func TestThePromptAndItsArguments(t *testing.T) {
	prompt := triageOf(t, m3.server(t), owner)
	if strings.TrimSpace(prompt.Description) == "" {
		t.Fatal("the triage prompt needs a description")
	}
	var got, want []string
	for _, arg := range prompt.Arguments {
		got = append(got, arg.Name+"="+strconv.FormatBool(arg.Required))
	}
	for _, arg := range expected.Prompt.Arguments {
		want = append(want, arg.Name+"="+strconv.FormatBool(arg.Required))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the triage prompt's arguments and required flags must be %v (CONTRACT.md, section 5); got %v", want, got)
	}
	for _, arg := range prompt.Arguments {
		if strings.TrimSpace(arg.Description) == "" {
			t.Fatalf("the argument %s needs a description", arg.Name)
		}
	}
}

func renderedText(t *testing.T, s *httpServer, arguments map[string]string, token string) string {
	t.Helper()
	result := s.getPrompt(t, arguments, token)
	if len(result.Messages) != 1 {
		t.Fatalf("the rendered prompt must be one message, got %d", len(result.Messages))
	}
	message := result.Messages[0]
	if message.Role != "user" {
		t.Fatalf("the rendered prompt's message must have role user, got %q", message.Role)
	}
	text, isText := message.Content.(*mcp.TextContent)
	if !isText || text.Text == "" {
		t.Fatal("the rendered prompt must be one text content")
	}
	return text.Text
}

func checkToolOrder(t *testing.T, text string) {
	t.Helper()
	type mention struct {
		at   int
		name string
	}
	var mentions []mention
	for _, name := range expected.Prompt.ToolOrder {
		loc := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).FindStringIndex(text)
		if loc == nil {
			t.Fatalf("the rendered prompt must name %s (CONTRACT.md, section 5)", name)
		}
		mentions = append(mentions, mention{loc[0], name})
	}
	sort.SliceStable(mentions, func(i, j int) bool { return mentions[i].at < mentions[j].at })
	var order []string
	for _, m := range mentions {
		order = append(order, m.name)
	}
	if !slices.Equal(order, expected.Prompt.ToolOrder) {
		t.Fatalf("the rendered prompt must first name the tools in the order %v; their first mentions come in the order %v",
			expected.Prompt.ToolOrder, order)
	}
}

func checkNoCorpusNames(t *testing.T, text string) {
	t.Helper()
	var hits []string
	for _, name := range corpusNames(t) {
		if strings.Contains(text, name) {
			hits = append(hits, name)
		}
	}
	if len(hits) > 0 {
		t.Fatalf("the rendered prompt names hosts or dates from s3_logs: %v", hits)
	}
	if addresses := ipv4RE.FindAllString(text, -1); len(addresses) > 0 {
		t.Fatalf("the rendered prompt names addresses: %v", addresses)
	}
}

func TestTheRenderedPromptWithEveryArgument(t *testing.T) {
	arguments := expected.Prompt.Render
	text := renderedText(t, m3.server(t), arguments, owner)
	for _, name := range sortedKeys(arguments) {
		if !strings.Contains(text, arguments[name]) {
			t.Fatalf("the rendered prompt must repeat the %s it was given, %q", name, arguments[name])
		}
	}
	checkToolOrder(t, text)
	checkNoCorpusNames(t, text)
}

func TestTheRenderedPromptWithTheSymptomOnly(t *testing.T) {
	arguments := expected.Prompt.RenderSymptomOnly
	text := renderedText(t, m3.server(t), arguments, owner)
	if !strings.Contains(text, arguments["symptom"]) {
		t.Fatal("the rendered prompt must repeat the symptom it was given")
	}
	checkToolOrder(t, text)
	checkNoCorpusNames(t, text)
}

func TestTheReadOnlyCallerGetsThePrompt(t *testing.T) {
	s := m3.server(t)
	triageOf(t, s, viewer)
	arguments := expected.Prompt.RenderSymptomOnly
	if !strings.Contains(renderedText(t, s, arguments, viewer), arguments["symptom"]) {
		t.Fatal("the rendered prompt for the read-only caller must repeat the symptom it was given")
	}
}

func TestAMissingSymptomIsRefused(t *testing.T) {
	s := m3.server(t)
	triageOf(t, s, owner)
	raw := s.rpc(t, "prompts/get", map[string]any{"name": expected.Prompt.Name,
		"arguments": map[string]any{"path": "hosts/example-7"}}, owner, "", nil)
	rpcError(t, raw, "symptom is required")
}

func TestAnUnknownPromptIsRefused(t *testing.T) {
	raw := m3.server(t).rpc(t, "prompts/get", map[string]any{"name": "no-such-prompt",
		"arguments": map[string]any{}}, owner, "", nil)
	rpcError(t, raw, "the server has no such prompt")
}
