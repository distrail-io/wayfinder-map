// Command edgecases writes fixtures/map-edge-cases.json: map-214.json edited
// so that one map has every edge case the viewer must flag. Run it from the
// repo root: go run ./fixtures/edgecases
package main

import (
	"encoding/json"
	"log"
	"os"
)

type obj = map[string]any

func blockedBy(more bool, refs ...[3]any) obj {
	nodes := []any{}
	for _, r := range refs {
		nodes = append(nodes, obj{"number": r[0], "state": r[1], "repository": obj{"nameWithOwner": r[2]}})
	}
	return obj{"pageInfo": obj{"hasNextPage": more}, "nodes": nodes}
}

func main() {
	raw, err := os.ReadFile("fixtures/map-214.json")
	if err != nil {
		log.Fatal(err)
	}
	var doc obj
	if err := json.Unmarshal(raw, &doc); err != nil {
		log.Fatal(err)
	}
	issue := doc["data"].(obj)["repository"].(obj)["issue"].(obj)
	issue["state"] = "CLOSED"                    // a closed map
	issue["subIssuesSummary"].(obj)["total"] = 9 // more tickets than fetched
	issue["subIssues"].(obj)["pageInfo"] = obj{"hasNextPage": true}
	ticket := map[float64]obj{}
	for _, n := range issue["subIssues"].(obj)["nodes"].([]any) {
		ticket[n.(obj)["number"].(float64)] = n.(obj)
	}
	ticket[215]["assignees"] = obj{"nodes": []any{obj{"login": "octocat"}}} // claimed
	ticket[216]["state"] = "CLOSED"                                         // done though assigned, and
	ticket[216]["assignees"] = obj{"nodes": []any{obj{"login": "hubot"}}}   // its edges fade
	for _, n := range []float64{217, 218, 219} {
		for _, b := range ticket[n]["blockedBy"].(obj)["nodes"].([]any) {
			if b.(obj)["number"] == 216.0 {
				b.(obj)["state"] = "CLOSED"
			}
		}
	}
	ticket[217]["labels"] = obj{"nodes": []any{obj{"name": "ready-for-agent"}}}                                                   // no type
	ticket[220]["blockedBy"].(obj)["pageInfo"] = obj{"hasNextPage": true}                                                         // more blockers than fetched
	ticket[221]["blockedBy"] = blockedBy(false, [3]any{220, "OPEN", "banked/ask-barry"}, [3]any{222, "OPEN", "banked/ask-barry"}) // a cycle
	ticket[222]["blockedBy"] = blockedBy(false, [3]any{221, "OPEN", "banked/ask-barry"}, [3]any{9, "OPEN", "other/repo"})         // cross-repo
	for _, t := range ticket {
		if _, ok := t["blockedBy"].(obj)["pageInfo"]; !ok {
			t["blockedBy"].(obj)["pageInfo"] = obj{"hasNextPage": false}
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("fixtures/map-edge-cases.json", out, 0o644); err != nil {
		log.Fatal(err)
	}
}
