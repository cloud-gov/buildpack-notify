package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"time"

	cfclient "github.com/cloudfoundry/go-cfclient/v3/client"
	cfconfig "github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"github.com/kelseyhightower/envconfig"
)

// TODO: handle errors centrally.

type Config struct {
	InState  string `envconfig:"in_state" required:"true"`
	OutState string `envconfig:"out_state" required:"true"`
	DryRun   bool   `envconfig:"dry_run"`
}

type EmailConfig struct {
	From     string `envconfig:"smtp_from" required:"true"`
	Host     string `envconfig:"smtp_host" required:"true"`
	Password string `envconfig:"smtp_password" required:"true"`
	Port     string `envconfig:"smtp_port" required:"true"`
	User     string `envconfig:"smtp_user" required:"true"`
	Cert     string `envconfig:"smtp_cert"`
}

type CFAPIConfig struct {
	API          string `envconfig:"cf_api" required:"true"`
	ClientID     string `envconfig:"client_id" required:"true"`
	ClientSecret string `envconfig:"client_secret" required:"true"`
}

func main() {
	var (
		config      Config
		emailConfig EmailConfig
		cfAPIConfig CFAPIConfig
	)
	ctx := context.Background()

	if err := envconfig.Process("", &config); err != nil {
		log.Fatalf("Unable to parse config: %s", err.Error())
	}
	if err := envconfig.Process("", &emailConfig); err != nil {
		log.Fatalf("Unable to parse email config: %s", err.Error())
	}
	if err := envconfig.Process("", &cfAPIConfig); err != nil {
		log.Fatalf("Unable to parse cf api config: %s", err.Error())
	}

	if config.DryRun {
		log.Println("Dry-Run mode activated. No modifications happening")
	}

	state, err := loadState(config.InState)
	if err != nil {
		log.Fatalf("Error reading state: %s", err)
	}

	templates, err := initTemplates()
	if err != nil {
		log.Fatalf("Unable to initialize templates: %s", err)
	}

	cfg, err := cfconfig.New(cfAPIConfig.API, cfconfig.ClientCredentials(cfAPIConfig.ClientID, cfAPIConfig.ClientSecret),
		cfconfig.HttpClient(&http.Client{Timeout: 30 * time.Second}))
	if err != nil {
		log.Fatalf("Unable to create config. Error: %s", err.Error())
	}

	client, err := cfclient.New(cfg)
	if err != nil {
		log.Fatalf("Unable to create client. Error: %s", err.Error())
	}

	log.Println("Calculating notifications to send for outdated buildpacks.")
	mailer := InitSMTPMailer(emailConfig)
	apps, buildpacks, state, err := getAppsAndBuildpacks(client, ctx, state)
	if err != nil {
		log.Fatalf("Unable to load apps and buildpacks: %s", err)
	}

	outdatedApps, updatedBuildpacks := findOutdatedApps(ctx, client, apps, buildpacks)
	owners := findOwnersOfApps(ctx, outdatedApps, client)

	log.Printf("Will notify %d owners of outdated apps.\n", len(owners))
	updatedBuildpacks = deduplicateBuildpacks(updatedBuildpacks)
	sendNotifyEmailToUsers(owners, updatedBuildpacks, templates, mailer, config.DryRun)

	if config.DryRun {
		if err := copyState(config.InState, config.OutState); err != nil {
			log.Fatalf("Error copying state: %s", err)
		}
	} else {
		if err := saveState(state, config.OutState); err != nil {
			log.Fatalf("Error saving state: %s", err)
		}
	}
}

func getAppsAndBuildpacks(client *cfclient.Client, ctx context.Context, state map[string]buildpackRecord) ([]*resource.App, map[string]resource.Buildpack, map[string]buildpackRecord, error) {
	apps, err := client.Applications.ListAll(ctx, nil)
	if err != nil {
		return nil, nil, state, err
	}
	// Get all the buildpacks from our CF deployment via CF_API.
	buildpackList, err := client.Buildpacks.ListAll(ctx, nil)
	if err != nil {
		return nil, nil, state, err
	}
	state = updateBuildpackState(buildpackList, state)

	// Create a map with the key being the buildpack name for quick comparison later on.
	buildpacks := make(map[string]resource.Buildpack)
	for _, buildpack := range buildpackList {
		buildpacks[buildpack.Name] = *buildpack
	}
	return apps, buildpacks, state, nil
}

