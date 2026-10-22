package main

import (
	"context"
	"log"

	cfclient "github.com/cloudfoundry/go-cfclient/v3/client"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
)

type cfSpaceCache struct {
	// spaceGUID -> owner users (space_manager/space_developer) with valid-email usernames
	spaceUsers map[string][]*resource.User
	// spaceGUID -> resolved space and org names, used to render the email to notify users.
	spaceNames map[string]spaceOrgNames
}

type spaceOrgNames struct {
	SpaceName string
	OrgName   string
}

// notifyApp is the view-model for a single app referenced in a notify email.
// v3 resource.App does not carry space/org names, so we resolve them and pass
// them explicitly to the template.
type notifyApp struct {
	GUID                      string
	SpaceGUID                 string
	IsUsingPinnedGitBuildpack bool
	PinnedGitBuildpackUrl     string
	SystemBuildpackName       string
	Name                      string
	SpaceName                 string
	OrgName                   string
}

func createCFSpaceCache() *cfSpaceCache {
	return &cfSpaceCache{
		spaceUsers: make(map[string][]*resource.User),
		spaceNames: make(map[string]spaceOrgNames),
	}
}

func getAppOwnerSpaceRoleTypes() []resource.SpaceRoleType {
	return []resource.SpaceRoleType{
		resource.SpaceRoleManager,
		resource.SpaceRoleDeveloper,
	}
}

func (c *cfSpaceCache) getOwnersInAppSpace(
	ctx context.Context,
	notifyApp notifyApp,
	client *cfclient.Client,
) ([]*resource.User, error) {
	spaceGUID := notifyApp.SpaceGUID

	if users, ok := c.spaceUsers[spaceGUID]; ok {
		return users, nil
	}

	opts := cfclient.NewRoleListOptions()
	opts.SpaceGUIDs.EqualTo(spaceGUID)
	// Replaces v2 getAppOwnerRoles() + filterForUsersWithRoles():
	// let CF return only the owner role types.
	opts.WithSpaceRoleType(getAppOwnerSpaceRoleTypes()...)

	_, users, err := client.Roles.ListIncludeUsersAll(ctx, opts)
	if err != nil {
		return nil, err
	}

	ownersWithSpaceRoles := filterForValidEmailUsernames(users, notifyApp)

	c.spaceUsers[spaceGUID] = ownersWithSpaceRoles
	return ownersWithSpaceRoles, nil
}

// getSpaceOrgNames resolves (and caches) the space name and parent org name for
// an app's space. v3 resource.App only carries the space GUID, so we look these
// up to render the "cf target -o ORG -s SPACE" line in the email to notify users.
func (c *cfSpaceCache) getSpaceOrgNames(
	ctx context.Context,
	notifyApp notifyApp,
	client *cfclient.Client,
) (spaceOrgNames, error) {
	spaceGUID := notifyApp.SpaceGUID

	if names, ok := c.spaceNames[spaceGUID]; ok {
		return names, nil
	}

	space, org, err := client.Spaces.GetIncludeOrganization(ctx, spaceGUID)
	if err != nil {
		return spaceOrgNames{}, err
	}

	names := spaceOrgNames{SpaceName: space.Name, OrgName: org.Name}
	c.spaceNames[spaceGUID] = names
	return names, nil
}

func findOwnersOfApps(ctx context.Context, apps []notifyApp, client *cfclient.Client) map[string][]notifyApp {
	// Mapping of users to the apps.
	owners := make(map[string][]notifyApp)
	spaceCache := createCFSpaceCache()
	for _, app := range apps {
		ownersWithSpaceRoles, err := spaceCache.getOwnersInAppSpace(ctx, app, client)
		if err != nil {
			log.Printf("Unable to get owners for app %s guid %s in space %s: %s\n",
				app.Name, app.GUID, app.SpaceGUID, err)
			continue
		}
		if len(ownersWithSpaceRoles) == 0 {
			continue
		}
		names, err := spaceCache.getSpaceOrgNames(ctx, app, client)
		if err != nil {
			log.Printf("Unable to get org/space names for app %s guid %s in space %s: %s\n",
				app.Name, app.GUID, app.SpaceGUID, err)
			continue
		}
		app.SpaceName = names.SpaceName
		app.OrgName = names.OrgName
		for _, ownerWithSpaceRoles := range ownersWithSpaceRoles {
			owners[*ownerWithSpaceRoles.Username] = append(owners[*ownerWithSpaceRoles.Username], app)
		}
	}
	return owners
}

// getCurrentDropletForApp will try to query the current droplet.
// A running app will have 1 droplet associated with it.
// If it doesn't have 1, it's not running.
func getCurrentDropletForApp(
	ctx context.Context,
	app resource.App,
	client *cfclient.Client,
) (*resource.Droplet, bool) {
	droplet, err := client.Droplets.GetCurrentForApp(ctx, app.GUID)
	if err != nil {
		log.Printf(
			"Unable to get current droplet for app. App %s App GUID %s Error %s",
			app.Name,
			app.GUID,
			err,
		)
		return nil, false
	}

	return droplet, true
}
