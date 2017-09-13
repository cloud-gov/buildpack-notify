package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"time"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	cfenv "github.com/cloudfoundry-community/go-cfenv"
)

// TODO: handle errors centrally.

const (
	cfUPSCreds    = "notify-cf-creds"
	emailUPSCreds = "notify-email-creds"
)

type emailConfig struct {
	from     string
	host     string
	password string
	port     string
	user     string
}

type cfConfig struct {
	api          string
	clientID     string
	clientSecret string
}

func getCFConfig(cfEnv *cfenv.App) cfConfig {
	config := cfConfig{
		api:          os.Getenv("CF_API"),
		clientID:     os.Getenv("CLIENT_ID"),
		clientSecret: os.Getenv("CLIENT_SECRET"),
	}
	if cfEnv != nil {
		if service, err := cfEnv.Services.WithName(cfUPSCreds); err == nil {
			log.Println("Using UPS for CF creds")
			if api, found := service.Credentials["CF_API"]; found {
				config.api = api.(string)
			}
			if clientID, found := service.Credentials["CLIENT_ID"]; found {
				config.clientID = clientID.(string)
			}
			if clientSecret, found := service.Credentials["CLIENT_SECRET"]; found {
				config.clientSecret = clientSecret.(string)
			}
		}
	}
	return config
}

func getEmailConfig(cfEnv *cfenv.App) emailConfig {
	config := emailConfig{
		from:     os.Getenv("SMTP_FROM"),
		host:     os.Getenv("SMTP_HOST"),
		password: os.Getenv("SMTP_PASS"),
		port:     os.Getenv("SMTP_PORT"),
		user:     os.Getenv("SMTP_USER"),
	}
	if cfEnv != nil {
		if service, err := cfEnv.Services.WithName(emailUPSCreds); err == nil {
			log.Println("Using UPS for email creds")
			if smtpFrom, found := service.Credentials["SMTP_FROM"]; found {
				config.from = smtpFrom.(string)
			}
			if smtpHost, found := service.Credentials["SMTP_HOST"]; found {
				config.host = smtpHost.(string)
			}
			if smtpPass, found := service.Credentials["SMTP_PASS"]; found {
				config.password = smtpPass.(string)
			}
			if smtpPort, found := service.Credentials["SMTP_PORT"]; found {
				config.port = smtpPort.(string)
			}
			if smtpUser, found := service.Credentials["SMTP_USER"]; found {
				config.user = smtpUser.(string)
			}
		}
	}
	return config
}

func main() {
	cfEnv, err := cfenv.Current()
	if err != nil {
		log.Println("Could not find cf env")
	}
	config := getEmailConfig(cfEnv)
	cfAPIConfig := getCFConfig(cfEnv)
	templates, err := initTemplates()
	if err != nil {
		log.Fatalf("Unable to initialize templates. Error: %s", err.Error())
	}
	client, err := cfclient.NewClient(&cfclient.Config{
		ApiAddress:        cfAPIConfig.api,
		ClientID:          cfAPIConfig.clientID,
		ClientSecret:      cfAPIConfig.clientSecret,
		SkipSslValidation: os.Getenv("INSECURE") == "1",
		HttpClient:        &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		log.Fatalf("Unable to create client. Error: %s", err.Error())
	}
	notify := flag.Bool("notify", false, "run notification program")
	flag.Parse()
	if *notify {
		log.Println("Calculating notifications to send for outdated buildpacks.")
		mailer := InitSMTPMailer(config)
		apps, buildpacks := getAppsAndBuildpacks(client)
		outdatedApps := findOutdatedApps(apps, buildpacks)
		owners := findOwnersOfApps(outdatedApps, client)
		log.Printf("Will notify %d owners of outdated apps.\n", len(owners))
		sendNotifyEmailToUsers(owners, templates, mailer)
	} else {
		log.Println("Starting notification server.")
		err := http.ListenAndServe(":"+os.Getenv("PORT"), nil)
		if err != nil {
			log.Fatalf("Unable to start server. Error %s", err.Error())
		}
	}
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
		log.Fatalf("Unable to parse last restage time. App %s App GUID %s Error %s",
			app.Name, app.Guid, err)
	}
	timeOfLastBuildpackUpdate, err := time.Parse(time.RFC3339, buildpack.Meta.UpdatedAt)
	if err != nil {
		log.Fatalf("Unable to parse last buildpack update time. Buildpack %s Buildpack GUID %s Error %s",
			buildpack.Entity.Name, buildpack.Meta.Guid, err)
	}
	return timeOfLastBuildpackUpdate.After(timeOfLastAppRestage)
}

type cfSpaceCache struct {
	spaceUsers map[string]map[string]cfclient.User
}

func createCFSpaceCache() *cfSpaceCache {
	return &cfSpaceCache{
		spaceUsers: make(map[string]map[string]cfclient.User),
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

func (c *cfSpaceCache) getOwnersInAppSpace(app cfclient.App, client *cfclient.Client) map[string]cfclient.User {
	var ok bool
	var usersWithSpaceRoles map[string]cfclient.User
	if usersWithSpaceRoles, ok = c.spaceUsers[app.SpaceGuid]; ok {
		return usersWithSpaceRoles
	}
	usersWithSpaceRoles = make(map[string]cfclient.User)
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

	for _, filteredUser := range filteredSpaceDevelopers {
		if _, found := usersWithSpaceRoles[filteredUser.Guid]; !found {
			usersWithSpaceRoles[filteredUser.Guid] = filteredUser
		}
	}

	for _, filteredUser := range filteredSpaceManagers {
		if _, found := usersWithSpaceRoles[filteredUser.Guid]; !found {
			usersWithSpaceRoles[filteredUser.Guid] = filteredUser
		}
	}
	c.spaceUsers[app.SpaceGuid] = usersWithSpaceRoles

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
		if app.State != "STARTED" {
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
		_ = appNoun
		// err := mailer.SendEmail(user, fmt.Sprintf("Please restage your %s", appNoun), body.Bytes())
		// if err != nil {
		// 	log.Printf("Unable to send e-mail to %s\n", user)
		// 	continue
		// }
		fmt.Printf("Sent e-mail to %s\n", user)
	}
}
