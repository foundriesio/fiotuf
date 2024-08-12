Fiotuf
------
A TUF ([The Update Framework](https://github.com/theupdateframework)) client agent based on go-tuf that handles TUF
metadata fetched from [Foundries.io device gateway](https://docs.foundries.io/94/reference-manual/security/device-gateway.html#ref-device-gateway). It also supports consuming
metadata from a local path.

## How to build

Running `make` will build the binary for all supported platforms. Currently, `linux/amd64` and `linux/arm`


## Running
In order to start the agent, run:

`bin/fiotuf-linux-amd64 start-agent`

or simply

`bin/fiotuf-linux-amd64`

It will start the HTTP server that listens for requests. It is designed to work with
[Aktualizr-lite](https://github.com/foundriesio/aktualizr-lite), but can be also accessed from other applications, or tested from the
command line:

Request a TUF refresh to be performed:

`curl -X POST 127.0.0.1:9080/targets/update/`

Request a TUF refresh based on a local [offline bundle](https://docs.foundries.io/latest/user-guide/offline-update/offline-update.html#obtaining-offline-update-content):

`curl -X POST 127.0.0.1:9080/targets/update/?localTufRepo=/path/to/offline/bundle`

Get latest targets list:

`curl 127.0.0.1:9080/targets`

Get latest root metadata:

`curl 127.0.0.1:9080/root`

## Configuration

Access to the device gateway is configured using the same toml configuration file used by Aktualizr-lite and [Fioconfig](https://github.com/foundriesio/fioconfig).
The default path for the configuration file is `/var/sota/sota.toml`.
This is an example configuration, where the mTLS connection with the device gateway is configured using keys certificates and private key saved to the local filesystem:

```
[tls]
server = "https://ota-lite.foundries.io:8443"
ca_source = "file"
pkey_source = "file"
cert_source = "file"

[storage]
path = "/var/sota/"

[import]
tls_cacert_path = "/var/sota/root.crt"
tls_pkey_path = "/var/sota/pkey.pem"
tls_clientcert_path = "/var/sota/client.pem"

[pacman]
tags = "main"
```

Like it happens with Aktualizr-lite and Fioconfig, configuration might be spread over more then one file.
Fragments might be, for example be present in the `/etc/sota/conf.d/` directory.
This is typically the case for the `tags` field, when `fioconfig` is used to set the device tag.

Setting up the configuration file, as well as the device private key and certificates is usually done during the registration process, by a tool such as [lmp-device-register](https://github.com/foundriesio/lmp-device-register).
