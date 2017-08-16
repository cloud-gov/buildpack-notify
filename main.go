package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/pkg/errors"
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

func listBuildpacks(c *cfclient.Client) ([]cfclient.BuildpackResource, error) {
	var buildpacks []cfclient.BuildpackResource
	requestURL := "/v2/buildpacks"
	for {
		buildpackResp, err := getBuildpackResponse(requestURL, c)
		if err != nil {
			return []cfclient.BuildpackResource{}, err
		}
		for _, buildpack := range buildpackResp.Resources {
			buildpack.Entity.Guid = buildpack.Meta.Guid
			buildpacks = append(buildpacks, buildpack)
		}

		requestURL = buildpackResp.NextUrl
		if requestURL == "" {
			break
		}
	}
	return buildpacks, nil
}

// Copied from github.com/cloudfoundry-community/go-cfclient/buildpacks.go for use in listBuildpacks.
// Without this, we wouldn't have access to the necessary metadata (buildpack.Meta).
func getBuildpackResponse(requestURL string, c *cfclient.Client) (cfclient.BuildpackResponse, error) {
	var buildpackResp cfclient.BuildpackResponse
	r := c.NewRequest("GET", requestURL)
	resp, err := c.DoRequest(r)
	if err != nil {
		return cfclient.BuildpackResponse{}, errors.Wrap(err, "Error requesting buildpacks")
	}
	resBody, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return cfclient.BuildpackResponse{}, errors.Wrap(err, "Error reading buildpack request")
	}
	err = json.Unmarshal(resBody, &buildpackResp)
	if err != nil {
		return cfclient.BuildpackResponse{}, errors.Wrap(err, "Error unmarshalling buildpack")
	}
	return buildpackResp, nil
}

func getAppsAndBuildpacks(client *cfclient.Client) ([]cfclient.App, map[string]cfclient.BuildpackResource) {
	apps, err := client.ListApps()
	if err != nil {
		log.Fatalf("Unable to get apps. Error: %s", err.Error())
	}
	buildpackList, err := listBuildpacks(client)
	if err != nil {
		log.Fatalf("Unable to get buildpacks. Error: %s", err)
	}

	// Create a map with the key being the buildpack name for quick comparison later on.
	// Buildpack names are unique so that can be a key.
	buildpacks := make(map[string]cfclient.BuildpackResource)
	for _, buildpack := range buildpackList {
		buildpacks[buildpack.Entity.Name] = buildpack
	}
	return apps, buildpacks
}

func isAppUsingSupportedBuildpack(app cfclient.App, buildpacks map[string]cfclient.BuildpackResource) (bool, *cfclient.BuildpackResource) {
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

func isAppUsingOutdatedBuildpack(app cfclient.App, buildpack *cfclient.BuildpackResource) bool {
	// 2016-06-08T16:41:45Z
	timeOfLastAppRestage, err := time.Parse(time.RFC3339, app.PackageUpdatedAt)
	if err != nil {
		log.Fatal(err)
	}
	timeOfLastBuildpackUpdate, err := time.Parse(time.RFC3339, buildpack.Meta.UpdatedAt)
	if err != nil {
		log.Fatal(err)
	}
	return timeOfLastBuildpackUpdate.After(timeOfLastAppRestage)
}

func findOutdatedApps(apps []cfclient.App, buildpacks map[string]cfclient.BuildpackResource) {
	for _, app := range apps {
		yes, buildpack := isAppUsingSupportedBuildpack(app, buildpacks)
		if !yes {
			continue
		}
		// If the app is using a supported buildpack, check if app is using an outdated buildpack.
		appIsOutdated := isAppUsingOutdatedBuildpack(app, buildpack)
		if appIsOutdated {
			// Get the list of space managers and space developers for the app.
		}
	}
}
