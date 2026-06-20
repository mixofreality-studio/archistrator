package main

// seed-contracts ingests extracted service-contract JSON data files into the
// archistrator project.json. Each *.json file in the data dir is unmarshalled
// into a projectstate.ServiceContract (Go json unmarshal is case-insensitive,
// so camelCase keys map onto PascalCase struct fields). The map key is the
// contract's Component field; if Component is empty the filename stem is used.
// The command leaves all other Project fields untouched and only sets
// Project.ServiceContracts.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

func main() {
	dataDir := flag.String("data", "./cmd/seed-contracts/data", "directory containing *.json contract files")
	file := flag.String("file", "/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json", "path to project.json to rewrite")
	id := flag.String("id", "archistrator", "project id")
	flag.Parse()

	n, err := ingest(*dataDir, *file, *id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed-contracts: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("seeded %d service contracts\n", n)
}

// ingest reads all *.json files from dataDir, loads project.json at file,
// sets Project.ServiceContracts to the parsed contracts, and writes back.
// Returns the number of contracts successfully ingested.
func ingest(dataDir, file, id string) (int, error) {
	contracts, err := loadContracts(dataDir)
	if err != nil {
		return 0, fmt.Errorf("load contracts: %w", err)
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", file, err)
	}

	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(id))
	if err != nil {
		return 0, fmt.Errorf("decode project.json: %w", err)
	}
	if !ok {
		return 0, fmt.Errorf("no project document in %s", file)
	}

	proj.ServiceContracts = contracts

	enc, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return 0, fmt.Errorf("encode project.json: %w", err)
	}

	if err := os.WriteFile(file, enc, 0o644); err != nil {
		return 0, fmt.Errorf("write %s: %w", file, err)
	}

	return len(contracts), nil
}

// loadContracts reads every *.json file from dir, unmarshals each into a
// ServiceContract, and returns the map keyed by Component (or filename stem
// if Component is empty). Files that fail to parse emit a WARNING and are
// skipped.
func loadContracts(dir string) (map[string]projectstate.ServiceContract, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	result := make(map[string]projectstate.ServiceContract)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING parse failed: %s: %v\n", e.Name(), err)
			continue
		}

		var sc projectstate.ServiceContract
		if err := json.Unmarshal(raw, &sc); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING parse failed: %s: %v\n", e.Name(), err)
			continue
		}

		key := sc.Component
		if key == "" {
			// fallback to filename stem
			key = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		}
		result[key] = sc
	}

	return result, nil
}
