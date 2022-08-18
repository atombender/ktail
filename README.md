***ktail is a tool to tail Kubernetes logs. It's like `kubectl logs`, but with a bunch of features to make it more convenient.***

:white_check_mark: **Detects pods and containers as they come and go**. If you run `ktail foo` and later start a pod or container named `foo`, then it will be picked up automatically. `kubectl` only works on a running pod/container.

:white_check_mark: **Tails multiple pods and containers** at the same time, based on names and labels. `kubectl` can only tail a single pod and container. ktail will match the pattern or patterns you specify against both the pod name and the container name.

:white_check_mark: **All containers are tailed by default**, not just a specific one. With `kubectl`, you have to use `-c`. With ktail, just do `ktail foo` and all its containers are automatically tailed.

:white_check_mark: **Recovers from failure**. ktail will keep retrying forever. `kubectl` just gives up.

# Usage

Ktail makes it super easy to tail by pod or container name. The following will match all containers whose pod name or container name contains the substring `foo`:

```shell
$ ktail foo
```

The arguments are regular expressions, so this is possible:

```shell
$ ktail '^foo'
```

If no filters are specified, _all_ pods in the current namespace are tailed.

Tailing supports the usual things like labels:

```shell
$ ktail -l app=myapp
```

This will tail all containers in all pods matching the label `app=myapp`. As new pods are created, it will also automatically tail those, too.

To abort tailing, hit `Ctrl+C`.

## Options

Run `ktail -h` for usage.

## Templating

ktail has a basic output format. To override, you can use a simple Go template. For example:

```shell
$ ktail -t "{{.Container.Name}} {{.Message}}"
```

The following variables are available:

* `Timestamp`: The time of the log event.
* `Message`: The log message.
* `Pod`: The pod object. It has properties such as `Name`, `Namspace`, `Status`, etc.
* `Container`: The container object. It properties such as `Name`.

# Installation

## Homebrew

```shell
$ brew tap atombender/ktail
$ brew install atombender/ktail/ktail
```

## Binary installation

Precompiled binaries for Windows, macOS, Linux (x64 and ARM) are available on the [GitHub release page](https://github.com/atombender/ktail/releases).

### Linux

```shell
$ curl -L https://github.com/atombender/ktail/releases/download/v0.7.0/ktail-linux-amd64 -o ktail
$ chmod +x ktail
$ sudo mv ./ktail /usr/local/bin/ktail
```

### macOS

```shell
$ curl -L https://github.com/atombender/ktail/releases/download/v0.7.0/ktail-darwin-amd64 -o ktail
$ chmod +x ktail
$ sudo mv ./ktail /usr/local/bin/ktail
```

### Windows

Download from [GitHub](https://github.com/atombender/ktail/releases/download/v0.7.0/ktail-windows-amd64.exe) and add the binary to your `PATH`.

## From source

This requires Go >= 1.10, as we use Go modules.

```shell
$ mkdir -p $GOPATH/src/github.com/atombender
$ cd $GOPATH/src/github.com/atombender
$ git clone https://github.com/atombender/ktail
$ cd ktail
$ go install .
```

# Acknowledgements

Some setup code was borrowed from [k8stail](https://github.com/dtan4/k8stail).

# License

MIT license. See `LICENSE` file.
