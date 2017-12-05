# cg-buildpack-notify

Encouraging cloud foundry customers to restage their apps so they can benefit from buildpack updates.

---

## Table of Contents

- [Deployment](#deployment)
- [Architecture](#architecture)
- [Development](#development)

---

## Deployment

This application is deployed to Cloud Foundry and requires certain
services to be available so that the application can bind to them.

Additionally, this repository contains a Concourse pipeline for deploying
the application and requires credentials.

Follow the 2 step instructions below to prepare your environment for deployment.

### Step 1: Setup Services for Cloud Foundry Application

#### Database Setup

This service requires a PostgreSQL database. The service name should be `buildpack-notify-db`
and should be in the same space that you will deploy the application.

For example, if using the [aws-broker](https://github.com/18F/aws-broker),
run: `cf cs aws-rds shared-psql buildpack-notify-db`

#### Credentials

Email:
- `SMTP_FROM`: The email-address that will be in the From field in the e-mail. e.g. `test@example.com` or `Me <test@example.com>`
- `SMTP_HOST`: The SMTP host to authenticate with. e.g. `smtp.host.com`
- `SMTP_PASS`: The password to authenticate with. e.g. `somepassword`
- `SMTP_PORT`: The SMTP port to connect to. e.g. `587`
- `SMTP_USER`: The username to authenticate with. e.g. `someuser@example.com`

CF API:
- `CF_API`: "https://api.local.pcfdev.io",
- `CLIENT_ID`: "client-id-here",
- `CLIENT_SECRET`: "client-secret-here"

The client mentioned above should be created with the following attributes:
- `authorities`: `cloud_controller.admin_read_only`
- `authorized_grant_types`: `client_credentials`

An example of creating the client with UAAC can be seen below for local purposes but it is recommended
that you add the client to your Cloud Foundry deployment YAML for production.


### Step 2: Setup Credentials for Concourse

Prior to deploying with Concourse, you need to setup certain credentials.
You will need to do these twice because the pipeline contains targets for both a staging and
a production environment.

Copy the `ci/credentials.example.yml` to `ci/credentials.yml`. The following steps will guide you to fill it out.

#### Credentials to Deploy The Application

You need an account with permissions to deploy the application to your space.

If you're using the [UAA credentials broker](https://github.com/cloudfoundry-community/uaa-credentials-broker),
follow the instructions to create a [UAA users](https://github.com/cloudfoundry-community/uaa-credentials-broker#uaa-users).

Use the username and password in
`deployer-username-staging` / `deployer-password-staging` / `cf-api-staging` / `cf-org-staging` / `cf-space-staging`
and `deployer-username-production` / `deployer-password-production` / `cf-api-production` / `cf-org-production` / `cf-space-production`
as appropriate.

#### Credentials for PR Requests

In order to report PR statuses, follow the directions [here](https://github.com/jtarchie/github-pullrequest-resource).
Those credentials are used in `cg-buildpack-notify-github-repo-name` and `status-access-token`.

#### Credentials for Pulling From Git

Separate from the above section, this is for merged code. Follow the directions [here](https://github.com/concourse/git-resource)
for `cg-buildpack-notify` and `cg-buildpack-notify-branch`.

#### Slack Credentials

You can setup having notifications posted in Slack via `slack-webhook-url`, `slack-channel`, `slack-username`, `slack-icon-url`.

#### Additional Arguments

You can provide more arguments in addition to the `-notify` flag. More about them in the additional flags [section](#additional-flags).
Those will go in `additional-args-staging` and/or `additional-args-production`.

---

## Architecture

This app is deployed as a [worker app](https://docs.cloudfoundry.org/devguide/deploy-apps/manifest.html#no-route).
That means it does not have an Internet accessible route. Upon deployment, the app will do nothing. The app will
only work when you invoke it with the `-notify` flag.

After setting up the Concourse pipeline, there will be a cron job that will run the
[application with the `-notify` flag](ci/notify.sh) via `cf run-task`.

The application will look at all the system buildpacks (i.e. result of `cf buildpacks`) and look at the time stamp of
when it was last updated. It will find all the applications using the system buildpacks and look at the last updated
time stamp and compare it with the last updated time stamp of the buildpack the application is using. If the application
was last updated before buildpack was updated, it will queue all the space managers and space developers to receive an
e-mail about that application. To prevent users from receiving multiple e-mails, all the applications in violation are
grouped per user so that the user receives one e-mail notifying them about all of the applications instead of an
e-mail per application. After the notifications are sent out, the buildpack version metadata (GUID and last updated time) is
stored in the database. By storing that data, notifications won't be sent out again when the cron job runs unless the buildpack
is updated by system admins again.

### Additional Flags

There are a few extra command line flags you can provide:

- `-clear`: Drop all database tables
- `-dry-run`: Run without actually sending e-mails or persisting to database.

---

## Development

### Requirements

- Go v1.8+
- PCFDev
- Glide
- Docker & Docker-Compose (for integration tests)

### Setup

1. Download Go dependencies

```sh
glide install
```

2. Setup UAA Client on PCFDev's UAA
```sh
docker run governmentpaas/cf-uaac \
  /bin/sh -c '
  uaac target https://uaa.local.pcfdev.io --skip-ssl-validation && \
  uaac token client get admin -s "admin-client-secret" && \
  uaac client delete buildpack-notify; \
  uaac client add buildpack-notify \
    --authorities="cloud_controller.admin_read_only" \
    --authorized_grant_types "client_credentials" -s "notarealsecret"'
```

### Unit Tests

You can run tests with: `go test`

### Integration Tests

These tests provide a good idea of how everything will work once in use. You should run these before pushing your code upstream.

Integration Tests can be found in the `integration` folder

If you want to run it locally with `pcfdev` and `docker`:
`docker-compose up -d && TEST_PASS="notarealpass" SMTP_FROM="no-reply@cloud.gov" SMTP_PASS="" SMTP_PORT="2525" SMTP_USER="" SMTP_HOST="localhost" CF_API="https://api.local.pcfdev.io" CLIENT_ID="buildpack-notify" CLIENT_SECRET="notarealsecret" INSECURE="1" CF_USER="admin" CF_PASS="admin" CF_API_SSL_FLAG="--skip-ssl-validation" DATABASE_URL="postgres://postgres:@localhost:5555/postgres?sslmode=disable" ./integration/test.sh`

You can check out the e-mail by navigating to http://localhost:8025

Next steps: Run this after deploy to each environment instead of locally.

---

## Contributing

See [CONTRIBUTING](CONTRIBUTING.md) for additional information.

## Public domain

This project is in the worldwide [public domain](LICENSE.md). As stated in [CONTRIBUTING](CONTRIBUTING.md):

> This project is in the public domain within the United States, and copyright and related rights in the work worldwide are waived through the [CC0 1.0 Universal public domain dedication](https://creativecommons.org/publicdomain/zero/1.0/).
>
> All contributions to this project will be released under the CC0 dedication. By submitting a pull request, you are agreeing to comply with this waiver of copyright interest.
