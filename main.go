package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"time"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
)

// TODO: handle errors centrally.

type emailConfig struct {
	from     string
	host     string
	password string
	port     string
	user     string
}

func main() {
	templates, err := initTemplates()
	if err != nil {
		log.Fatalf("Unable to initialize templates. Error: %s", err.Error())
	}
	config := emailConfig{
		from:     os.Getenv("SMTP_FROM"),
		host:     os.Getenv("SMTP_HOST"),
		password: os.Getenv("SMTP_PASS"),
		port:     os.Getenv("SMTP_PORT"),
		user:     os.Getenv("SMTP_USER"),
	}
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
	mailer := InitSMTPMailer(config)
	apps, buildpacks := getAppsAndBuildpacks(client)
	outdatedApps := findOutdatedApps(apps, buildpacks)
	owners := findOwnersOfApps(outdatedApps, client)
	log.Printf("Will notify %d owners of outdated apps.\n", len(owners))
	sendNotifyEmailToUsers(owners, templates, mailer)
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

type cfSpaceCache struct {
	spaceUsers map[string][]cfclient.User
}

func createCFSpaceCache() *cfSpaceCache {
	return &cfSpaceCache{
		spaceUsers: make(map[string][]cfclient.User),
	}
}

func filterForValidEmailUsernames(users []cfclient.User, app cfclient.App) []cfclient.User {
	var filteredUsers []cfclient.User
	for _, user := range users {
		if _, err := mail.ParseAddress(user.Username); err == nil {
			filteredUsers = append(filteredUsers, user)
		} else {
			log.Printf("Dropping notification to user %s about app %s in space %s because "+
				"invalid e-mail address\n", user.Username, app.Name, app.SpaceGuid)
		}
	}
	return filteredUsers
}

func (c *cfSpaceCache) getOwnersInAppSpace(app cfclient.App, client *cfclient.Client) []cfclient.User {
	var ok bool
	var usersWithSpaceRoles []cfclient.User
	if usersWithSpaceRoles, ok = c.spaceUsers[app.SpaceGuid]; ok {
		return usersWithSpaceRoles
	}
	space, err := app.Space()
	if err != nil {
		log.Fatalf("Unable to get space of app %s. Error: %s", app.Name, err.Error())
	}
	spaceDevelopers, err := listUsersWithSpaceRole(client, space.Guid, SpaceDevelopers)
	if err != nil {
		log.Fatalf("Unable to get space developers of app %s. Error: %s", app.Name, err.Error())
	}
	filteredSpaceDevelopers := filterForValidEmailUsernames(spaceDevelopers, app)
	spaceManagers, err := listUsersWithSpaceRole(client, space.Guid, SpaceManagers)
	if err != nil {
		log.Fatalf("Unable to get space manager of app %s. Error: %s", app.Name, err.Error())
	}
	filteredSpaceManagers := filterForValidEmailUsernames(spaceManagers, app)

	usersWithSpaceRoles = append(usersWithSpaceRoles, filteredSpaceDevelopers...)
	usersWithSpaceRoles = append(usersWithSpaceRoles, filteredSpaceManagers...)

	return usersWithSpaceRoles
}

func findOwnersOfApps(apps []cfclient.App, client *cfclient.Client) map[string][]cfclient.App {
	// Mapping of users to the apps.
	owners := make(map[string][]cfclient.App)
	spaceCache := createCFSpaceCache()
	for _, app := range apps {
		// Get the space
		ownersWithSpaceRoles := spaceCache.getOwnersInAppSpace(app, client)
		for _, ownerWithSpaceRoles := range ownersWithSpaceRoles {
			owners[ownerWithSpaceRoles.Username] = append(owners[ownerWithSpaceRoles.Username], app)
		}
	}
	return owners
}

func findOutdatedApps(apps []cfclient.App, buildpacks map[string]cfclient.BuildpackResource) (outdatedApps []cfclient.App) {
	for _, app := range apps {
		yes, buildpack := isAppUsingSupportedBuildpack(app, buildpacks)
		if !yes {
			continue
		}
		// If the app is using a supported buildpack, check if app is using an outdated buildpack.
		if appIsOutdated := isAppUsingOutdatedBuildpack(app, buildpack); !appIsOutdated {
			continue
		}
		outdatedApps = append(outdatedApps, app)
	}
	return
}

func spaceUserHasRoles(user cfclient.SpaceRole, roles ...string) bool {
	for _, roleOfUser := range user.SpaceRoles {
		for _, role := range roles {
			if role == roleOfUser {
				return true
			}
		}
	}
	return false
}

func sendNotifyEmailToUsers(users map[string][]cfclient.App, templates *Templates, mailer Mailer) {
	for user, apps := range users {
		// Create buffer
		body := new(bytes.Buffer)
		// Fill buffer with completed e-mail
		templates.getNotifyEmail(body, notifyEmail{user, apps})
		// Send email
		appNoun := "application"
		if len(apps) > 1 {
			appNoun = "applications"
		}
		err := mailer.SendEmail(user, fmt.Sprintf("Please restage your %s", appNoun), body.Bytes())
		if err != nil {
			log.Printf("Unable to send e-mail to %s\n", user)
			continue
		}
		fmt.Printf("Sent e-mail to %s\n", user)
	}
}
