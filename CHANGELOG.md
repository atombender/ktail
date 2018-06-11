# [v0.10.0](https://github.com/atombender/ktail/releases/tag/v0.10.0) (2018-06-11)

## Fixes

* Fix parsing of very long lines.

# [v0.9.0](https://github.com/atombender/ktail/releases/tag/v0.9.0) (2018-06-08)

## Fixes

* Fix timestamp comparison logic that was only supposed to be triggered when recovering from a stream error, and which caused lines to be ignored if sharing the exact same timestamp.

# [v0.8.0](https://github.com/atombender/ktail/releases/tag/v0.8.0) (2018-06-05)

## Fixes

* Fix surprisingly broken exclusion matching.

# [v0.7.0](https://github.com/atombender/ktail/releases/tag/v0.7.0) (2018-05-15)

## Features

* Add `--exclude`, `-x` flag to exclude pods and containers.
* Add `--since-start` to get logs since container start.

## Fixes

* Fix rare edge case where we might use the wrong timestamp if a newly discovered pod has multiple containers that started at different times.
* Fix rare edge case where a new container would not be detected because it has the exact same name as a previous instance.

# [v0.6.0](https://github.com/atombender/ktail/releases/tag/v0.6.0) (2017-12-14)

## Fixes

* Fix race conditions causing some log entries to be dropped on startup, as well as if a container is flapping.
* More fine-grained container status (e.g. a crashed container will not be tracked until it starts again).

# [v0.5.0](https://github.com/atombender/ktail/releases/tag/v0.5.0) (2017-06-01)

## Fixes

* Fix concurrent mutation bug, causing wrong pod/container to be followed.

# [v0.4.0](https://github.com/atombender/ktail/releases/tag/v0.4.0) (2017-06-01)

## Fixes

* Fix a weird edge case where logs would sometimes not appear.

# [v0.3.0](https://github.com/atombender/ktail/releases/tag/v0.3.0) (2017-05-16)

## Fixes

* Upgrade to newer Kubernetes client library, which fixes issues with the `gcp` auth provider.

# [v0.2.0](https://github.com/atombender/ktail/releases/tag/v0.2.0) (2017-05-16)

## Features

* Filtering by pod/container name.

# [v0.1.0](https://github.com/atombender/ktail/releases/tag/v0.1.0) (2017-04-24)

## Features

Initial release.
