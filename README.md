Fiotuf
---
A TUF (The Update Framework) client agent based on go-tuf that handles TUF
metadata fetched from Foundries.io device gateway. It also supports consuming
metadata from a local path.

## How to build
`make bin/fiotuf-linux-amd64`

## Running
In order to start the agent, run:

`fiotuf start-agent`

or simply

`fiotuf`

It will start the HTTP server that listens for requests. It is designed to work with
Aktualizr-lite, but can be also accessed from other applications, or tested from the
command line:

Request a TUF refresh to be performed:

`curl -X POST 127.0.0.1:9080/targets/update/`

Get latest targets list:

`curl 127.0.0.1:9080/targets`

Get latest root metadata:

`curl 127.0.0.1:9080/root`
