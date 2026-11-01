// Command oasmodels types archistrator's OpenAPI document from the one go tool appgen clients
// writes: the contracts carry an artifact draft as an opaque raw message, and this splices the
// projectstate slot models in and points those fields at them. It is the one archistrator-only
// step in the client layer, since it reflects archistrator's own types; the wire is untouched.
//
//	cd server && make gen-client
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// generatedPath is where appgen clients writes the document (appgen.json clients.oas).
	generatedPath = "api/openapi.generated.yaml"
	// oasPath is the document everything else reads.
	oasPath = "api/openapi.yaml"
)

// schemaRefPrefix is the JSON-pointer prefix every component schema $ref carries.
const schemaRefPrefix = "#/components/schemas/"

func main() {
	if err := run(generatedPath, oasPath); err != nil {
		fmt.Fprintln(os.Stderr, "oasmodels:", err)
		os.Exit(1)
	}
}

func run(path, dest string) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- this tool's own constant
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	comps, _ := doc["components"].(map[string]any)
	schemas, _ := comps["schemas"].(map[string]any)
	if schemas == nil {
		return fmt.Errorf("%s has no components.schemas", path)
	}
	repointed, err := spliceModelSchemas(schemas)
	if err != nil {
		return fmt.Errorf("splice slot-model schemas: %w", err)
	}
	if len(repointed) == 0 {
		return errors.New("no opaque DraftModel.model fields found to repoint (detection pattern drift?)")
	}
	fmt.Printf("repointed %d opaque draft field(s) onto Model* oneOf: %s\n", len(repointed), strings.Join(repointed, ", "))
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, out, 0o600) // #nosec G306 G703 -- this tool's own constant
}
