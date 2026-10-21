package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"time"

	"github.com/cloudfoundry/go-cfclient/v3/resource"
)

type buildpackRecord struct {
	LastUpdatedAt time.Time
}

func loadState(path string) (map[string]buildpackRecord, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			log.Printf("Error closing load state at path %s: %s", path, err)
		}
	}()
	decoder := json.NewDecoder(fp)
	var state map[string]buildpackRecord
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	return state, nil
}

func copyState(inPath, outPath string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := in.Close(); err != nil {
			log.Printf("Error closing input state path %s: %s", inPath, err)
		}
	}()

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := out.Close(); err != nil {
			log.Printf("Error closing output state path %s: %s", outPath, err)
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

func saveState(state map[string]buildpackRecord, path string) error {
	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			log.Printf("Error closing state path %s: %s", path, err)
		}
	}()
	encoder := json.NewEncoder(fp)
	return encoder.Encode(state)
}

func updateBuildpackState(
	buildpacks []*resource.Buildpack,
	state map[string]buildpackRecord,
) map[string]buildpackRecord {
	// For each buildpack:
	// 1) If its GUID isn't in state -> it's new, keep it and record its UpdatedAt.
	// 2) If it is in state and UpdatedAt is newer than what we stored, keep it and update state.
	// 3) Otherwise it's unchanged, skip it.

	for _, buildpack := range buildpacks {
		storedBuildpack, found := state[buildpack.GUID]

		if !found || buildpack.UpdatedAt.After(storedBuildpack.LastUpdatedAt) {
			state[buildpack.GUID] = buildpackRecord{LastUpdatedAt: buildpack.UpdatedAt}
			continue
		}

		log.Printf("Supported Buildpack %s has not been updated\n", buildpack.Name)
	}

	return state
}
