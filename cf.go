package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/pkg/errors"
)

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

const (
	SpaceDevelopers = "developers"
	SpaceManagers   = "managers"
)

func getUserResponse(requestURL string, c *cfclient.Client) (cfclient.UserResponse, error) {
	var userResp cfclient.UserResponse
	r := c.NewRequest("GET", requestURL)
	resp, err := c.DoRequest(r)
	if err != nil {
		return cfclient.UserResponse{}, errors.Wrap(err, "Error requesting users")
	}
	resBody, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return cfclient.UserResponse{}, errors.Wrap(err, "Error reading user request")
	}
	err = json.Unmarshal(resBody, &userResp)
	if err != nil {
		return cfclient.UserResponse{}, errors.Wrap(err, "Error unmarshalling user")
	}
	return userResp, nil
}

func listUsersWithSpaceRole(c *cfclient.Client, spaceGUID string, role string) ([]cfclient.User, error) {
	var users []cfclient.User
	requestURL := fmt.Sprintf("/v2/spaces/%s/%s", spaceGUID, role)
	for {
		userResp, err := getUserResponse(requestURL, c)
		if err != nil {
			return []cfclient.User{}, err
		}
		for _, user := range userResp.Resources {
			user.Entity.Guid = user.Meta.Guid
			users = append(users, user.Entity)
		}
		requestURL = userResp.NextUrl
		if requestURL == "" {
			break
		}
	}
	return users, nil
}
