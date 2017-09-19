package main

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"testing"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
)

func TestGetNotifyEmail(t *testing.T) {
	rootDataPath := filepath.Join("testdata", "mail", "notify")
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
			}}, "application"},
			filepath.Join(rootDataPath, "single_app.html"),
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
			expectedBody, err := ioutil.ReadFile(tc.expectedEmail)
			if err != nil {
				t.Fatalf("Unable to read expected file. %s", err.Error())
			}
			if string(expectedBody) != string(body.Bytes()) {
				t.Errorf("Test %s failed. For the actual output, inspect %s.returned.", tc.name, filepath.Base(tc.expectedEmail))
				ioutil.WriteFile(filepath.Join(rootDataPath, filepath.Base(tc.expectedEmail)+".returned"), body.Bytes(), 0444)
			}
		})
	}
}
