# ktail

ktail is a tool to tail Kubernetes logs. It's like `kubectl logs`, with the following additional features and improvements:

* ktail will detect pods and containers as they come and go (`kubect` only works on a running pod/container)
* ktail tails multiple pods and containers at the same time, based on labels (`kubectl` can only tail a single pod and container)
* All pod containers are tailed by default, not just a specific one (`kubectl` requires `-c`)
* ktail will try hard to keep running in the presence of failures
* ktail will retry until a container's logs are available
* Template-based output formatting

Unlike `kubectl logs`, ktail can currently not get historical logs.

# Installation

## Homebrew

```shell
$ brew tap atombender/ktail
$ brew install atombender/ktail/ktail
```

## Binary installation
Precompiled binaries for Windows, OS X, Linux are available on the [GitHub release page](https://github.com/atombender/ktail/releases).

### Linux and macOS:
```shell
# Linux
curl -L https://github.com/atombender/ktail/releases/download/v0.5.1/ktail-linux-amd64 -o ktail

# macOS
curl -L https://github.com/atombender/ktail/releases/download/v0.5.1/ktail-darwin-amd64 -o ktail

chmod +x ktail
sudo mv ./ktail /usr/local/bin/ktail
```

### Windows:
Download from [GitHub](https://github.com/atombender/ktail/releases/download/v0.5.1/ktail-windows-amd64.exe) and add the binary to your `PATH`.

## From source

This requires [Dep](https://github.com/golang/dep) and Go >= 1.7.

```shell
$ mkdir -p $GOPATH/src/github.com/atombender
$ cd $GOPATH/src/github.com/atombender
$ git clone https://github.com/atombender/ktail
$ cd ktail
$ dep ensure -vendor-only
$ make
```

# Usage

Tailing based on label:

```shell
ktail -l app=myapp
```

This will tail all containers in all pods matching the label `app=myapp`. As new pods are created, it will also automatically tail those, too.

It's also possible to filter on pod/container name. The following will match all containers whose pod name or container name contains the substring `foo`:

```shell
ktail foo
```

The arguments are regular expressions, so this is possible:

```shell
ktail '^foo'
```

If no filters are specified, _all_ pods in the current namespace are tailed.

To abort tailing, hit Ctrl-C.

## Options

Run `ktail -h` for usage.

## Templating

ktail has a basic output format. To override, you can use a simple Go template. For example:

```shell
ktail -t "{{.Container.Name}} {{.Message}}"
```

The following variables are available:

* `Timestamp`: The time of the log event.
* `Message`: The log message.
* `Pod`: The pod object. It has properties such as `Name`, `Namspace`, `Status`, etc.
* `Container`: The container object. It properties such as `Name`.

# Acknowledgements

Some setup code was borrowed from [k8stail](https://github.com/dtan4/k8stail).

# License

MIT license. See `LICENSE` file.
