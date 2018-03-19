default:
	cd go && GOOS=linux GOARCH=amd64 go build -o dummy-app
	mv go/dummy-app binary/dummy-app
deploy:
	cd $(app) && cf push dummy-app $(buildpack) --no-route -m 128M -k 1G