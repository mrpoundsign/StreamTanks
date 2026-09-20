package scenariolab

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScenarioLabServer manages the HTTP and REST API endpoints for scenario testing and visual replay.
type ScenarioLabServer struct {
	ScenariosDir string
	Mux          *http.ServeMux
}

// NewScenarioLabServer creates a configured server instance for cmd/scenariolab.
func NewScenarioLabServer(scenariosDir string, publicLabDir string) *ScenarioLabServer {
	if scenariosDir == "" {
		scenariosDir = "scenarios"
	}
	_ = os.MkdirAll(scenariosDir, 0o755)

	mux := http.NewServeMux()
	s := &ScenarioLabServer{
		ScenariosDir: scenariosDir,
		Mux:          mux,
	}

	// REST API Routes
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/scenarios/list", s.handleListScenarios)
	mux.HandleFunc("/api/scenarios/load", s.handleLoadScenario)
	mux.HandleFunc("/api/scenarios/generate", s.handleGenerateScenarios)
	mux.HandleFunc("/api/scenarios/run", s.handleRunScenario)
	mux.HandleFunc("/api/scenarios/save", s.handleSaveScenario)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Static asset server for lab UI
	if publicLabDir != "" {
		fileHandler := http.FileServer(http.Dir(publicLabDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			fileHandler.ServeHTTP(w, r)
		})
	}

	return s
}

func (s *ScenarioLabServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *ScenarioLabServer) handleListScenarios(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type ScenarioItem struct {
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		Description      string  `json:"description,omitempty"`
		Source           string  `json:"source"` // "builtin" or "saved"
		Filename         string  `json:"filename,omitempty"`
		MaxTerrainDiff   float64 `json:"maxTerrainDiff"`
		MaxPositionDiff  float64 `json:"maxPositionDiff"`
		MaxPixelDiff     float64 `json:"maxPixelDiff"`
		TerrainDiffCount int     `json:"terrainDiffCount"`
		HasDiscrepancy   bool    `json:"hasDiscrepancy"`
		Summary          string  `json:"summary,omitempty"`
	}

	var items []ScenarioItem
	for _, sc := range getBuiltInScenarios() {
		items = append(items, ScenarioItem{
			ID:          sc.ID,
			Name:        sc.Name,
			Description: sc.Description,
			Source:      "builtin",
		})
	}

	files, err := os.ReadDir(s.ScenariosDir)
	if err == nil {
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".json") {
				path := filepath.Join(s.ScenariosDir, f.Name())
				sc, diff, err := LoadScenarioWithDiff(path)
				if err == nil && sc != nil {
					maxTerrain := 0.0
					maxPos := 0.0
					diffCount := 0
					hasDisc := false
					summary := ""
					if diff != nil {
						maxTerrain = diff.MaxTerrainDiff
						maxPos = diff.MaxPositionDiff
						diffCount = diff.TerrainDiffCount
						hasDisc = diff.HasDiscrepancy
						summary = diff.Summary
					}
					maxPixel := math.Max(maxTerrain, maxPos)
					items = append(items, ScenarioItem{
						ID:               sc.ID,
						Name:             sc.Name,
						Description:      sc.Description,
						Source:           "saved",
						Filename:         f.Name(),
						MaxTerrainDiff:   maxTerrain,
						MaxPositionDiff:  maxPos,
						MaxPixelDiff:     maxPixel,
						TerrainDiffCount: diffCount,
						HasDiscrepancy:   hasDisc,
						Summary:          summary,
					})
				}
			}
		}
	}

	sortMode := r.URL.Query().Get("sort")
	if sortMode == "name" {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
	} else if sortMode == "source" {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Source != items[j].Source {
				return items[i].Source < items[j].Source
			}
			return items[i].Name < items[j].Name
		})
	} else {
		// Default: sort by MaxPixelDiff descending (highest pixel diff first)
		sort.SliceStable(items, func(i, j int) bool {
			diffI := items[i].MaxPixelDiff
			diffJ := items[j].MaxPixelDiff
			if diffI != diffJ {
				return diffI > diffJ
			}
			if items[i].Source != items[j].Source {
				return items[i].Source == "saved"
			}
			return items[i].Name < items[j].Name
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"scenarios": items,
	})
}

func (s *ScenarioLabServer) handleGenerateScenarios(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		Count int   `json:"count"`
		Seed  int64 `json:"seed"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100 {
		req.Count = 100
	}
	if req.Seed == 0 {
		req.Seed = time.Now().UnixNano()
	}

	type GeneratedItem struct {
		Scenario     *Scenario         `json:"scenario"`
		ServerResult *SimulationResult `json:"serverResult"`
	}

	var items []GeneratedItem
	for i := range req.Count {
		curSeed := req.Seed + int64(i)*1337
		sc := GenerateRandomScenario(curSeed)
		res := RunScenarioSimulation(sc, 1.0)
		items = append(items, GeneratedItem{
			Scenario:     sc,
			ServerResult: res,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"scenarios": items,
	})
}

func (s *ScenarioLabServer) handleRunScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var sc Scenario
	if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
		http.Error(w, fmt.Sprintf("invalid scenario JSON: %v", err), http.StatusBadRequest)
		return
	}

	dtScale := sc.Rules.PhysicsSpeed
	if dtScale <= 0 {
		dtScale = 1.0
	}

	res := RunScenarioSimulation(&sc, dtScale)
	_ = json.NewEncoder(w).Encode(res)
}

func (s *ScenarioLabServer) handleSaveScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var payload struct {
		Scenario   *Scenario   `json:"scenario"`
		DiffReport *DiffReport `json:"diffReport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if payload.Scenario == nil {
		http.Error(w, "scenario is required", http.StatusBadRequest)
		return
	}

	savedPath, err := SaveScenario(s.ScenariosDir, payload.Scenario, payload.DiffReport)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to save: %v", err), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"path":    savedPath,
	})
}

func (s *ScenarioLabServer) handleLoadScenario(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "scenario id or filename is required", http.StatusBadRequest)
		return
	}

	for _, sc := range getBuiltInScenarios() {
		if sc.ID == id {
			_ = json.NewEncoder(w).Encode(sc)
			return
		}
	}

	targetPath := filepath.Join(s.ScenariosDir, id)
	if !strings.HasSuffix(targetPath, ".json") {
		targetPath += ".json"
	}
	if _, err := os.Stat(targetPath); err != nil {
		targetPath = filepath.Join(s.ScenariosDir, fmt.Sprintf("mismatch_%s.json", id))
	}

	sc, err := LoadScenario(targetPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("scenario not found: %v", err), http.StatusNotFound)
		return
	}

	_ = json.NewEncoder(w).Encode(sc)
}

// StartScenarioLab runs the Scenario Lab web server on the given address.
func StartScenarioLab(addr string, scenariosDir string, publicLabDir string) error {
	if addr == "" {
		addr = ":8105"
	}
	srv := NewScenarioLabServer(scenariosDir, publicLabDir)
	displayURL := addr
	if strings.HasPrefix(displayURL, ":") {
		displayURL = "localhost" + displayURL
	}
	if !strings.HasPrefix(displayURL, "http://") && !strings.HasPrefix(displayURL, "https://") {
		displayURL = "http://" + displayURL
	}

	log.Printf("==================================================")
	log.Printf(" STREAMTANKS SCENARIO LAB & REPLAY RUNNING AT:   ")
	log.Printf(" %s", displayURL)
	log.Printf(" Scenarios Directory: %s", srv.ScenariosDir)
	log.Printf("==================================================")

	return http.ListenAndServe(addr, srv.Mux)
}
