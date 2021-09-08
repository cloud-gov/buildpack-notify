package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
)

func TestGetNotifyEmail(t *testing.T) {
	rootDataPath := filepath.Join("testdata", "mail", "notify")
	updatedBuildpacksSingleApp := []buildpackReleaseInfo{
		{
			"python_buildpack",
			"v1.7.43",
			"https://github.com/cloudfoundry/python-buildpack/releases/tags/v1.7.43",
		},
	}
	updatedBuildpacksMultipleApps := []buildpackReleaseInfo{
		{
			"python_buildpack",
			"v1.7.43",
			"https://github.com/cloudfoundry/python-buildpack/releases/tags/v1.7.43",
		},
		{
			"ruby_buildpack",
			"v1.8.43",
			"https://github.com/cloudfoundry/ruby-buildpack/releases/tags/v1.8.43",
		},
	}
	testCases := []struct {
		name          string
		email         notifyEmail
		expectedEmail string
	}{
		{
			"single app",
			notifyEmail{"test@example.com", []cfclient.App{{Name: "my-drupal-app",
				SpaceData: cfclient.SpaceResource{Entity: cfclient.Space{Name: "dev",
					OrgData: cfclient.OrgResource{Entity: cfclient.Org{Name: "sandbox"}},
				}},
			}}, false, updatedBuildpacksSingleApp},
			filepath.Join(rootDataPath, "single_app.txt"),
		},
		{
			"multiple apps",
			notifyEmail{"test@example.com", []cfclient.App{
				{Name: "my-drupal-app",
					SpaceData: cfclient.SpaceResource{Entity: cfclient.Space{Name: "dev",
						OrgData: cfclient.OrgResource{Entity: cfclient.Org{Name: "sandbox"}},
					}},
				},
				{Name: "my-wordpress-app",
					SpaceData: cfclient.SpaceResource{Entity: cfclient.Space{Name: "staging",
						OrgData: cfclient.OrgResource{Entity: cfclient.Org{Name: "paid-org"}},
					}},
				},
			}, true, updatedBuildpacksMultipleApps},
			filepath.Join(rootDataPath, "multiple_apps.txt"),
		},
	}
	for _, tc := range testCases {
		templates, err := initTemplates()
		if err != nil {
			t.Fatalf("Unable to init templates. Error %s", err.Error())
		}
		t.Run(tc.name, func(t *testing.T) {
			body := new(bytes.Buffer)
			err := templates.getNotifyEmail(body, tc.email)
			if err != nil {
				t.Errorf("Can't construct final email. Error %s", err.Error())
			}
			if os.Getenv("OVERRIDE_TEMPLATES") == "1" {
				err := ioutil.WriteFile(tc.expectedEmail, body.Bytes(), 0644)
				if err != nil {
					t.Errorf("Can't save expected email. Error %s", err.Error())
				}
			}
			expectedBody, err := ioutil.ReadFile(tc.expectedEmail)
			if err != nil {
				t.Fatalf("Unable to read expected file. %s", err.Error())
			}
			if string(expectedBody) != string(body.Bytes()) {
				t.Logf("\n===========Expected %s e-mail case BEGIN===========\n%s\n===========Expected %s e-mail case END===========\n", tc.name, string(expectedBody), tc.name)
				t.Logf("\n===========Actual %s e-mail case BEGIN===========\n%s\n===========Actual %s e-mail case END===========\n", tc.name, string(body.Bytes()), tc.name)
				t.Errorf("Test %s failed. For the actual output, inspect %s.returned.", tc.name, filepath.Base(tc.expectedEmail))
				ioutil.WriteFile(filepath.Join(rootDataPath, filepath.Base(tc.expectedEmail)+".returned"), body.Bytes(), 0644)
			}
		})
	}
}
