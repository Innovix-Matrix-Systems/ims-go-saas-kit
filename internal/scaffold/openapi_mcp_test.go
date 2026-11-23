package scaffold

import (
	"strings"
	"testing"
)

func TestListOperationGetsAnMCPHint(t *testing.T) {
	paths := RenderRegions([]ResourceSpec{SpecFromFields("order_item", []Field{{Name: "name", Type: "string"}})}).Paths
	if !strings.Contains(paths, "# x-mcp: {tool: list_order_items}") {
		t.Fatalf("no commented x-mcp hint on the list operation:\n%s", paths)
	}
}

func TestRegenerationKeepsAnEnabledMCPMark(t *testing.T) {
	existing := markerPathsBegin + "\n" +
		"    get:\n      operationId: listProducts\n      x-mcp: {tool: list_products, title: Products}\n" +
		markerPathsEnd + "\n"
	rendered := "    get:\n      operationId: listProducts\n      # x-mcp: {tool: list_products}\n      parameters:\n" +
		"    post:\n      operationId: createProduct\n"

	got := keepMCPMarks(rendered, mcpMarks(existing))
	if !strings.Contains(got, "      x-mcp: {tool: list_products, title: Products}\n      parameters:") {
		t.Fatalf("the enabled mark was not kept in place of the hint:\n%s", got)
	}
	if strings.Contains(got, "# x-mcp") {
		t.Fatalf("the hint should be replaced:\n%s", got)
	}
	if strings.Count(got, "x-mcp") != 1 {
		t.Fatalf("createProduct must not get a mark:\n%s", got)
	}
}
