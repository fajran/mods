build : mods

.PHONY: mods
mods :
	go build -o mods github.com/fajran/mods

