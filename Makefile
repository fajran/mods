build : mods

.PHONY: mods
mods :
	go build -o mods github.com/fajran/mods

.PHONY: files
files :
	go build -o files github.com/fajran/mods/cmd/files

