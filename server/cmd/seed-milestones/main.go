// Command seed-milestones bridges the AUTHORED milestone rows in the design source
// (methodpoc/designs/aiarch/project/network.yaml — the rows with type: milestone) into
// the live project.json `network` slot's Milestones[] so they surface in the API
// (compute-at-read fills onCriticalPath/eventTime per read; this tool seeds only the
// AUTHORED id/name/public/dependsOn). network.yaml is the SINGLE SOURCE OF TRUTH for the
// milestone set; re-run this whenever the architect re-authors the milestones there.
//
// It follows the same decode → mutate → EncodeProjectJSON → write discipline as the
// other cmd/seed-* tools (the on-disk JSON shape has one writer). It does NOT commit.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"gopkg.in/yaml.v3"
)

// networkYAML is the minimal view of network.yaml this tool needs: the activities list,
// of which the milestone rows (type: milestone) carry the authored fan-in (dependencies)
// plus the public flag. Other fields are ignored.
type networkYAML struct {
	Activities []struct {
		ID           string   `yaml:"id"`
		Name         string   `yaml:"name"`
		Type         string   `yaml:"type"`
		Public       bool     `yaml:"public"`
		Dependencies []string `yaml:"dependencies"`
	} `yaml:"activities"`
}

func main() {
	networkPath := flag.String("network", "../../methodpoc/designs/aiarch/project/network.yaml", "path to network.yaml (the authoritative milestone source)")
	file := flag.String("file", "/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json", "path to project.json to rewrite")
	id := flag.String("id", "archistrator", "project id")
	flag.Parse()

	// --- Load the authored milestones from network.yaml. ---
	yraw, err := os.ReadFile(*networkPath)
	must(err, "read network.yaml")
	var ny networkYAML
	must(yaml.Unmarshal(yraw, &ny), "parse network.yaml")

	var milestones []projectstate.NetworkMilestone
	for _, a := range ny.Activities {
		if a.Type != "milestone" {
			continue
		}
		milestones = append(milestones, projectstate.NetworkMilestone{
			ID:        a.ID,
			Name:      a.Name,
			Public:    a.Public,
			DependsOn: a.Dependencies, // the milestone's fan-in (predecessor activity ids)
		})
	}
	if len(milestones) == 0 {
		fail("no type:milestone rows found in network.yaml")
	}

	// --- Decode the live project.json, set the network slot's milestones, re-encode. ---
	raw, err := os.ReadFile(*file)
	must(err, "read project.json")
	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(*id))
	must(err, "decode project.json")
	if !ok {
		fail("no project document in project.json")
	}

	net, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || net == nil {
		fail("project.json has no authored network slot model")
	}
	net.Milestones = milestones
	// Computed/Summary are compute-at-read only; never persisted (omitempty + this tool
	// never sets them). They stay absent on disk.

	enc, err := projectstate.EncodeProjectJSON(proj)
	must(err, "encode project.json")
	must(os.WriteFile(*file, enc, 0o644), "write project.json")

	fmt.Printf("seeded %d milestones into %s network slot:\n", len(milestones), *id)
	for _, m := range milestones {
		fmt.Printf("  %-10s public=%-5v dependsOn=%v\n", m.ID, m.Public, m.DependsOn)
	}
}

func must(err error, ctx string) {
	if err != nil {
		fail(ctx + ": " + err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "seed-milestones:", msg)
	os.Exit(1)
}
