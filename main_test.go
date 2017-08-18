package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
)

func TestSpaceUserHasRoles(t *testing.T) {
	testCases := []struct {
		name         string
		rolesToCheck []string
		spaceUser    cfclient.SpaceRole
		expected     bool
	}{
		{"role there", []string{"test"}, cfclient.SpaceRole{SpaceRoles: []string{"test"}}, true},
		{"role not there", []string{"test"}, cfclient.SpaceRole{SpaceRoles: []string{""}}, false},
		{"multiple roles not there", []string{"test", "test2"}, cfclient.SpaceRole{SpaceRoles: []string{"foo"}}, false},
		{"multiple roles there", []string{"test", "test2"}, cfclient.SpaceRole{SpaceRoles: []string{"test2", "test"}}, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if ret := spaceUserHasRoles(tc.spaceUser, tc.rolesToCheck...); ret != tc.expected {
				t.Errorf("Test %s failed. Expected %v Actual %v\n", tc.name, tc.expected, ret)
			}
		})
	}
}

type spaceSpec struct {
	space      cfclient.SpaceResource
	spaceRoles cfclient.SpaceRoleResponse
}

func TestFindOwnersOfApps(t *testing.T) {
	testCases := []struct {
		name     string
		apps     cfclient.AppResponse
		spaces   map[string]spaceSpec
		expected map[string][]cfclient.App
	}{
		{
			"single app, single user",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager"}}}}},
				},
			},
			map[string][]cfclient.App{"user1": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}}},
		},
		{
			"single app, single user multiple valid roles",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager", "space_developer"}}}}},
				},
			},
			map[string][]cfclient.App{"user1": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}}},
		},
		{
			"single app, single user one valid role, one invalid role",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager", "space_auditor"}}}}},
				},
			},
			map[string][]cfclient.App{"user1": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}}},
		},
		{
			"single app, single user no valid role",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_auditor"}}}}},
				},
			},
			map[string][]cfclient.App{},
		},
		{
			"same single app, multiple users",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager"}}},
						{Entity: cfclient.SpaceRole{Username: "user2", SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				"user1": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}},
				"user2": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}},
			},
		},
		{
			"same single app, multiple users, one without valid role",
			cfclient.AppResponse{Resources: []cfclient.AppResource{{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}}}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_auditor"}}},
						{Entity: cfclient.SpaceRole{Username: "user2", SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				"user2": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}},
			},
		},
		{
			"two apps in different spaces, two users, mutually exclusive app ownership",
			cfclient.AppResponse{Resources: []cfclient.AppResource{
				{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}},
				{Meta: cfclient.Meta{Guid: "app2"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space2"}},
			}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager"}}},
					}},
				},
				"space2": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space2"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Entity: cfclient.SpaceRole{Username: "user2", SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				"user1": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}},
				"user2": []cfclient.App{cfclient.App{Guid: "app2", SpaceURL: "/v2/spaces/space2"}},
			},
		},
		{
			"two apps in different spaces, two users with ownership in both spaces",
			cfclient.AppResponse{Resources: []cfclient.AppResource{
				{Meta: cfclient.Meta{Guid: "app1"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space1"}},
				{Meta: cfclient.Meta{Guid: "app2"}, Entity: cfclient.App{SpaceURL: "/v2/spaces/space2"}},
			}},
			map[string]spaceSpec{
				"space1": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space1"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager"}}},
						{Entity: cfclient.SpaceRole{Username: "user2", SpaceRoles: []string{"space_manager"}}},
					}},
				},
				"space2": {
					cfclient.SpaceResource{Meta: cfclient.Meta{Guid: "space2"}, Entity: cfclient.Space{}},
					cfclient.SpaceRoleResponse{Resources: []cfclient.SpaceRoleResource{
						{Entity: cfclient.SpaceRole{Username: "user1", SpaceRoles: []string{"space_manager"}}},
						{Entity: cfclient.SpaceRole{Username: "user2", SpaceRoles: []string{"space_manager"}}},
					}},
				},
			},
			map[string][]cfclient.App{
				"user1": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}, cfclient.App{Guid: "app2", SpaceURL: "/v2/spaces/space2"}},
				"user2": []cfclient.App{cfclient.App{Guid: "app1", SpaceURL: "/v2/spaces/space1"}, cfclient.App{Guid: "app2", SpaceURL: "/v2/spaces/space2"}},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				encoder := json.NewEncoder(w)
				parts := strings.Split(r.URL.Path, "/")
				if r.URL.Path == "/v2/apps" {
					encoder.Encode(tc.apps)
				} else if strings.HasSuffix(r.URL.Path, "user_roles") {
					encoder.Encode(tc.spaces[parts[len(parts)-2]].spaceRoles)
				} else if len(parts) >= 3 {
					encoder.Encode(tc.spaces[parts[3]].space)
				} else {
					t.Fatalf("Unable to find handler for path %s", r.URL.Path)
				}
			}))
			defer ts.Close()
			c := cfclient.Client{Config: cfclient.Config{HttpClient: http.DefaultClient, ApiAddress: ts.URL}}
			apps, err := c.ListApps()
			if err != nil {
				t.Fatal(err)
			}
			actual := findOwnersOfApps(apps)
			if len(actual) != len(tc.expected) {
				t.Errorf("Test %s failed. Expected %d user entries, only found %d\n", tc.name, len(tc.expected), len(actual))
			}
			for actualUsername, actualOutdatedApps := range actual {
				expectedOutdatedApps, found := tc.expected[actualUsername]
				if !found {
					t.Errorf("Test %s failed. Couldn't find user %s in expected map\n", tc.name, actualUsername)
					continue
				}
				if len(expectedOutdatedApps) != len(actualOutdatedApps) {
					t.Errorf("Test %s failed. Expected %d outdated apps, only found %d\n", tc.name, len(expectedOutdatedApps), len(actualOutdatedApps))
					t.Errorf("Expected %+v\nActual %+v\n", expectedOutdatedApps, actualOutdatedApps)
					continue
				}
				for _, actualOutdatedApp := range actualOutdatedApps {
					found := false
					for _, expectedOutdatedApp := range expectedOutdatedApps {
						if expectedOutdatedApp.Guid == actualOutdatedApp.Guid {
							found = true
						}
					}
					if !found {
						t.Errorf("Test %s failed. Looked for app with guid %s, Could not find it", tc.name, actualOutdatedApp.Guid)
					}
				}
			}
		})
	}
}
