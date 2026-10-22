package triage

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const triagePromptName = "triage"

func triagePrompt() *mcp.Prompt {
	return &mcp.Prompt{
		Name:        triagePromptName,
		Title:       "Triage a symptom",
		Description: "A guided investigation of a symptom in the logs: where to look, which tools to use in which order, and what a finding must hold.",
		Arguments: []*mcp.PromptArgument{
			{Name: "symptom", Required: true, Description: "What the user saw, in their words"},
			{Name: "path", Description: "The file or folder to look in; the whole log root when it is empty or not given"},
			{Name: "since", Description: "The start of the time to look at, a date or a date and time"},
			{Name: "until", Description: "The end of the time to look at, a date or a date and time"},
		},
	}
}

// renderTriage writes the prompt's one user message. go-sdk doesn't check a
// prompt's required arguments, so a missing symptom is refused here.
func renderTriage(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	args := req.Params.Arguments
	symptom := strings.TrimSpace(args["symptom"])
	if symptom == "" {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: `prompt "triage" needs the argument "symptom"`}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Investigate this symptom in the logs: %s\n\n", args["symptom"])
	if path := args["path"]; path != "" {
		fmt.Fprintf(&b, "Where to look: %s\n", path)
	} else {
		b.WriteString("Where to look: the whole log root\n")
	}
	switch since, until := args["since"], args["until"]; {
	case since != "" && until != "":
		fmt.Fprintf(&b, "Time to look at: from %s until %s\n", since, until)
	case since != "":
		fmt.Fprintf(&b, "Time to look at: from %s on\n", since)
	case until != "":
		fmt.Fprintf(&b, "Time to look at: until %s\n", until)
	default:
		b.WriteString("Time to look at: not given; find it from the symptom and the logs\n")
	}
	b.WriteString(`
Work through it in this order, and read each answer before the next call:

1. Call list_logs on the place to look, to see which files and formats are there.
2. Call summarize_logs on that place for the time of the symptom, and again for a quiet window of the same length just before it. Compare the two. A group that is just as steady on other days is background, not the cause.
3. For a group that stands out, call group_lines with the scan_id and its group_id to read its lines.
4. Call timeline with the scan_id to see when those lines start, peak and stop, and how a number in them moves.
5. Call search_logs for words from the symptom, and to find what other sources said at the same time.
6. Call read_lines around the lines that matter, to see what came just before and after.
7. Before you trust an order of events across files, check that the sources agree on time: a file can hold lines stamped with another day, and a test log stamps every line with the time its run started.
8. When you know what happened, record it with save_finding. Cite a file and a line for every claim, and say what the logs don't show.
`)
	return &mcp.GetPromptResult{
		Description: "Triage: " + symptom,
		Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: b.String()}}},
	}, nil
}
