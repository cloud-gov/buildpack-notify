# cg-buildpack-notify

Toward proactively encouraging cloud.gov customers to restage their apps so they can benefit from buildpack updates.

## Deploying With Concourse

Prior to deploying with Concourse, you need to setup services and get the credentials.

### Database Setup

Create a PostgreSQL database: `cf cs aws-rds shared-psql buildpack-notify-db`

### Service Deployer Account

Follow the instructions in our [docs](https://cloud.gov/docs/services/cloud-gov-service-account/) to
get credentials for a deployer service named `buildpack-notify-deployer`.

### User Provided Services

You need to copy the `*.example.json` credential files into `*.json` files.
Fill them out then run the following commands:

```sh
cf cups notify-email-creds -p cf/notify_email_creds.json
cf cups notify-cf-creds -p cf/notify_cf_creds.json
```

## Running `cf run-task` locally

Note: You don't need to do this because Concourse will do this for you once the pipeline is set.

After the service is deployed, run:

`cf run-task cg-buildpack-notify "bin/cg-buildpack-notify -notify"`

## Requirements

Go v1.8+

## Local setup using PCFDev
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

```sh
go build
CF_API="https://api.local.pcfdev.io" CLIENT_ID="buildpack-notify" CLIENT_SECRET="notarealsecret" INSECURE="1" ./cg-buildpack-notify
```

## Tests

You can run tests with: `go test`

### Integration Tests

Integration Tests can be found in the `integration` folder

If you want to run it locally with `pcfdev` and `docker`:
`docker-compose up -d && TEST_PASS="notarealpass" SMTP_FROM="no-reply@cloud.gov" SMTP_PASS="" SMTP_PORT="2525" SMTP_USER="" SMTP_HOST="localhost" CF_API="https://api.local.pcfdev.io" CLIENT_ID="buildpack-notify" CLIENT_SECRET="notarealsecret" INSECURE="1" CF_USER="admin" CF_PASS="admin" CF_API_SSL_FLAG="--skip-ssl-validation" DATABASE_URL="postgres://postgres:@localhost:5555/postgres?sslmode=disable" ./integration/test.sh`

## Contributing

See [CONTRIBUTING](CONTRIBUTING.md) for additional information.

## Public domain

This project is in the worldwide [public domain](LICENSE.md). As stated in [CONTRIBUTING](CONTRIBUTING.md):

> This project is in the public domain within the United States, and copyright and related rights in the work worldwide are waived through the [CC0 1.0 Universal public domain dedication](https://creativecommons.org/publicdomain/zero/1.0/).
>
> All contributions to this project will be released under the CC0 dedication. By submitting a pull request, you are agreeing to comply with this waiver of copyright interest.
