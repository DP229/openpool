package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dp229/openpool/pkg/p2p"
)

// ── GhostBoundary / GhostRegion Tests ────────────────────────────────────────────

func TestGhostBoundary_Serialization(t *testing.T) {
	gb := GhostBoundary{
		Direction: "north",
		Width:     2,
		Data:      json.RawMessage(`[1.0, 2.0, 3.0]`),
	}

	data, err := json.Marshal(gb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GhostBoundary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Direction != "north" {
		t.Errorf("Direction = %s, want north", decoded.Direction)
	}
	if decoded.Width != 2 {
		t.Errorf("Width = %d, want 2", decoded.Width)
	}
}

func TestGhostRegion_WithBoundaries(t *testing.T) {
	gr := GhostRegion{
		InteriorI:  10,
		InteriorJ:  10,
		GhostWidth: 2,
		Interior:   json.RawMessage(`{"type":"float64_array"}`),
		Boundaries: []GhostBoundary{
			{Direction: "north", Width: 2, Data: json.RawMessage(`[0.1, 0.2]`)},
			{Direction: "south", Width: 2, Data: json.RawMessage(`[0.3]`)},
			{Direction: "west", Width: 2, Data: nil},
			{Direction: "east", Width: 2, Data: nil},
		},
	}

	data, err := json.Marshal(gr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GhostRegion
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.InteriorI != 10 {
		t.Errorf("InteriorI = %d, want 10", decoded.InteriorI)
	}
	if len(decoded.Boundaries) != 4 {
		t.Fatalf("len(Boundaries) = %d, want 4", len(decoded.Boundaries))
	}

	filled := 0
	for _, b := range decoded.Boundaries {
		if b.Data != nil && string(b.Data) != "null" && len(b.Data) > 0 {
			filled++
		}
	}
	if filled != 2 {
		t.Errorf("filled boundaries = %d, want 2", filled)
	}
}

// ── DAGNode Tests ─────────────────────────────────────────────────────────────────

func TestDAGNode_BasicFields(t *testing.T) {
	node := &DAGNode{
		ID:       "tile-0-0",
		TaskID:   "cfd-sim-1",
		Index:    0,
		Parents:  []string{},
		Children: []string{"tile-0-1", "tile-1-0"},
		Params:   json.RawMessage(`{"type":"cfd_chunk","i_start":0,"i_end":10,"j_start":0,"j_end":10}`),
		Credits:  10,
		Timeout:  60,
	}

	if len(node.Children) != 2 {
		t.Errorf("len(Children) = %d, want 2", len(node.Children))
	}
	if node.Children[0] != "tile-0-1" {
		t.Errorf("Children[0] = %s, want tile-0-1", node.Children[0])
	}
}

func TestDAGNode_WithGhostRegions(t *testing.T) {
	node := &DAGNode{
		ID:      "tile-1-1",
		Index:   4,
		Parents: []string{"tile-0-1", "tile-1-0"},
		GhostRegions: []GhostRegion{{
			GhostWidth: 1,
			Boundaries: []GhostBoundary{
				{Direction: "north", Width: 1},
				{Direction: "south", Width: 1},
				{Direction: "west", Width: 1},
				{Direction: "east", Width: 1},
			},
		}},
	}

	if len(node.GhostRegions) != 1 {
		t.Fatalf("len(GhostRegions) = %d, want 1", len(node.GhostRegions))
	}
	if len(node.GhostRegions[0].Boundaries) != 4 {
		t.Errorf("len(Boundaries) = %d, want 4", len(node.GhostRegions[0].Boundaries))
	}
}

// ── DAGResult Tests ───────────────────────────────────────────────────────────────

func TestDAGResult_Success(t *testing.T) {
	r := &DAGResult{
		NodeID:     "tile-0-0",
		Index:      0,
		Success:    true,
		Data:       json.RawMessage(`{"residual":0.001}`),
		DurationMs: 150,
	}
	if !r.Success {
		t.Error("expected Success=true")
	}
	if r.DurationMs != 150 {
		t.Errorf("DurationMs = %d, want 150", r.DurationMs)
	}
}

func TestDAGResult_Failure(t *testing.T) {
	r := &DAGResult{
		NodeID:  "tile-1-0",
		Index:   3,
		Success: false,
		Error:   "connection timeout",
	}
	if r.Success {
		t.Error("expected Success=false")
	}
	if r.Error != "connection timeout" {
		t.Errorf("Error = %s, want 'connection timeout'", r.Error)
	}
}

// ── DAGTask Tests ─────────────────────────────────────────────────────────────────

func TestDAGTask_WithMeshSpec(t *testing.T) {
	task := &DAGTask{
		ID:       "cfd-run-42",
		Code:     "solve_navier_stokes",
		Lang:     "python",
		Credits:  100,
		Timeout:  300,
		MeshSpec: json.RawMessage(`{"rows":3,"cols":3,"ghost_width":2}`),
	}

	var spec CFDMeshSpec
	if err := json.Unmarshal(task.MeshSpec, &spec); err != nil {
		t.Fatalf("unmarshal mesh spec: %v", err)
	}
	if spec.Rows != 3 || spec.Cols != 3 || spec.GhostWidth != 2 {
		t.Errorf("spec = %+v, want rows=3 cols=3 ghost_width=2", spec)
	}
}

// ── Topological Level Tests ───────────────────────────────────────────────────────

func TestTopologicalLevels_LinearChain(t *testing.T) {
	nodes := []*DAGNode{
		{ID: "a", Parents: []string{}, Children: []string{"b"}},
		{ID: "b", Parents: []string{"a"}, Children: []string{"c"}},
		{ID: "c", Parents: []string{"b"}, Children: []string{}},
	}

	levels, err := topologicalLevels(nodes)
	if err != nil {
		t.Fatalf("topologicalLevels: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("len(levels) = %d, want 3", len(levels))
	}
	if levels[0][0].ID != "a" {
		t.Errorf("level 0 = %s, want a", levels[0][0].ID)
	}
	if levels[1][0].ID != "b" {
		t.Errorf("level 1 = %s, want b", levels[1][0].ID)
	}
	if levels[2][0].ID != "c" {
		t.Errorf("level 2 = %s, want c", levels[2][0].ID)
	}
}

func TestTopologicalLevels_Diamond(t *testing.T) {
	nodes := []*DAGNode{
		{ID: "root", Parents: []string{}, Children: []string{"left", "right"}},
		{ID: "left", Parents: []string{"root"}, Children: []string{"sink"}},
		{ID: "right", Parents: []string{"root"}, Children: []string{"sink"}},
		{ID: "sink", Parents: []string{"left", "right"}, Children: []string{}},
	}

	levels, err := topologicalLevels(nodes)
	if err != nil {
		t.Fatalf("topologicalLevels: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("len(levels) = %d, want 3", len(levels))
	}
	if len(levels[0]) != 1 || levels[0][0].ID != "root" {
		t.Errorf("level 0 = %+v, want [root]", levelIDs(levels[0]))
	}
	if len(levels[1]) != 2 {
		t.Errorf("level 1 count = %d, want 2", len(levels[1]))
	}
	if len(levels[2]) != 1 || levels[2][0].ID != "sink" {
		t.Errorf("level 2 = %+v, want [sink]", levelIDs(levels[2]))
	}
}

func TestTopologicalLevels_Flat(t *testing.T) {
	nodes := []*DAGNode{
		{ID: "a", Parents: []string{}, Children: []string{}},
		{ID: "b", Parents: []string{}, Children: []string{}},
		{ID: "c", Parents: []string{}, Children: []string{}},
	}

	levels, err := topologicalLevels(nodes)
	if err != nil {
		t.Fatalf("topologicalLevels: %v", err)
	}
	if len(levels) != 1 {
		t.Fatalf("len(levels) = %d, want 1", len(levels))
	}
	if len(levels[0]) != 3 {
		t.Errorf("level 0 count = %d, want 3", len(levels[0]))
	}
}

func TestTopologicalLevels_CycleDetection(t *testing.T) {
	nodes := []*DAGNode{
		{ID: "a", Parents: []string{"b"}, Children: []string{"b"}},
		{ID: "b", Parents: []string{"a"}, Children: []string{"a"}},
	}

	_, err := topologicalLevels(nodes)
	if err == nil {
		t.Fatal("expected error for cyclic graph")
	}
}

func TestTopologicalLevels_SingleNode(t *testing.T) {
	nodes := []*DAGNode{
		{ID: "only", Parents: []string{}, Children: []string{}},
	}

	levels, err := topologicalLevels(nodes)
	if err != nil {
		t.Fatalf("topologicalLevels: %v", err)
	}
	if len(levels) != 1 {
		t.Fatalf("len(levels) = %d, want 1", len(levels))
	}
	if len(levels[0]) != 1 || levels[0][0].ID != "only" {
		t.Errorf("level 0 = %+v, want [only]", levelIDs(levels[0]))
	}
}

// ── Ghost Data Injection Tests ────────────────────────────────────────────────────

func TestInjectGhostData_ByPosition(t *testing.T) {
	node := &DAGNode{
		ID: "tile-1-1",
		GhostRegions: []GhostRegion{{
			Boundaries: []GhostBoundary{
				{Direction: "north", Width: 1},
				{Direction: "south", Width: 1},
				{Direction: "west", Width: 1},
				{Direction: "east", Width: 1},
			},
		}},
	}

	parentResults := map[string]*DAGResult{
		"tile-0-1": {NodeID: "tile-0-1", Success: true, Data: json.RawMessage(`[0.1, 0.2]`)},
		"tile-1-0": {NodeID: "tile-1-0", Success: true, Data: json.RawMessage(`[0.3, 0.4]`)},
	}

	injectGhostData(node, parentResults)

	boundaries := node.GhostRegions[0].Boundaries

	filledCount := 0
	for _, b := range boundaries {
		if b.Data != nil {
			filledCount++
		}
	}
	if filledCount != 2 {
		t.Errorf("filled boundaries = %d, want 2 (from 2 successful parents)", filledCount)
	}

	if boundaries[0].Data == nil {
		t.Error("north boundary should be filled (first nil slot from first parent)")
	}
}

func TestInjectGhostData_FailedParent(t *testing.T) {
	node := &DAGNode{
		ID: "tile-1-0",
		GhostRegions: []GhostRegion{{
			Boundaries: []GhostBoundary{
				{Direction: "north", Width: 1},
			},
		}},
	}

	parentResults := map[string]*DAGResult{
		"tile-0-0": {NodeID: "tile-0-0", Success: false, Error: "timeout"},
	}

	injectGhostData(node, parentResults)

	if node.GhostRegions[0].Boundaries[0].Data != nil {
		t.Error("north boundary should NOT be filled from a failed parent")
	}
}

func TestInjectGhostData_NoGhostRegions(t *testing.T) {
	node := &DAGNode{
		ID: "flat-chunk-0",
	}

	parentResults := map[string]*DAGResult{
		"parent": {NodeID: "parent", Success: true, Data: json.RawMessage(`{}`)},
	}

	injectGhostData(node, parentResults)
}

func TestInjectGhostData_DirectionSuffix(t *testing.T) {
	node := &DAGNode{
		ID: "center",
		GhostRegions: []GhostRegion{{
			Boundaries: []GhostBoundary{
				{Direction: "north", Width: 1},
				{Direction: "south", Width: 1},
			},
		}},
	}

	parentResults := map[string]*DAGResult{
		"upstream-north": {NodeID: "upstream-north", Success: true, Data: json.RawMessage(`"north_data"`)},
		"upstream-south": {NodeID: "upstream-south", Success: true, Data: json.RawMessage(`"south_data"`)},
	}

	injectGhostData(node, parentResults)

	if string(node.GhostRegions[0].Boundaries[0].Data) != `"north_data"` {
		t.Errorf("north boundary = %s, want north_data", string(node.GhostRegions[0].Boundaries[0].Data))
	}
	if string(node.GhostRegions[0].Boundaries[1].Data) != `"south_data"` {
		t.Errorf("south boundary = %s, want south_data", string(node.GhostRegions[0].Boundaries[1].Data))
	}
}

// ── CFD Mesh DAG Builder Tests ────────────────────────────────────────────────────

func TestBuildCFDMeshDAG_1x1(t *testing.T) {
	task := &DAGTask{
		ID:       "cfd-1x1",
		Credits:  10,
		Timeout:  60,
		MeshSpec: json.RawMessage(`{"rows":1,"cols":1,"ghost_width":1}`),
	}

	nodes, err := BuildCFDMeshDAG(task)
	if err != nil {
		t.Fatalf("BuildCFDMeshDAG: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].ID != "tile-0-0" {
		t.Errorf("node ID = %s, want tile-0-0", nodes[0].ID)
	}
	if len(nodes[0].Parents) != 0 {
		t.Errorf("1x1 tile should have no parents, got %d", len(nodes[0].Parents))
	}
	if len(nodes[0].GhostRegions[0].Boundaries) != 0 {
		t.Errorf("1x1 tile should have no ghost boundaries, got %d", len(nodes[0].GhostRegions[0].Boundaries))
	}
}

func TestBuildCFDMeshDAG_2x2(t *testing.T) {
	task := &DAGTask{
		ID:       "cfd-2x2",
		Credits:  10,
		Timeout:  60,
		MeshSpec: json.RawMessage(`{"rows":2,"cols":2,"ghost_width":1}`),
	}

	nodes, err := BuildCFDMeshDAG(task)
	if err != nil {
		t.Fatalf("BuildCFDMeshDAG: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("len(nodes) = %d, want 4", len(nodes))
	}

	nodeMap := make(map[string]*DAGNode)
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	tile00 := nodeMap["tile-0-0"]
	tile01 := nodeMap["tile-0-1"]
	tile10 := nodeMap["tile-1-0"]
	tile11 := nodeMap["tile-1-1"]

	if len(tile00.Parents) != 0 {
		t.Errorf("tile-0-0 parents = %d, want 0 (root)", len(tile00.Parents))
	}
	if len(tile10.Parents) != 1 {
		t.Errorf("tile-1-0 parents = %d, want 1", len(tile10.Parents))
	}
	if tile10.Parents[0] != "tile-0-0" {
		t.Errorf("tile-1-0 parent = %s, want tile-0-0", tile10.Parents[0])
	}
	if len(tile01.Parents) != 1 {
		t.Errorf("tile-0-1 parents = %d, want 1", len(tile01.Parents))
	}
	if len(tile11.Parents) != 2 {
		t.Errorf("tile-1-1 parents = %d, want 2", len(tile11.Parents))
	}

	if len(tile00.GhostRegions[0].Boundaries) != 2 {
		t.Errorf("tile-0-0 ghost boundaries = %d, want 2 (south+east)", len(tile00.GhostRegions[0].Boundaries))
	}
	if len(tile11.GhostRegions[0].Boundaries) != 2 {
		t.Errorf("tile-1-1 ghost boundaries = %d, want 2 (north+west)", len(tile11.GhostRegions[0].Boundaries))
	}
	if len(tile01.GhostRegions[0].Boundaries) != 2 {
		t.Errorf("tile-0-1 ghost boundaries = %d, want 2 (south+west)", len(tile01.GhostRegions[0].Boundaries))
	}
	if len(tile10.GhostRegions[0].Boundaries) != 2 {
		t.Errorf("tile-1-0 ghost boundaries = %d, want 2 (north+east)", len(tile10.GhostRegions[0].Boundaries))
	}
}

func TestBuildCFDMeshDAG_3x3(t *testing.T) {
	task := &DAGTask{
		ID:       "cfd-3x3",
		Credits:  10,
		Timeout:  60,
		MeshSpec: json.RawMessage(`{"rows":3,"cols":3,"ghost_width":2}`),
	}

	nodes, err := BuildCFDMeshDAG(task)
	if err != nil {
		t.Fatalf("BuildCFDMeshDAG: %v", err)
	}
	if len(nodes) != 9 {
		t.Fatalf("len(nodes) = %d, want 9", len(nodes))
	}

	nodeMap := make(map[string]*DAGNode)
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	center := nodeMap["tile-1-1"]
	if len(center.Parents) != 2 {
		t.Errorf("center parents = %d, want 2", len(center.Parents))
	}
	if len(center.GhostRegions[0].Boundaries) != 4 {
		t.Errorf("center ghost boundaries = %d, want 4", len(center.GhostRegions[0].Boundaries))
	}
	if center.GhostRegions[0].GhostWidth != 2 {
		t.Errorf("center ghost width = %d, want 2", center.GhostRegions[0].GhostWidth)
	}

	levels, err := topologicalLevels(nodes)
	if err != nil {
		t.Fatalf("topologicalLevels: %v", err)
	}
	if len(levels) < 2 {
		t.Errorf("3x3 mesh should have at least 2 levels, got %d", len(levels))
	}
}

func TestBuildCFDMeshDAG_Defaults(t *testing.T) {
	task := &DAGTask{
		ID:      "cfd-default",
		Credits: 10,
		Timeout: 60,
	}

	nodes, err := BuildCFDMeshDAG(task)
	if err != nil {
		t.Fatalf("BuildCFDMeshDAG: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("default mesh should be 1x1, got %d nodes", len(nodes))
	}
}

// ── DAGSpec Streaming Reduce Tests ────────────────────────────────────────────────

func TestDAGSpec_OnPartialCallback(t *testing.T) {
	var partials []*DAGResult

	spec := DAGSpec{
		Build: func(task *DAGTask) ([]*DAGNode, error) {
			return []*DAGNode{
				{ID: "a", Index: 0, Parents: []string{}, Credits: 10, Timeout: 30},
				{ID: "b", Index: 1, Parents: []string{}, Credits: 10, Timeout: 30},
			}, nil
		},
		OnPartial: func(result *DAGResult) bool {
			partials = append(partials, result)
			return true
		},
		Finalize: func(results []*DAGResult) (json.RawMessage, error) {
			return json.RawMessage(`{"count":` + string(rune('0'+len(results))) + `}`), nil
		},
	}

	task := &DAGTask{ID: "test-streaming", Credits: 10, Timeout: 30}

	nodes, err := spec.Build(task)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}

	mockResults := []*DAGResult{
		{NodeID: "a", Index: 0, Success: true, Data: json.RawMessage(`{"residual":0.01}`)},
		{NodeID: "b", Index: 1, Success: true, Data: json.RawMessage(`{"residual":0.02}`)},
	}
	for _, r := range mockResults {
		if !spec.OnPartial(r) {
			t.Error("OnPartial should return true")
		}
	}
	if len(partials) != 2 {
		t.Errorf("OnPartial called %d times, want 2", len(partials))
	}

	finalData, err := spec.Finalize(partials)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if finalData == nil {
		t.Error("Finalize returned nil")
	}
}

// ── NewDAGEngine Tests ────────────────────────────────────────────────────────────

func TestNewDAGEngine(t *testing.T) {
	sched := New(nil, "test-engine")
	engine := NewDAGEngine(sched, "test-engine")

	if engine == nil {
		t.Fatal("NewDAGEngine returned nil")
	}
	if engine.sched != sched {
		t.Error("engine sched mismatch")
	}
	if engine.nodeID != "test-engine" {
		t.Errorf("engine.nodeID = %s, want test-engine", engine.nodeID)
	}
}

func TestDAGSpec_OnPartialEarlyAbort(t *testing.T) {
	var partials []*DAGResult

	spec := DAGSpec{
		OnPartial: func(result *DAGResult) bool {
			partials = append(partials, result)
			return len(partials) < 2
		},
		Finalize: func(results []*DAGResult) (json.RawMessage, error) {
			return json.RawMessage(`{"aborted_early":true}`), nil
		},
	}

	mockResults := []*DAGResult{
		{NodeID: "a", Success: true},
		{NodeID: "b", Success: true},
		{NodeID: "c", Success: true},
	}
	for _, r := range mockResults {
		if !spec.OnPartial(r) {
			break
		}
	}
	if len(partials) != 2 {
		t.Errorf("OnPartial should have been called 2 times before abort, got %d", len(partials))
	}

	finalData, err := spec.Finalize(partials)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if string(finalData) != `{"aborted_early":true}` {
		t.Errorf("Finalize = %s, want aborted_early", string(finalData))
	}
}

func TestSubmitDAG_NoPeers(t *testing.T) {
	ledger := newMockLedger()
	node := p2p.NewNode(ledger)
	sched := New(node, "test-s")
	engine := NewDAGEngine(sched, "test-s")

	task := &DAGTask{ID: "test-dag", Credits: 10, Timeout: 30}
	spec := DAGSpec{
		Build: func(task *DAGTask) ([]*DAGNode, error) {
			return []*DAGNode{
				{ID: "a", Index: 0, Parents: []string{}, Credits: 10, Timeout: 30},
			}, nil
		},
		Finalize: func(results []*DAGResult) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := engine.SubmitDAG(ctx, task, spec)
	if err == nil {
		t.Error("SubmitDAG should fail without peers")
	}
}

// ── mergeJSON Tests ───────────────────────────────────────────────────────────────

func TestMergeJSON(t *testing.T) {
	a := json.RawMessage(`{"type":"cfd_chunk","i_start":0}`)
	b := json.RawMessage(`{"ghost_regions":[{}]}`)

	merged, err := mergeJSON(a, b)
	if err != nil {
		t.Fatalf("mergeJSON: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}

	if m["type"] != "cfd_chunk" {
		t.Errorf("merged type = %v, want cfd_chunk", m["type"])
	}
	if _, ok := m["ghost_regions"]; !ok {
		t.Error("merged should contain ghost_regions")
	}
	if _, ok := m["i_start"]; !ok {
		t.Error("merged should contain i_start")
	}
}

// ── Helper ────────────────────────────────────────────────────────────────────────

func levelIDs(level []*DAGNode) []string {
	ids := make([]string, len(level))
	for i, n := range level {
		ids[i] = n.ID
	}
	return ids
}
