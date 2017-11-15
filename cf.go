package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"

	"github.com/cloudfoundry-community/go-cfclient"
	"github.com/pkg/errors"
)

// App represents the V3 API JSON object of an app
// http://v3-apidocs.cloudfoundry.org/version/3.34.0/index.html#the-app-object
type App struct {
	GUID      string `json:"guid"`
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Lifecycle struct {
		Type string `json:"type"`
		Data struct {
			Buildpacks []string `json:"buildpacks,omitempty"`
			Stack      string   `json:"stack,omitempty"`
		} `json:"data,omitempty"`
	} `json:"lifecycle"`
}

// AppResponse represents the V3 API JSON Response when querying for apps.
type AppResponse struct {
	Pagination struct {
		TotalResults int `json:"total_results"`
		TotalPages   int `json:"total_pages"`
		First        struct {
			Href string `json:"href"`
		} `json:"first"`
		Last struct {
			Href string `json:"href"`
		} `json:"last"`
		Next struct {
			Href string `json:"href,omitempty"`
		} `json:"next,omitempty"`
		Previous struct {
			Href string `json:"href,omitempty"`
		} `json:"previous,omitempty"`
	} `json:"pagination"`
	Apps []App `json:"resources"`
}

// Droplet represents the V3 API JSON object of a droplet
// http://v3-apidocs.cloudfoundry.org/version/3.34.0/index.html#the-app-object
type Droplet struct {
	GUID       string `json:"guid"`
	State      string `json:"state"`
	Error      string `json:"error"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	Buildpacks []struct {
		Name         string `json:"name"`
		DetectOutput string `json:"detect_output"`
	} `json:"buildpacks,omitempty"`
}

// DropletResponse represents the V3 API JSON Response when querying for droplets.
type DropletResponse struct {
	Pagination struct {
		TotalResults int `json:"total_results"`
		TotalPages   int `json:"total_pages"`
		First        struct {
			Href string `json:"href"`
		} `json:"first"`
		Last struct {
			Href string `json:"href"`
		} `json:"last"`
		Next struct {
			Href string `json:"href,omitempty"`
		} `json:"next,omitempty"`
		Previous struct {
			Href string `json:"href,omitempty"`
		} `json:"previous,omitempty"`
	} `json:"pagination"`
	Droplets []Droplet `json:"resources"`
}

// ListApps will query for all V3 App objects
// http://v3-apidocs.cloudfoundry.org/version/3.34.0/index.html#list-apps
func ListApps(c *cfclient.Client) ([]App, error) {
	apps := []App{}
	requestURL := "/v3/apps"
	for {
		var appResp AppResponse
		r := c.NewRequest("GET", requestURL)
		resp, err := c.DoRequest(r)
		if err != nil {
			return nil, errors.Wrap(err, "Error requesting apps")
		}
		defer resp.Body.Close()
		resBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "Error reading app response")
		}

		err = json.Unmarshal(resBody, &appResp)
		if err != nil {
			return nil, errors.Wrap(err, "Error unmarshalling app")
		}

		for _, app := range appResp.Apps {
			apps = append(apps, app)
		}

		requestHref := appResp.Pagination.Next.Href
		if requestHref == "" {
			break
		}
		u, err := url.Parse(requestHref)
		if err != nil {
			break
		}
		requestURL = u.RequestURI()
		if requestURL == "" {
			break
		}

	}
	return apps, nil
}

// GetDropletsByQuery will query for droplets using the passed in query parameters
// http://v3-apidocs.cloudfoundry.org/version/3.34.0/index.html#list-droplets
func (a *App) GetDropletsByQuery(c *cfclient.Client, query url.Values) ([]Droplet, error) {
	var droplets []Droplet
	requestURL := fmt.Sprintf("/v3/apps/%s/droplets?%s", a.GUID, query.Encode())
	for {
		var dropletResp DropletResponse
		r := c.NewRequest("GET", requestURL)
		resp, err := c.DoRequest(r)
		if err != nil {
			return nil, errors.Wrap(err, "Error requesting droplets")
		}
		defer resp.Body.Close()
		resBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "Error reading droplet response")
		}

		err = json.Unmarshal(resBody, &dropletResp)
		if err != nil {
			return nil, errors.Wrap(err, "Error unmarshalling droplets")
		}

		for _, droplet := range dropletResp.Droplets {
			droplets = append(droplets, droplet)
		}

		requestHref := dropletResp.Pagination.Next.Href
		if requestHref == "" {
			break
		}
		u, err := url.Parse(requestHref)
		if err != nil {
			break
		}
		requestURL = u.RequestURI()
		if requestURL == "" {
			break
		}

	}
	return droplets, nil
}
