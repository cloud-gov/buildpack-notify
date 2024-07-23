default:
	cd src && GOOS=linux GOARCH=amd64 go build -o dummy-app
	mv src/dummy-app bin/dummy-app
deploy:
	cd bin && cf push