// filterForValidEmailUsernames drops any users whose username is not a valid
// e-mail address, since notifications are delivered by e-mail.
func filterForValidEmailUsernames(users []*resource.User, app *resource.App) []*resource.User {
	var filteredUsers []*resource.User
	for _, user := range users {
		if user.Username == nil {
			log.Printf("Dropping notification to user %s about app %s in space %s because missing e-mail address\n",
				user.GUID, app.Name, app.Relationships.Space.Data.GUID)
			continue
		}
		if _, err := mail.ParseAddress(*user.Username); err != nil {
			log.Printf("Dropping notification to user %s about app %s in space %s because "+
				"invalid e-mail address\n", *user.Username, app.Name, app.Relationships.Space.Data.GUID)
		} else {
			filteredUsers = append(filteredUsers, user)
		}
	}
	return filteredUsers
}

func findOutdatedApps(ctx context.Context, client *cfclient.Client, apps []*resource.App, systemBuildpacks map[string]resource.Buildpack) (outdatedApps []*resource.App, updatedBuildpacks []buildpackReleaseInfo) {
	for _, app := range apps {
		if app.State != "STARTED" {
			log.Printf("App %s guid %s not in STARTED state\n", app.Name, app.GUID)
			continue
		}

		droplet, foundDroplet := getCurrentDropletForApp(ctx, *app, client)
		if !foundDroplet {
			log.Printf("Unable to find current droplet for app %s guid %s. Safely skipping.\n", app.Name, app.GUID)
			continue
		}

		for _, dropletBuildpack := range droplet.Buildpacks {
			supported, supportedBuildpackInfo := isBuildpackSupported(dropletBuildpack, systemBuildpacks)
			if !supported {
				continue
			}

			supportedBuildpack := supportedBuildpackInfo.buildpack

			supportedBuildpackVersion, err := parseBuildpackVersion(*supportedBuildpack.Filename)
			if err != nil {
				log.Printf("Error parsing buildpack version: %s", err)
				continue
			}

			// If the app is using a supported buildpack, check if app is using an outdated buildpack.
			appIsOutdated := isDropletBuildpackOutdated(&dropletBuildpack, supportedBuildpackInfo.source, supportedBuildpackVersion)
			if !appIsOutdated {
				log.Printf("App %s GUID %s | Buildpack %s not outdated\n", app.Name, app.GUID, dropletBuildpack.Name)
				continue
			}

			// If the app is using an outdated buildpack, get the buildpack information to pass along to the user.
			log.Printf("App %s GUID %s | Buildpack %s is outdated\n", app.Name, app.GUID, supportedBuildpack.Name)
			buildpackReleaseURL := getBuildpackReleaseURL(supportedBuildpack.Name)
			if supportedBuildpack.Filename == nil {
				log.Printf("Buildpack %s guid %s missing filename metadata; skipping release info\n", supportedBuildpack.Name, supportedBuildpack.GUID)
				continue
			}

			buildpackVersionURL := getBuildpackVersionURL(buildpackReleaseURL, supportedBuildpackVersion)

			updatedBuildpack := buildpackReleaseInfo{
				BuildpackName:    supportedBuildpack.Name,
				BuildpackVersion: supportedBuildpackVersion,
				BuildpackURL:     buildpackVersionURL,
			}

			updatedBuildpacks = append(updatedBuildpacks, updatedBuildpack)
			outdatedApps = append(outdatedApps, app)
		}
	}
	return
}

func sendNotifyEmailToUsers(users map[string][]notifyApp, updatedBuildpacks []buildpackReleaseInfo, templates *Templates, mailer Mailer, dryRun bool) {
	for user, apps := range users {
		// Create buffer
		body := new(bytes.Buffer)
		// Determine whether the user has one application or more than one.
		isMultipleApp := len(apps) > 1
		// Fill buffer with completed e-mail
		err := templates.getNotifyEmail(body, notifyEmail{user, apps, isMultipleApp, updatedBuildpacks})
		if err != nil {
			log.Printf("Unable to render e-mail for %s: %s\n", user, err)
			continue
		}
		// Send email
		if !dryRun {
			subj := "Action required: restage your application"
			if isMultipleApp {
				subj += "s"
			}
			err := mailer.SendEmail(user, subj, body.Bytes())
			if err != nil {
				log.Printf("Unable to send e-mail to %s\n", user)
				continue
			}
		}
		fmt.Printf("Sent e-mail to %s\n", user)
	}
}
