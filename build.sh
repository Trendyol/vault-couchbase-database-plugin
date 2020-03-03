#!/usr/bin/env bash

# https://github.com/mitchellh/gox
gox -ldflags="-w -s" -osarch="linux/amd64" -output couchbase-database-plugin