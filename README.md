# cg-buildpack-notify

Toward proactively encouraging cloud.gov customers to restage their apps so they can benefit from buildpack updates.

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

## Contributing

See [CONTRIBUTING](CONTRIBUTING.md) for additional information.

## Public domain

This project is in the worldwide [public domain](LICENSE.md). As stated in [CONTRIBUTING](CONTRIBUTING.md):

> This project is in the public domain within the United States, and copyright and related rights in the work worldwide are waived through the [CC0 1.0 Universal public domain dedication](https://creativecommons.org/publicdomain/zero/1.0/).
>
> All contributions to this project will be released under the CC0 dedication. By submitting a pull request, you are agreeing to comply with this waiver of copyright interest.
