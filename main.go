package main

import (
	"log"
	"net/http"
	"os"
	"time"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
)

// TODO: handle errors centrally.

func main() {
	client, err := cfclient.NewClient(&cfclient.Config{
		ApiAddress:        os.Getenv("CF_API"),
		ClientID:          os.Getenv("CLIENT_ID"),
		ClientSecret:      os.Getenv("CLIENT_SECRET"),
		SkipSslValidation: os.Getenv("INSECURE") == "1",
		HttpClient:        &http.Client{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("Unable to create client. Error: %s", err.Error())
	}
	apps, buildpacks := getAppsAndBuildpacks(client)
	findOutdatedApps(apps, buildpacks)
}

func getAppsAndBuildpacks(client *cfclient.Client) ([]cfclient.App, map[string]cfclient.Buildpack) {
	apps, err := client.ListApps()
	if err != nil {
		log.Fatalf("Unable to get apps. Error: %s", err.Error())
	}
	buildpackList, err := client.ListBuildpacks()
	if err != nil {
		log.Fatalf("Unable to get buildpacks. Error: %s", err)
	}
	// Create a map with the key being the buildpack name for quick comparison later on.
	// Buildpack names are unique so that can be a key.
	buildpacks := make(map[string]cfclient.Buildpack)
	for _, buildpack := range buildpackList {
		buildpacks[buildpack.Name] = buildpack
	}
	return apps, buildpacks
}

func isAppUsingSupportedBuildpack(app cfclient.App, buildpacks map[string]cfclient.Buildpack) (bool, *cfclient.Buildpack) {
	if buildpack, found := buildpacks[app.Buildpack]; found && app.Buildpack != "" {
		return true, &buildpack
	}
	// Check the "detected_buildpack" JSON field because that's what the CF API populates (instead of
	// "buildpack") if the app doesn't specify a buildpack in its manifest.
	if buildpack, found := buildpacks[app.DetectedBuildpack]; found && app.DetectedBuildpack != "" {
		return true, &buildpack
	}
	return false, nil
}

func findOutdatedApps(apps []cfclient.App, buildpacks map[string]cfclient.Buildpack) {
	for _, app := range apps {
		yes, _ := isAppUsingSupportedBuildpack(app, buildpacks)
		if !yes {
			continue
		}
	}
}
